package file

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
)

// TestCacheThumbnail tests that thumbnails are fetched once, and that only images are cached.
func TestCacheThumbnail(t *testing.T) {
	oldDir := paths.CacheThumbnailDir
	paths.CacheThumbnailDir = filepath.Join(t.TempDir(), "cache", "thumbnails")
	t.Cleanup(func() { paths.CacheThumbnailDir = oldDir })

	png := []byte("\x89PNG\r\n\x1a\n0000000000000000")
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/img.png":
			_, _ = w.Write(png)
		case "/page":
			_, _ = w.Write([]byte("<html>not an image</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	first, err := CacheThumbnail(context.Background(), srv.URL+"/img.png")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	second, err := CacheThumbnail(context.Background(), srv.URL+"/img.png")
	if err != nil || second != first || hits != 1 {
		t.Fatalf("second fetch should come from the cache: err=%v paths=%q,%q hits=%d", err, first, second, hits)
	}
	if b, err := os.ReadFile(first); err != nil || string(b) != string(png) {
		t.Fatalf("cached file = %q, %v; want the served image", b, err)
	}

	for _, p := range []string{"/page", "/missing"} {
		if _, err := CacheThumbnail(context.Background(), srv.URL+p); err == nil {
			t.Errorf("%s was cached", p)
		}
	}
	if entries, _ := os.ReadDir(paths.CacheThumbnailDir); len(entries) != 1 {
		t.Errorf("cache holds %d files, want 1 (no rejected or temporary files left)", len(entries))
	}
}

// TestRemoveUnusedCachedThumbnails tests that startup cleanup keeps cached thumbnails a video's URL uses, and only those.
func TestRemoveUnusedCachedThumbnails(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: io.Discard}
	t.Cleanup(func() { logger.Pl = origLogger })
	oldDir := paths.CacheThumbnailDir
	paths.CacheThumbnailDir = filepath.Join(t.TempDir(), "cache", "thumbnails")
	t.Cleanup(func() { paths.CacheThumbnailDir = oldDir })
	if err := os.MkdirAll(paths.CacheThumbnailDir, 0o700); err != nil {
		t.Fatal(err)
	}

	crawled, override := "https://i.example/crawled.jpg", "https://i.example/override.jpg"
	keep := []string{cachedThumbnailPath(crawled), cachedThumbnailPath(override)}
	drop := []string{cachedThumbnailPath("https://i.example/gone.jpg"), filepath.Join(paths.CacheThumbnailDir, ".tmp-123")}
	for _, p := range append(append([]string{}, keep...), drop...) {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	RemoveUnusedCachedThumbnails([]string{crawled}, map[int64]models.VideoOverrides{1: {Thumbnail: override}, 2: {StripThumbnail: true}})
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("in-use %s was removed: %v", p, err)
		}
	}
	for _, p := range drop {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("unused %s was kept", p)
		}
	}
}
