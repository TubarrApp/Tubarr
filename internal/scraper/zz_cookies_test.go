package scraper

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestCookieFileWritable tests that the cookie file is saved writable by its owner, replacing a read-only one. yt-dlp
// writes its cookies back to the file after each run, and exits with an error if it can't.
func TestCookieFileWritable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte("# Netscape HTTP Cookie File\n"), 0o400); err != nil {
		t.Fatal(err)
	}

	if err := saveCookiesToFile([]*http.Cookie{{Name: "a", Value: "1", Domain: "example.com", Path: "/"}}, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o600 != 0o600 {
		t.Errorf("cookie file saved with mode %o, want it readable and writable by its owner", mode)
	}
}
