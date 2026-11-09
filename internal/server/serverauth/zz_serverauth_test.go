package serverauth

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/repo"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/go-chi/chi/v5"
)

// ******************************** Logins and access *********************************************************************************

// newTestDB returns a fresh database in a temporary directory.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestServer returns a router guarded by a fresh Manager, with /api/v1/ping and /index.html behind the login.
func newTestServer(t *testing.T, envMode string) (*Manager, http.Handler) {
	t.Helper()
	db := newTestDB(t)
	m, err := New(db, repo.GetTubarrSettingsStore(db), envMode)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(m.Middleware)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	r.Route("/api/v1", func(r chi.Router) {
		m.Mount(r)
		r.Get("/ping", ok)
	})
	r.Get("/index.html", ok)
	r.Post(MetarrLogsPath, ok)
	return m, r
}

// request sends a request from a LAN address, so localhost's login skip doesn't apply.
func request(h http.Handler, method, target, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "192.168.1.50:5000"
	req.Host = "192.168.1.10:8827"
	if edit != nil {
		edit(req)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestLoginFlow tests that a login is required, created once, and lets its session through.
func TestLoginFlow(t *testing.T) {
	_, h := newTestServer(t, "")

	if w := request(h, http.MethodGet, "/api/v1/ping", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("API without login = %d, want 401", w.Code)
	}
	if w := request(h, http.MethodGet, "/index.html", "", nil); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login.html") {
		t.Fatalf("page without login = %d to %q, want a redirect to the login page", w.Code, w.Header().Get("Location"))
	}

	w := request(h, http.MethodPost, "/api/v1/auth/setup", `{"username":"admin","password":"correct horse"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("setup = %d %s", w.Code, w.Body)
	}
	cookie := w.Result().Cookies()[0]
	withCookie := func(r *http.Request) { r.AddCookie(cookie) }
	if w := request(h, http.MethodGet, "/api/v1/ping", "", withCookie); w.Code != http.StatusOK {
		t.Fatalf("API with session = %d, want 200", w.Code)
	}

	if w := request(h, http.MethodPost, "/api/v1/auth/setup", `{"username":"other","password":"another one"}`, nil); w.Code != http.StatusConflict {
		t.Fatalf("second setup = %d, want 409", w.Code)
	}

	if w := request(h, http.MethodPost, "/api/v1/auth/logout", "", withCookie); w.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", withCookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("API after logout = %d, want 401", w.Code)
	}
}

// TestLoginRateLimit tests that repeated wrong passwords lock the address out, even for the right one.
func TestLoginRateLimit(t *testing.T) {
	m, h := newTestServer(t, "")
	if err := m.setLogin("admin", "correct horse"); err != nil {
		t.Fatal(err)
	}
	for range maxLoginFailures {
		if w := request(h, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"wrong password"}`, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password = %d, want 401", w.Code)
		}
	}
	if w := request(h, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"correct horse"}`, nil); w.Code != http.StatusTooManyRequests {
		t.Fatalf("login after failures = %d, want 429", w.Code)
	}
}

// TestAPIKey tests that the API key lets requests through and a wrong one is refused.
func TestAPIKey(t *testing.T) {
	m, h := newTestServer(t, "")
	key, err := m.apiKey()
	if err != nil {
		t.Fatal(err)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.Header.Set(apiKeyHeader, key) }); w.Code != http.StatusOK {
		t.Fatalf("API with key = %d, want 200", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.Header.Set(apiKeyHeader, "wrong") }); w.Code != http.StatusUnauthorized {
		t.Fatalf("API with wrong key = %d, want 401", w.Code)
	}
}

// TestSkipLogin tests trusted addresses skip the login, but only when they name a known host.
func TestSkipLogin(t *testing.T) {
	m, h := newTestServer(t, "")
	if err := m.setAccessConfig(ModeLocal, []string{"192.168.1.0/24"}, []string{"tubarr.home"}, nil); err != nil {
		t.Fatal(err)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", nil); w.Code != http.StatusOK {
		t.Fatalf("trusted address = %d, want 200", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.Host = "tubarr.home:8827" }); w.Code != http.StatusOK {
		t.Fatalf("trusted address by allowed hostname = %d, want 200", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.Host = "attacker.example:8827" }); w.Code != http.StatusForbidden {
		t.Fatalf("trusted address by unknown hostname = %d, want 403", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.RemoteAddr = "10.0.0.5:5000" }); w.Code != http.StatusUnauthorized {
		t.Fatalf("untrusted address = %d, want 401", w.Code)
	}
}

// TestProxyOnThisMachine tests a reverse proxy on this machine doesn't let its visitors skip the login as localhost.
func TestProxyOnThisMachine(t *testing.T) {
	m, h := newTestServer(t, "")
	viaProxy := func(client string) func(*http.Request) {
		return func(r *http.Request) {
			r.RemoteAddr = "127.0.0.1:41000"
			r.Host = "127.0.0.1:8827" // nginx's default Host for proxy_pass http://127.0.0.1:8827.
			r.Header.Set("X-Forwarded-For", client)
		}
	}
	local := func(r *http.Request) { r.RemoteAddr = "127.0.0.1:41000"; r.Host = "127.0.0.1:8827" }

	if w := request(h, http.MethodGet, "/api/v1/ping", "", local); w.Code != http.StatusOK {
		t.Fatalf("browser on this machine = %d, want 200", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", viaProxy("203.0.113.7")); w.Code != http.StatusUnauthorized {
		t.Fatalf("internet visitor via untrusted local proxy = %d, want 401", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) {
		local(r)
		r.Header.Set("X-Real-IP", "203.0.113.7")
	}); w.Code != http.StatusUnauthorized {
		t.Fatalf("X-Real-IP via untrusted local proxy = %d, want 401", w.Code)
	}

	// Trusted, the proxy's forwarded address decides instead.
	if err := m.setAccessConfig(ModeLocal, nil, nil, []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", viaProxy("203.0.113.7")); w.Code != http.StatusUnauthorized {
		t.Fatalf("internet visitor via trusted proxy = %d, want 401", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", viaProxy("127.0.0.1")); w.Code != http.StatusOK {
		t.Fatalf("this machine via trusted proxy = %d, want 200", w.Code)
	}
}

// TestLoginNotRequired tests --web-auth none lets requests through, still checking the host.
func TestLoginNotRequired(t *testing.T) {
	_, h := newTestServer(t, ModeNone)
	if w := request(h, http.MethodGet, "/api/v1/ping", "", nil); w.Code != http.StatusOK {
		t.Fatalf("no login required = %d, want 200", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.Host = "attacker.example" }); w.Code != http.StatusForbidden {
		t.Fatalf("no login required, unknown hostname = %d, want 403", w.Code)
	}
}

// TestMetarrLogToken tests the Metarr log endpoint takes only the log token.
func TestMetarrLogToken(t *testing.T) {
	m, h := newTestServer(t, "")
	if w := request(h, http.MethodPost, MetarrLogsPath, "line\n", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("logs without token = %d, want 401", w.Code)
	}
	if w := request(h, http.MethodPost, MetarrLogsPath, "line\n", func(r *http.Request) { r.Header.Set(logTokenHeader, m.LogToken()) }); w.Code != http.StatusOK {
		t.Fatalf("logs with token = %d, want 200", w.Code)
	}
}

// TestClientIP tests X-Forwarded-For is only read from trusted proxies.
func TestClientIP(t *testing.T) {
	m, _ := newTestServer(t, "")
	if err := m.setAccessConfig(ModeLocal, nil, nil, []string{"10.0.0.2"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.2")

	req.RemoteAddr = "10.0.0.2:4000"
	if got := m.clientIP(req); got != netip.MustParseAddr("203.0.113.9") {
		t.Errorf("client IP via trusted proxy = %v, want 203.0.113.9", got)
	}
	req.RemoteAddr = "198.51.100.7:4000"
	if got := m.clientIP(req); got != netip.MustParseAddr("198.51.100.7") {
		t.Errorf("client IP via untrusted connection = %v, want 198.51.100.7", got)
	}
}

// putAuthMode sends a login mode straight to the access settings handler, as a logged-in Settings page does.
func putAuthMode(m *Manager, mode string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	m.handleSetSettings(w, httptest.NewRequest(http.MethodPut, "/api/v1/settings/auth", strings.NewReader(`{"auth_mode":"`+mode+`"}`)))
	return w
}

// TestAuthModeDefault tests a new install skips the login from localhost, and the old localhost setting still applies.
func TestAuthModeDefault(t *testing.T) {
	for _, tt := range []struct {
		name          string
		skipLocalhost string // The old setting, "" for unset.
		savedMode     string
		want          string
	}{
		{"new install", "", "", ModeLocal},
		{"localhost skip turned off", "false", "", ModeRequired},
		{"localhost skip left on", "true", "", ModeLocal},
		{"saved mode wins", "true", ModeNone, ModeNone},
	} {
		db := newTestDB(t)
		settings := repo.GetTubarrSettingsStore(db)
		for key, v := range map[string]string{keys.WebSkipLocalhost: tt.skipLocalhost, keys.WebAuthMode: tt.savedMode} {
			if v != "" {
				if err := settings.SetSetting(key, v); err != nil {
					t.Fatal(err)
				}
			}
		}
		m, err := New(db, settings, "")
		if err != nil {
			t.Fatal(err)
		}
		if m.cfg.mode != tt.want {
			t.Errorf("%s: mode %q, want %q", tt.name, m.cfg.mode, tt.want)
		}
	}
}

// TestAuthModeFromEnv tests --web-auth (TUBARR_WEB_AUTH) overrides the saved mode, which the page then can't change.
func TestAuthModeFromEnv(t *testing.T) {
	if _, err := New(newTestDB(t), nil, "off"); err == nil {
		t.Error("an unknown login mode was accepted")
	}

	m, _ := newTestServer(t, ModeNone)
	if m.loginRequired() {
		t.Fatal("login is required with the environment's mode none")
	}
	if w := putAuthMode(m, ModeRequired); w.Code != http.StatusBadRequest {
		t.Errorf("changing the environment's mode = %d, want 400", w.Code)
	}
	if w := putAuthMode(m, ModeNone); w.Code != http.StatusOK {
		t.Fatalf("saving with the environment's mode = %d %s", w.Code, w.Body)
	}
	if _, found, _ := m.settings.GetSetting(keys.WebAuthMode); found {
		t.Error("the environment's mode was saved, so it would outlast removing the variable")
	}
}

// TestTurningLoginOnNeedsLogin tests the login can't be turned back on before one exists, which anyone could then claim.
func TestTurningLoginOnNeedsLogin(t *testing.T) {
	m, _ := newTestServer(t, "")
	if w := putAuthMode(m, ModeNone); w.Code != http.StatusOK {
		t.Fatalf("turning the login off = %d %s", w.Code, w.Body)
	}
	if w := putAuthMode(m, ModeRequired); w.Code != http.StatusBadRequest {
		t.Fatalf("turning the login on with none set up = %d, want 400", w.Code)
	}
	if err := m.setLogin("admin", "correct horse"); err != nil {
		t.Fatal(err)
	}
	if w := putAuthMode(m, ModeRequired); w.Code != http.StatusOK {
		t.Fatalf("turning the login on = %d %s", w.Code, w.Body)
	}
	if mode, _, _ := m.settings.GetSetting(keys.WebAuthMode); mode != ModeRequired {
		t.Errorf("saved mode %q, want %q", mode, ModeRequired)
	}
}

// TestResetLogin tests a reset removes the login and its sessions, so the login page offers setup again.
func TestResetLogin(t *testing.T) {
	m, h := newTestServer(t, "")
	w := request(h, http.MethodPost, "/api/v1/auth/setup", `{"username":"admin","password":"correct horse"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("setup = %d %s", w.Code, w.Body)
	}
	cookie := w.Result().Cookies()[0]

	if err := ResetLogin(m.settings); err != nil {
		t.Fatal(err)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.AddCookie(cookie) }); w.Code != http.StatusUnauthorized {
		t.Errorf("a session from before the reset = %d, want 401", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/auth/status", "", nil); !strings.Contains(w.Body.String(), `"setup_required":true`) {
		t.Errorf("status after the reset = %s, want setup required", w.Body)
	}
}

// TestEnvLogin tests the environment login needs a username until one is saved, keeps sessions on restarts when it
// already matches, and resets the password alone keeping the saved username.
func TestEnvLogin(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := repo.GetTubarrSettingsStore(db)
	start := func() *Manager {
		t.Helper()
		m, err := New(db, settings, "")
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	t.Setenv(sharedconsts.EnvTubarrWebPassword, "env password")
	if _, err := New(db, settings, ""); err == nil || !strings.Contains(err.Error(), sharedconsts.EnvTubarrWebUsername) {
		t.Fatalf("password alone with no login = %v, want an error naming %s", err, sharedconsts.EnvTubarrWebUsername)
	}

	t.Setenv(sharedconsts.EnvTubarrWebUsername, "envuser")
	m := start()
	if ok, _ := m.checkLogin("envuser", "env password"); !ok {
		t.Fatal("environment login wasn't set")
	}
	w := httptest.NewRecorder()
	if err := m.createSession(w, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(w.Result().Cookies()[0])
	if m = start(); !m.validSession(httptest.NewRecorder(), req) {
		t.Error("restarting with the same environment login ended the session")
	}

	t.Setenv(sharedconsts.EnvTubarrWebUsername, "")
	t.Setenv(sharedconsts.EnvTubarrWebPassword, "new env password")
	m = start()
	if ok, _ := m.checkLogin("envuser", "new env password"); !ok {
		t.Error("password alone didn't reset the saved login's password")
	}
}

// ******************************** Form wiring ***************************************************************************************

// readPage returns a page from the repository's web folder.
func readPage(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "web", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// usedNearPath reports whether some quoted occurrence of path in src has every key nearby, as a whole word.
func usedNearPath(src, path string, keys []string) bool {
	for _, loc := range regexp.MustCompile("[`'\"]"+regexp.QuoteMeta(path)+"[`'\"]").FindAllStringIndex(src, -1) {
		window := src[max(0, loc[0]-1200):min(len(src), loc[1]+800)]
		if !slices.ContainsFunc(keys, func(k string) bool {
			return !regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(k) + `([^\w-]|$)`).MatchString(window)
		}) {
			return true
		}
	}
	return false
}

// hasKey reports whether a decoded JSON object holds a dotted key path.
func hasKey(v any, path string) bool {
	for _, k := range strings.Split(path, ".") {
		obj, ok := v.(map[string]any)
		if !ok {
			return false
		}
		if v, ok = obj[k]; !ok {
			return false
		}
	}
	return true
}

// TestAuthPagesWired tests that the login, header and Settings pages' requests reach the login handlers with the keys
// they send, and that the keys they read come back. The keys they send are checked against the pages in the server
// package's TestPageRequestsWired.
func TestAuthPagesWired(t *testing.T) {
	_, h := newTestServer(t, "")
	header := filepath.Join("partials", "header.html")
	pages := map[string]string{}
	for _, name := range []string{"login.html", "settings.html", header} {
		pages[name] = readPage(t, name)
	}

	var session *http.Cookie
	send := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := request(h, method, path, body, func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			if session != nil {
				r.AddCookie(session)
			}
		})
		for _, c := range w.Result().Cookies() {
			if c.Name == sessionCookie {
				session = c
			}
		}
		return w
	}
	// call sends a request as page does, checking its status and that the response keys the page reads come back. The
	// page is checked for each key's last part, as it may read an object whole and bind its fields elsewhere.
	call := func(page, method, path, body string, want int, reads ...string) map[string]any {
		t.Helper()
		names := make([]string, len(reads))
		for i, k := range reads {
			names[i] = k[strings.LastIndex(k, ".")+1:]
		}
		if len(reads) > 0 && !usedNearPath(pages[page], path, names) {
			t.Errorf("%s doesn't read all of %v from %s", page, names, path)
		}
		w := send(method, path, body)
		if w.Code != want {
			t.Fatalf("%s %s with %s = %d %s, want %d", method, path, body, w.Code, w.Body.String(), want)
		}
		var got map[string]any
		if len(reads) > 0 {
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			for _, k := range reads {
				if !hasKey(got, k) {
					t.Errorf("%s reads %q from %s, which isn't in the response: %s", page, k, path, w.Body.String())
				}
			}
		}
		return got
	}
	// refusesUnknownKey checks a body with an unknown key added is refused, naming the key, so a renamed key can't be
	// silently dropped.
	refusesUnknownKey := func(method, path, body string) {
		t.Helper()
		var fields map[string]any
		if err := json.Unmarshal([]byte(body), &fields); err != nil {
			t.Fatal(err)
		}
		fields["unknown_key"] = 1
		data, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if w := send(method, path, string(data)); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown_key") {
			t.Fatalf("%s %s with an unknown key = %d %s, want 400 naming it", method, path, w.Code, w.Body.String())
		}
	}

	call("login.html", http.MethodGet, "/api/v1/auth/status", "", http.StatusOK, "authenticated", "setup_required")
	call(header, http.MethodGet, "/api/v1/auth/status", "", http.StatusOK, "session")

	const login = `{"username":"admin","password":"correct horse"}`
	refusesUnknownKey(http.MethodPost, "/api/v1/auth/setup", login) // First, as setup is refused once a login exists.
	call("login.html", http.MethodPost, "/api/v1/auth/setup", login, http.StatusOK)
	call("login.html", http.MethodPost, "/api/v1/auth/login", login, http.StatusOK)
	refusesUnknownKey(http.MethodPost, "/api/v1/auth/login", login)

	settings := call("settings.html", http.MethodGet, "/api/v1/settings/auth", "", http.StatusOK,
		"login_from_env", "auth_mode_from_env", "username", consts.AuthAPIKey, "access.auth_mode", "access.skip_login", "access.allowed_hosts", "access.trusted_proxies")
	if settings["username"] != "admin" {
		t.Errorf("username reads back as %v, want admin", settings["username"])
	}

	const access = `{"auth_mode":"required","skip_login":["192.168.2.0/24"],"allowed_hosts":["tubarr.example.com"],"trusted_proxies":["10.0.0.1"]}`
	call("settings.html", http.MethodPut, "/api/v1/settings/auth", access, http.StatusOK)
	refusesUnknownKey(http.MethodPut, "/api/v1/settings/auth", access)
	var sent map[string]any
	if err := json.Unmarshal([]byte(access), &sent); err != nil {
		t.Fatal(err)
	}
	if got := call("settings.html", http.MethodGet, "/api/v1/settings/auth", "", http.StatusOK, "access"); !reflect.DeepEqual(got["access"], sent) {
		t.Errorf("access settings were sent as %v but read back as %v", sent, got["access"])
	}

	call("settings.html", http.MethodPut, "/api/v1/settings/auth/password", `{"current_password":"correct horse","username":"admin2","new_password":"battery staple"}`, http.StatusOK)
	call("login.html", http.MethodPost, "/api/v1/auth/login", `{"username":"admin2","password":"battery staple"}`, http.StatusOK)
	refusesUnknownKey(http.MethodPut, "/api/v1/settings/auth/password", `{"current_password":"battery staple","username":"admin2","new_password":"battery staple"}`)

	if got := call("settings.html", http.MethodPost, "/api/v1/settings/auth/api-key", "", http.StatusOK, consts.AuthAPIKey); got[consts.AuthAPIKey] == settings[consts.AuthAPIKey] {
		t.Error("regenerating the API key returned the old one")
	}
	call(header, http.MethodPost, "/api/v1/auth/logout", "", http.StatusNoContent)
}
