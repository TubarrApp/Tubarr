package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"

	"github.com/spf13/viper"
)

// TestScrapeConfigContentEndpoints tests loading and saving the scrape config file through the web UI's editor endpoints.
func TestScrapeConfigContentEndpoints(t *testing.T) {
	logger.Pl.Console = io.Discard
	p := filepath.Join(t.TempDir(), "rules.toml")
	original := "[[sites]]\ndomain = \"a.com\"\n  [sites.crawl]\n  selector = \"a\"\n"
	if err := os.WriteFile(p, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.Set(keys.ScrapeConfigFile, p)
	t.Cleanup(func() { viper.Set(keys.ScrapeConfigFile, "") })
	ss := &serverStore{}

	get := func() map[string]string {
		w := httptest.NewRecorder()
		ss.handleGetScrapeConfigContent(w, httptest.NewRequest(http.MethodGet, "/", nil))
		var m map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatalf("GET: %v (%s)", err, w.Body.String())
		}
		return m
	}
	put := func(content, version string) (int, map[string]any) {
		form := url.Values{"content": {content}, "version": {version}}
		r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		ss.handleSetScrapeConfigContent(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}

	loaded := get()
	if loaded["path"] != p || loaded["content"] != original || loaded["version"] == "" {
		t.Fatalf("unexpected GET response: %+v", loaded)
	}

	// Invalid rules are rejected without touching the file.
	if code, _ := put("[[sites]]\ndomain = \"b.com\"\n", loaded["version"]); code != http.StatusBadRequest {
		t.Errorf("invalid save: got HTTP %d, want 400", code)
	}
	if got, _ := os.ReadFile(p); string(got) != original {
		t.Errorf("expected the file to be untouched after an invalid save, got %q", got)
	}

	// Valid rules are saved, returning the new version.
	updated := strings.Replace(original, "a.com", "c.com", 1)
	code, saved := put(updated, loaded["version"])
	if code != http.StatusOK {
		t.Fatalf("valid save: got HTTP %d, want 200", code)
	}
	if got, _ := os.ReadFile(p); string(got) != updated {
		t.Errorf("expected the file to be saved, got %q", got)
	}

	// Saving from the outdated copy is refused, but the version from the last save works.
	if code, _ := put(original, loaded["version"]); code != http.StatusConflict {
		t.Errorf("stale save: got HTTP %d, want 409", code)
	}
	if code, _ := put(original, saved["version"].(string)); code != http.StatusOK {
		t.Errorf("save with the latest version: got HTTP %d, want 200", code)
	}

	// With no file set, there is nothing to edit.
	viper.Set(keys.ScrapeConfigFile, "")
	if loaded := get(); loaded["path"] != "" {
		t.Errorf("expected no path, got %q", loaded["path"])
	}
	if code, _ := put(original, ""); code != http.StatusBadRequest {
		t.Errorf("save with no file set: got HTTP %d, want 400", code)
	}
}
