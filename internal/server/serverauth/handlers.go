package serverauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"

	"github.com/go-chi/chi/v5"
)

// Mount registers the login endpoints and the access settings endpoints on the /api/v1 router.
func (m *Manager) Mount(r chi.Router) {
	r.Get("/auth/status", m.handleStatus)
	r.Post("/auth/setup", m.handleSetup)
	r.Post("/auth/login", m.handleLogin)
	r.Post("/auth/logout", m.handleLogout)

	r.Get("/settings/auth", m.handleGetSettings)
	r.Put("/settings/auth", m.handleSetSettings)
	r.Put("/settings/auth/password", m.handleSetPassword)
	r.Post("/settings/auth/api-key", m.handleRegenerateAPIKey)
}

// credentials is a username and password sent by the login page.
type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleStatus reports whether a login must be set up, whether this request is already let in, and whether it has a
// session to log out of.
func (m *Manager) handleStatus(w http.ResponseWriter, r *http.Request) {
	hasLogin, err := m.hasLogin()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	required := m.loginRequired()
	session := required && m.validSession(w, r)
	authenticated := !required || m.skipsLoginFor(r) || session
	writeJSON(w, map[string]bool{
		"login_required": required,
		"setup_required": required && !hasLogin,
		"authenticated":  authenticated && (hasLogin || !required),
		"session":        session,
	})
}

// handleSetup creates the first login. It is refused once a login exists.
func (m *Manager) handleSetup(w http.ResponseWriter, r *http.Request) {
	hasLogin, err := m.hasLogin()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if hasLogin {
		http.Error(w, "a login has already been set up", http.StatusConflict)
		return
	}
	var c credentials
	if err := decodeJSON(r, &c); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := m.setLogin(c.Username, c.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logger.Pl.S("Web interface login created for %q", c.Username)
	if err := m.createSession(w, r); err != nil {
		logger.Pl.E("Failed to start web session: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "Login created"})
}

// handleLogin starts a session for a correct username and password.
func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := m.clientIP(r)
	if m.limiter.blocked(ip) {
		http.Error(w, "too many failed logins, try again later", http.StatusTooManyRequests)
		return
	}
	var c credentials
	if err := decodeJSON(r, &c); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	ok, err := m.checkLogin(c.Username, c.Password)
	if errors.Is(err, errNoLogin) {
		http.Error(w, "no login has been set up yet", http.StatusConflict)
		return
	}
	if err != nil {
		logger.Pl.E("Failed to check web login: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		m.limiter.fail(ip)
		logger.Pl.W("Failed web interface login for %q from %s", c.Username, ip)
		http.Error(w, "incorrect username or password", http.StatusUnauthorized)
		return
	}
	m.limiter.succeed(ip)
	if err := m.createSession(w, r); err != nil {
		logger.Pl.E("Failed to start web session: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "Logged in"})
}

// handleLogout ends the request's session.
func (m *Manager) handleLogout(w http.ResponseWriter, r *http.Request) {
	m.endSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// accessSettings are the web interface access settings shown on the settings page.
type accessSettings struct {
	AuthMode       string   `json:"auth_mode"`
	SkipLogin      []string `json:"skip_login"`
	AllowedHosts   []string `json:"allowed_hosts"`
	TrustedProxies []string `json:"trusted_proxies"`
}

// handleGetSettings returns the access settings, username and API key.
func (m *Manager) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	username, _, err := m.settings.GetSetting(keys.WebUsername)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	apiKey, err := m.apiKey()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	var s accessSettings
	if s.SkipLogin, err = m.stringList(keys.WebSkipLogin); err == nil {
		if s.AllowedHosts, err = m.stringList(keys.WebAllowedHosts); err == nil {
			s.TrustedProxies, err = m.stringList(keys.WebTrustedProxies)
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m.mu.RLock()
	s.AuthMode = m.cfg.mode
	m.mu.RUnlock()

	writeJSON(w, map[string]any{
		"username":           username,
		"api_key":            apiKey,
		"login_required":     s.AuthMode != ModeNone,
		"login_from_env":     os.Getenv(consts.EnvWebPassword) != "", // Set again at each start.
		"auth_mode_from_env": m.envMode != "",
		"access":             s,
	})
}

// handleSetSettings validates, applies and stores the access settings.
func (m *Manager) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	var s accessSettings
	if err := decodeJSON(r, &s); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if m.envMode != "" && s.AuthMode != m.envMode {
		http.Error(w, fmt.Sprintf("the login mode is set by --%s (%s), so it can't be changed here", keys.WebAuth, consts.EnvWebAuth), http.StatusBadRequest)
		return
	}
	// Turning the login on before one exists would let whoever reached the page first create it.
	if hasLogin, err := m.hasLogin(); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	} else if !hasLogin && s.AuthMode != ModeNone && !m.loginRequired() {
		http.Error(w, "create a login with Change login first, then turn the login on", http.StatusBadRequest)
		return
	}
	if err := m.setAccessConfig(s.AuthMode, s.SkipLogin, s.AllowedHosts, s.TrustedProxies); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for key, list := range map[string][]string{
		keys.WebSkipLogin:      s.SkipLogin,
		keys.WebAllowedHosts:   s.AllowedHosts,
		keys.WebTrustedProxies: s.TrustedProxies,
	} {
		if err := m.setStringList(key, list); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if m.envMode == "" { // The environment's mode isn't saved, so removing it restores the saved one.
		if err := m.settings.SetSetting(keys.WebAuthMode, s.AuthMode); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]string{"message": "Access settings saved"})
}

// handleSetPassword changes the login, given the current password. Other sessions are ended.
func (m *Manager) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		Username        string `json:"username"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	username, _, err := m.settings.GetSetting(keys.WebUsername)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	ip := m.clientIP(r)
	if m.limiter.blocked(ip) {
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if ok, err := m.checkLogin(username, body.CurrentPassword); err != nil && !errors.Is(err, errNoLogin) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	} else if !ok && !errors.Is(err, errNoLogin) {
		m.limiter.fail(ip)
		http.Error(w, "current password is incorrect", http.StatusUnauthorized)
		return
	}
	if body.Username != "" {
		username = body.Username
	}
	if err := m.setLogin(username, body.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := m.createSession(w, r); err != nil {
		logger.Pl.E("Failed to start web session: %v", err)
	}
	logger.Pl.S("Web interface login changed for %q", username)
	writeJSON(w, map[string]string{"message": "Login changed"})
}

// handleRegenerateAPIKey replaces the API key.
func (m *Manager) handleRegenerateAPIKey(w http.ResponseWriter, _ *http.Request) {
	key, err := m.regenerateAPIKey()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"api_key": key})
}

// decodeJSON decodes a JSON request body into v, refusing unknown keys.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// writeJSON writes v as a JSON response.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}
