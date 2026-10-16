package serverauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/repo"

	"github.com/go-chi/chi/v5"
)

// newTestServer returns a router guarded by a fresh Manager, with /api/v1/ping and /index.html behind the login.
func newTestServer(t *testing.T, required bool) (*Manager, http.Handler) {
	t.Helper()
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Close() })

	m, err := New(db.DB, repo.GetTubarrSettingsStore(db.DB), required)
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
	_, h := newTestServer(t, true)

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
	m, h := newTestServer(t, true)
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
	m, h := newTestServer(t, true)
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
	m, h := newTestServer(t, true)
	if err := m.setAccessConfig(true, []string{"192.168.1.0/24"}, []string{"tubarr.home"}, nil); err != nil {
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

// TestLoginNotRequired tests --web-auth none lets requests through, still checking the host.
func TestLoginNotRequired(t *testing.T) {
	_, h := newTestServer(t, false)
	if w := request(h, http.MethodGet, "/api/v1/ping", "", nil); w.Code != http.StatusOK {
		t.Fatalf("no login required = %d, want 200", w.Code)
	}
	if w := request(h, http.MethodGet, "/api/v1/ping", "", func(r *http.Request) { r.Host = "attacker.example" }); w.Code != http.StatusForbidden {
		t.Fatalf("no login required, unknown hostname = %d, want 403", w.Code)
	}
}

// TestMetarrLogToken tests the Metarr log endpoint takes only the log token.
func TestMetarrLogToken(t *testing.T) {
	m, h := newTestServer(t, true)
	if w := request(h, http.MethodPost, MetarrLogsPath, "line\n", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("logs without token = %d, want 401", w.Code)
	}
	if w := request(h, http.MethodPost, MetarrLogsPath, "line\n", func(r *http.Request) { r.Header.Set(logTokenHeader, m.LogToken()) }); w.Code != http.StatusOK {
		t.Fatalf("logs with token = %d, want 200", w.Code)
	}
}

// TestClientIP tests X-Forwarded-For is only read from trusted proxies.
func TestClientIP(t *testing.T) {
	m, _ := newTestServer(t, true)
	if err := m.setAccessConfig(true, nil, nil, []string{"10.0.0.2"}); err != nil {
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
