package serverauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/repo"
)

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

	t.Setenv(consts.EnvWebPassword, "env password")
	if _, err := New(db, settings, ""); err == nil || !strings.Contains(err.Error(), consts.EnvWebUsername) {
		t.Fatalf("password alone with no login = %v, want an error naming %s", err, consts.EnvWebUsername)
	}

	t.Setenv(consts.EnvWebUsername, "envuser")
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

	t.Setenv(consts.EnvWebUsername, "")
	t.Setenv(consts.EnvWebPassword, "new env password")
	m = start()
	if ok, _ := m.checkLogin("envuser", "new env password"); !ok {
		t.Error("password alone didn't reset the saved login's password")
	}
}
