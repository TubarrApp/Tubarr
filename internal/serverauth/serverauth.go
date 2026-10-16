// Package serverauth controls access to Tubarr's web interface and its API: logins, sessions, API keys and trusted addresses.
package serverauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie   = "tubarr_session"
	sessionLifetime = 30 * 24 * time.Hour
	sessionRenewAt  = 29 * 24 * time.Hour // Renew sessions with less than this left, at most about once a day.

	apiKeyHeader   = "X-Api-Key"
	logTokenHeader = "X-Tubarr-Log-Token"

	// MetarrLogsPath is the endpoint Metarr posts logs to, authorized by the log token alone.
	MetarrLogsPath = "/metarr-logs"

	// Env vars setting the login at startup, for when the password is forgotten (e.g. in Docker).
	envUsername = "TUBARR_WEB_USERNAME"
	envPassword = "TUBARR_WEB_PASSWORD"

	// LogTokenEnv passes the log token to Metarr.
	LogTokenEnv = "TUBARR_LOG_TOKEN"
)

// errNoLogin is returned when no login has been set up yet.
var errNoLogin = errors.New("no login has been set up")

// Manager controls access to the web interface.
type Manager struct {
	db       *sql.DB
	settings contracts.TubarrSettingsStore
	required bool
	logToken string

	mu  sync.RWMutex
	cfg accessConfig

	limiter loginLimiter
}

// accessConfig holds the addresses and hostnames trusted by the interface.
type accessConfig struct {
	skipLocalhost  bool
	skipLogin      []netip.Prefix
	allowedHosts   map[string]struct{}
	trustedProxies []netip.Prefix
}

// New returns a Manager. With required false, no login is asked for, but requests must still name a known host.
func New(db *sql.DB, settings contracts.TubarrSettingsStore, required bool) (*Manager, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		db:       db,
		settings: settings,
		required: required,
		logToken: token,
		limiter:  loginLimiter{failures: make(map[netip.Addr]*loginFailures)},
	}
	if err := m.loadAccessConfig(); err != nil {
		return nil, err
	}
	if err := m.applyEnvLogin(); err != nil {
		return nil, err
	}
	if _, err := m.apiKey(); err != nil {
		return nil, err
	}
	if !required {
		logger.Pl.W("Web interface login is disabled (--%s none): anyone who can reach it can control Tubarr", keys.WebAuth)
	}
	return m, nil
}

// LogToken returns the token Metarr must send with its logs.
func (m *Manager) LogToken() string {
	return m.logToken
}

// Middleware lets a request through if it is public, from a trusted address, or carries a valid session or API key.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == MetarrLogsPath:
			if subtle.ConstantTimeCompare([]byte(r.Header.Get(logTokenHeader)), []byte(m.logToken)) != 1 {
				http.Error(w, "invalid log token", http.StatusUnauthorized)
				return
			}
		case isPublicPath(r.URL.Path):
		case !m.required || m.skipsLogin(m.clientIP(r)):
			// No login, so the host must be checked: another site's page could otherwise reach Tubarr by DNS rebinding.
			if !m.hostAllowed(r.Host) {
				http.Error(w, fmt.Sprintf("host %q is not allowed: add it under Settings > Web Interface Access, or use the server's IP address", r.Host), http.StatusForbidden)
				return
			}
		case r.Header.Get(apiKeyHeader) != "":
			if !m.validAPIKey(r.Header.Get(apiKeyHeader)) {
				http.Error(w, "invalid API key", http.StatusUnauthorized)
				return
			}
		default:
			if !m.validSession(w, r) {
				rejectUnauthenticated(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isPublicPath reports whether a path is served without a login (health checks, and the login page and its assets).
func isPublicPath(p string) bool {
	switch p {
	case "/health", "/api/v1/status/idle", "/login.html", "/style.css", "/bundle.js":
		return true
	}
	return strings.HasPrefix(p, "/api/v1/auth/") || strings.HasPrefix(p, "/src/")
}

// rejectUnauthenticated answers API calls with 401, and sends page loads to the login page.
func rejectUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"login required"}`))
		return
	}
	http.Redirect(w, r, "/login.html?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
}

// ---- Hosts and addresses ----

// hostAllowed reports whether the Host header names this server: localhost, an IP address, its hostname, or an allowed host.
func (m *Manager) hostAllowed(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if name, err := os.Hostname(); err == nil && strings.EqualFold(name, host) {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.cfg.allowedHosts[host]
	return ok
}

// skipsLogin reports whether requests from ip need no login.
func (m *Manager) skipsLogin(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg.skipLocalhost && ip.IsLoopback() {
		return true
	}
	return containsAddr(m.cfg.skipLogin, ip)
}

// clientIP returns the request's client address, read from X-Forwarded-For only when the connection is from a trusted proxy.
func (m *Manager) clientIP(r *http.Request) netip.Addr {
	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := addrPort.Addr().Unmap()

	m.mu.RLock()
	proxies := m.cfg.trustedProxies
	m.mu.RUnlock()
	if !containsAddr(proxies, ip) {
		return ip
	}

	// Walk back from the nearest hop, skipping trusted proxies.
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return netip.Addr{}
		}
		if hop = hop.Unmap(); !containsAddr(proxies, hop) {
			return hop
		}
	}
	return ip
}

// containsAddr reports whether any prefix contains ip.
func containsAddr(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// parsePrefixes parses IP addresses and CIDR ranges.
func parsePrefixes(entries []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if p, err := netip.ParsePrefix(e); err == nil {
			prefixes = append(prefixes, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(e)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR range (e.g. 192.168.1.0/24)", e)
		}
		addr = addr.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

// ---- Stored settings ----

// loadAccessConfig loads the trusted addresses and hostnames from the settings table.
func (m *Manager) loadAccessConfig() error {
	skipLocalhost := true
	if v, found, err := m.settings.GetSetting(keys.WebSkipLocalhost); err != nil {
		return err
	} else if found {
		skipLocalhost = v == "true"
	}

	lists := make(map[string][]string, 3)
	for _, key := range []string{keys.WebSkipLogin, keys.WebAllowedHosts, keys.WebTrustedProxies} {
		list, err := m.stringList(key)
		if err != nil {
			return err
		}
		lists[key] = list
	}
	return m.setAccessConfig(skipLocalhost, lists[keys.WebSkipLogin], lists[keys.WebAllowedHosts], lists[keys.WebTrustedProxies])
}

// setAccessConfig validates and applies trusted addresses and hostnames.
func (m *Manager) setAccessConfig(skipLocalhost bool, skipLogin, allowedHosts, trustedProxies []string) error {
	skip, err := parsePrefixes(skipLogin)
	if err != nil {
		return fmt.Errorf("skip login: %w", err)
	}
	proxies, err := parsePrefixes(trustedProxies)
	if err != nil {
		return fmt.Errorf("trusted proxies: %w", err)
	}
	hosts := make(map[string]struct{}, len(allowedHosts))
	for _, h := range allowedHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts[h] = struct{}{}
		}
	}

	m.mu.Lock()
	m.cfg = accessConfig{skipLocalhost: skipLocalhost, skipLogin: skip, allowedHosts: hosts, trustedProxies: proxies}
	m.mu.Unlock()
	return nil
}

// stringList reads a JSON list of strings from the settings table.
func (m *Manager) stringList(key string) ([]string, error) {
	v, found, err := m.settings.GetSetting(key)
	if err != nil || !found || v == "" {
		return nil, err
	}
	var list []string
	if err := json.Unmarshal([]byte(v), &list); err != nil {
		return nil, fmt.Errorf("invalid %s setting: %w", key, err)
	}
	return list, nil
}

// setStringList stores a JSON list of strings in the settings table.
func (m *Manager) setStringList(key string, list []string) error {
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return m.settings.SetSetting(key, string(data))
}

// ---- Credentials ----

// applyEnvLogin sets the login from the environment if a password is given there.
func (m *Manager) applyEnvLogin() error {
	password := os.Getenv(envPassword)
	if password == "" {
		return nil
	}
	username := os.Getenv(envUsername)
	if username == "" {
		if current, _, err := m.settings.GetSetting(keys.WebUsername); err == nil && current != "" {
			username = current
		} else {
			username = "admin"
		}
	}
	if err := m.setLogin(username, password); err != nil {
		return fmt.Errorf("failed to set web login from %s: %w", envPassword, err)
	}
	logger.Pl.I("Web interface login for %q set from %s", username, envPassword)
	return nil
}

// hasLogin reports whether a login has been set up.
func (m *Manager) hasLogin() (bool, error) {
	hash, found, err := m.settings.GetSetting(keys.WebPasswordHash)
	return found && hash != "", err
}

// setLogin stores the username and a hash of the password, and ends all sessions.
func (m *Manager) setLogin(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(password) > 72 {
		return errors.New("password must be at most 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := m.settings.SetSetting(keys.WebUsername, username); err != nil {
		return err
	}
	if err := m.settings.SetSetting(keys.WebPasswordHash, string(hash)); err != nil {
		return err
	}
	if _, err := m.db.Exec(`DELETE FROM web_sessions`); err != nil {
		return fmt.Errorf("failed to end sessions: %w", err)
	}
	return nil
}

// checkLogin reports whether the username and password match the stored login.
func (m *Manager) checkLogin(username, password string) (bool, error) {
	storedUser, _, err := m.settings.GetSetting(keys.WebUsername)
	if err != nil {
		return false, err
	}
	hash, found, err := m.settings.GetSetting(keys.WebPasswordHash)
	if err != nil {
		return false, err
	}
	if !found || hash == "" {
		return false, errNoLogin
	}
	userOK := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(storedUser)) == 1
	passOK := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	return userOK && passOK, nil
}

// apiKey returns the API key, generating one if none exists.
func (m *Manager) apiKey() (string, error) {
	key, found, err := m.settings.GetSetting(keys.WebAPIKey)
	if err != nil {
		return "", err
	}
	if found && key != "" {
		return key, nil
	}
	return m.regenerateAPIKey()
}

// regenerateAPIKey replaces the API key with a new random one.
func (m *Manager) regenerateAPIKey() (string, error) {
	key, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := m.settings.SetSetting(keys.WebAPIKey, key); err != nil {
		return "", err
	}
	return key, nil
}

// validAPIKey reports whether key matches the API key.
func (m *Manager) validAPIKey(key string) bool {
	stored, found, err := m.settings.GetSetting(keys.WebAPIKey)
	if err != nil || !found || stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(stored)) == 1
}

// ---- Sessions ----

// createSession starts a session and sets its cookie.
func (m *Manager) createSession(w http.ResponseWriter, r *http.Request) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(sessionLifetime)
	if _, err := m.db.Exec(`INSERT INTO web_sessions (token_hash, expires_at) VALUES (?, ?)`, hashToken(token), expires); err != nil {
		return fmt.Errorf("failed to store session: %w", err)
	}
	m.setSessionCookie(w, r, token, expires)
	return nil
}

// validSession reports whether the request carries a live session, renewing it when due.
func (m *Manager) validSession(w http.ResponseWriter, r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	var expires time.Time
	hash := hashToken(cookie.Value)
	if err := m.db.QueryRow(`SELECT expires_at FROM web_sessions WHERE token_hash = ?`, hash).Scan(&expires); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logger.Pl.E("Failed to look up web session: %v", err)
		}
		return false
	}
	now := time.Now()
	if !expires.After(now) {
		if _, err := m.db.Exec(`DELETE FROM web_sessions WHERE token_hash = ?`, hash); err != nil {
			logger.Pl.W("Failed to delete expired web session: %v", err)
		}
		return false
	}
	if expires.Sub(now) < sessionRenewAt {
		expires = now.Add(sessionLifetime)
		if _, err := m.db.Exec(`UPDATE web_sessions SET expires_at = ? WHERE token_hash = ?`, expires, hash); err != nil {
			logger.Pl.W("Failed to renew web session: %v", err)
		} else {
			m.setSessionCookie(w, r, cookie.Value, expires)
		}
	}
	return true
}

// endSession deletes the request's session and clears its cookie.
func (m *Manager) endSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		if _, err := m.db.Exec(`DELETE FROM web_sessions WHERE token_hash = ?`, hashToken(cookie.Value)); err != nil {
			logger.Pl.W("Failed to delete web session: %v", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// setSessionCookie sets the session cookie, marked Secure when the request came over HTTPS.
func (m *Manager) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	secure := r.TLS != nil
	if !secure && r.Header.Get("X-Forwarded-Proto") == "https" {
		if addrPort, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
			m.mu.RLock()
			secure = containsAddr(m.cfg.trustedProxies, addrPort.Addr().Unmap())
			m.mu.RUnlock()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// randomToken returns 32 random bytes, base64url encoded.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex SHA-256 of a session token, as stored in the database.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---- Login rate limiting ----

const (
	maxLoginFailures   = 5
	loginFailureWindow = 15 * time.Minute
)

// loginLimiter counts failed logins per address.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[netip.Addr]*loginFailures
}

// loginFailures holds an address's recent failed logins.
type loginFailures struct {
	count int
	first time.Time
}

// blocked reports whether an address has failed too many logins recently.
func (l *loginLimiter) blocked(ip netip.Addr) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.failures[ip]
	if !ok {
		return false
	}
	if time.Since(f.first) > loginFailureWindow {
		delete(l.failures, ip)
		return false
	}
	return f.count >= maxLoginFailures
}

// fail records a failed login.
func (l *loginLimiter) fail(ip netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.failures[ip]
	if !ok || time.Since(f.first) > loginFailureWindow {
		l.failures[ip] = &loginFailures{count: 1, first: time.Now()}
		return
	}
	f.count++
}

// succeed clears an address's failed logins.
func (l *loginLimiter) succeed(ip netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}
