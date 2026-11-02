package file

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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
