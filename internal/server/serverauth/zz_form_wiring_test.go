package serverauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

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
	_, h := newTestServer(t, true)
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
		"login_required", "login_from_env", "username", "api_key", "access.skip_localhost", "access.skip_login", "access.allowed_hosts", "access.trusted_proxies")
	if settings["username"] != "admin" {
		t.Errorf("username reads back as %v, want admin", settings["username"])
	}

	const access = `{"skip_localhost":false,"skip_login":["192.168.2.0/24"],"allowed_hosts":["tubarr.example.com"],"trusted_proxies":["10.0.0.1"]}`
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

	if got := call("settings.html", http.MethodPost, "/api/v1/settings/auth/api-key", "", http.StatusOK, "api_key"); got["api_key"] == settings["api_key"] {
		t.Error("regenerating the API key returned the old one")
	}
	call(header, http.MethodPost, "/api/v1/auth/logout", "", http.StatusNoContent)
}
