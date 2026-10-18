package serverauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"tubarr/internal/domain/keys"
	"tubarr/internal/repo"
)

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
