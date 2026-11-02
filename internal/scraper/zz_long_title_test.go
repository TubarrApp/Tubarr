package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"tubarr/internal/models"
	"tubarr/internal/siterules"
)

// TestScrapeCustomSiteLongTitle tests a scraped title too long for a filename, in bytes, still gets its metadata file.
func TestScrapeCustomSiteLongTitle(t *testing.T) {
	var title string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h1>%s</h1></body></html>", title)
	}))
	defer srv.Close()

	siterules.Reset()
	t.Cleanup(siterules.Reset)
	siterules.Register([]models.SiteScraper{{Domain: "127.0.0.1", Selectors: []models.ScrapeSelectors{{Field: "title", Selector: "h1"}}}})

	for _, tc := range []struct{ name, title string }{
		{"short", "A short title"},
		{"100 Japanese characters", strings.Repeat("猫", 100)},
		{"300 English characters", strings.Repeat("a", 300)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title = tc.title
			dir := t.TempDir()
			v := &models.Video{URL: srv.URL + "/watch/1"}
			if err := New().ScrapeCustomSite(t.Context(), v.URL, dir, v, nil); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("wrote %d files, want 1", len(entries))
			}
			if v.Title != tc.title {
				t.Errorf("title = %q, want it whole", v.Title)
			}
		})
	}
}
