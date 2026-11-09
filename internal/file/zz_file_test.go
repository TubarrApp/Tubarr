package file

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
)

// ******************************** Reading ops files *********************************************************************************

// TestReadOpLinesKeepsTrailingSpaces covers an operation value ending in a space, such
// as "title:keep-after:| ", surviving being read from a file.
func TestReadOpLinesKeepsTrailingSpaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.txt")
	content := "  title:keep-after:| \r\n\n   \n# comment\ntitle:suffix:!\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadOpLines(path)
	if err != nil {
		t.Fatalf("ReadOpLines: %v", err)
	}
	if want := []string{"title:keep-after:| ", "title:suffix:!"}; !slices.Equal(got, want) {
		t.Errorf("ReadOpLines = %q, want %q", got, want)
	}

	if got, _ := ReadFileLines(path); got[0] != "title:keep-after:|" {
		t.Errorf("ReadFileLines should still trim, got %q", got[0])
	}
}

// ******************************** Thumbnail cache ***********************************************************************************

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

// ******************************** Thumbnail types ***********************************************************************************

// TestThumbnailUploadTypesMatchDialog tests the Download As dialog only offers image types the upload accepts, and names them.
func TestThumbnailUploadTypesMatchDialog(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "web", "partials", "download-as.html"))
	if err != nil {
		t.Fatal(err)
	}
	accept := regexp.MustCompile(`accept="([^"]+)"`).FindSubmatch(page)
	if accept == nil {
		t.Fatal("the dialog's thumbnail input has no accept list")
	}
	offered := strings.Split(string(accept[1]), ",")
	if len(offered) != len(thumbnailExts) {
		t.Errorf("the dialog offers %v, the upload accepts %v", offered, thumbnailExts)
	}
	for _, mime := range offered {
		if _, ok := thumbnailExts[mime]; !ok {
			t.Errorf("the dialog offers %q, which the upload refuses", mime)
		}
	}
	if strings.Contains(string(page), "WebP") {
		t.Error("the dialog still names WebP, which the upload refuses")
	}
}
