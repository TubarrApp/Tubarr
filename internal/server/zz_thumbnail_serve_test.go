package server

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// noisyJPEG returns a JPEG well over the 512 bytes sniffed for its type.
func noisyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	r := rand.New(rand.NewSource(1))
	for y := range 200 {
		for x := range 200 {
			img.Set(x, y, color.RGBA{uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256)), 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestServeThumbnailContentType tests a thumbnail is sent as an image, whole, with or without an extension (cached ones
// have none).
func TestServeThumbnailContentType(t *testing.T) {
	data := noisyJPEG(t)
	dir := t.TempDir()
	for _, name := range []string{"thumb.jpg", "0123456789abcdef"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			serveThumbnailFile(w, httptest.NewRequest(http.MethodGet, "/", nil), path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if got := w.Header().Get("Content-Type"); got != "image/jpeg" {
				t.Errorf("Content-Type = %q, want image/jpeg", got)
			}
			if !bytes.Equal(w.Body.Bytes(), data) {
				t.Errorf("sent %d bytes, want the whole %d byte image", w.Body.Len(), len(data))
			}
		})
	}
}

// TestServeThumbnailRefusesOtherFiles tests a file that isn't an image isn't served, as the path may be anything typed in.
func TestServeThumbnailRefusesOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.txt")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	serveThumbnailFile(w, httptest.NewRequest(http.MethodGet, "/", nil), path)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
