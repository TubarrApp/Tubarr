package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestResponsesCompressed tests that a browser accepting gzip gets it, with the same content, and that a client not
// accepting it gets plain responses.
func TestResponsesCompressed(t *testing.T) {
	setupFuzzServer(t)
	const path = "/api/v1/channels/all"

	get := func(encoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s with Accept-Encoding %q: status %d", path, encoding, w.Code)
		}
		return w
	}

	plain := get("")
	if got := plain.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("no gzip asked for, got Content-Encoding %q", got)
	}
	if !json.Valid(plain.Body.Bytes()) {
		t.Fatalf("plain response isn't JSON: %q", plain.Body.String())
	}

	zipped := get("gzip")
	if got := zipped.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("gzip asked for, got Content-Encoding %q", got)
	}
	zr, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unzipped, plain.Body.Bytes()) {
		t.Errorf("unzipped response differs from the plain one:\n%s\n%s", unzipped, plain.Body.String())
	}
}

// TestStaticFilesCompressedAndCached tests that a static script is compressed while the browser can still revalidate
// its cached copy, and that images aren't compressed again.
func TestStaticFilesCompressedAndCached(t *testing.T) {
	setupFuzzServer(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	webDir := filepath.Join(filepath.Dir(exe), "web")
	script := strings.Repeat("console.log('compress me');\n", 500)
	if err := os.WriteFile(filepath.Join(webDir, "zz-compress.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "zz-compress.png"), []byte("\x89PNG\r\n\x1a\n"+script), 0o644); err != nil {
		t.Fatal(err)
	}

	get := func(path, ifModifiedSince string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		if ifModifiedSince != "" {
			req.Header.Set("If-Modified-Since", ifModifiedSince)
		}
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}

	w := get("/zz-compress.js", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("script: status %d, Content-Encoding %q, want 200 and gzip", w.Code, w.Header().Get("Content-Encoding"))
	}
	if w.Body.Len() >= len(script)/2 {
		t.Errorf("script sent as %d of its %d bytes", w.Body.Len(), len(script))
	}
	lastModified := w.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("script has no Last-Modified to revalidate with")
	}

	later := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	if w := get("/zz-compress.js", later); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Errorf("revalidating the script: status %d with %d bytes, want 304 and none", w.Code, w.Body.Len())
	}

	if w := get("/zz-compress.png", ""); w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "" {
		t.Errorf("image: status %d, Content-Encoding %q, want 200 and none", w.Code, w.Header().Get("Content-Encoding"))
	}
}
