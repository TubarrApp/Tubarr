package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
		if !strings.Contains(body, "<aside") || !strings.Contains(body, "<header") {
			t.Errorf("%s: rendered page is missing the sidebar or header", filepath.Base(page))
		}
	}
}

// TestServePageMissingPartial tests that a page including an unknown partial fails rather than serving a broken page.
func TestServePageMissingPartial(t *testing.T) {
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
