package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
)

// TestServePageIncludes tests that every web page has its shared partials filled in.
func TestServePageIncludes(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0) // TestMain moves the working directory, so locate web/ from this source file.
	webRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "web")
	pages, err := filepath.Glob(filepath.Join(webRoot, "*.html"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no web pages found in %q: %v", webRoot, err)
	}

	for _, page := range pages {
		w := httptest.NewRecorder()
		servePage(w, httptest.NewRequest(http.MethodGet, "/"+filepath.Base(page), nil), webRoot, page)
		body := w.Body.String()

		if w.Code != http.StatusOK {
			t.Errorf("%s: got status %d", filepath.Base(page), w.Code)
		}
		for _, leftover := range []string{"<!-- include:", "{{title}}", "{{selected}}"} {
			if strings.Contains(body, leftover) {
				t.Errorf("%s: rendered page still contains %q", filepath.Base(page), leftover)
			}
		}
		if filepath.Base(page) == "login.html" { // Shown before logging in, so it has no sidebar or header.
			continue
		}
		if !strings.Contains(body, "<aside") || !strings.Contains(body, "<header") {
			t.Errorf("%s: rendered page is missing the sidebar or header", filepath.Base(page))
		}
	}
}

// TestWithDefaultPort tests that listen addresses given without a port get the default one.
func TestWithDefaultPort(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0":        "0.0.0.0:8827",
		"localhost":      "localhost:8827",
		"localhost:":     "localhost:8827",
		"::":             "[::]:8827",
		"::1":            "[::1]:8827",
		"[::1]":          "[::1]:8827",
		"[::1]:9000":     "[::1]:9000",
		":9000":          ":9000",
		"127.0.0.1:9000": "127.0.0.1:9000",
	} {
		if got := withDefaultPort(in); got != want {
			t.Errorf("withDefaultPort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMetarrLogURL tests that Metarr is pointed at a reachable address for each listen address.
func TestMetarrLogURL(t *testing.T) {
	for _, tc := range []struct{ scheme, addr, want string }{
		{"http", "127.0.0.1:8827", "http://127.0.0.1:8827/metarr-logs"},
		{"http", ":8827", "http://127.0.0.1:8827/metarr-logs"},
		{"https", "0.0.0.0:9000", "https://127.0.0.1:9000/metarr-logs"},
		{"https", "[::]:9000", "https://127.0.0.1:9000/metarr-logs"},
		{"http", "192.168.1.5:8827", "http://192.168.1.5:8827/metarr-logs"},
		{"http", "[::1]:8827", "http://[::1]:8827/metarr-logs"},
		{"http", "localhost:8827", "http://localhost:8827/metarr-logs"},
	} {
		if got := metarrLogURL(tc.scheme, tc.addr); got != tc.want {
			t.Errorf("metarrLogURL(%q, %q) = %q, want %q", tc.scheme, tc.addr, got, tc.want)
		}
	}
}

// TestServePageMissingPartial tests that a page including an unknown partial fails rather than serving a broken page.
func TestServePageMissingPartial(t *testing.T) {
	logger.Pl.Console = io.Discard // servePage logs the error.
	webRoot := t.TempDir()
	page := filepath.Join(webRoot, "page.html")
	if err := os.WriteFile(page, []byte(`<!-- include:nope title="x" -->`), 0o644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	servePage(w, httptest.NewRequest(http.MethodGet, "/page.html", nil), webRoot, page)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
