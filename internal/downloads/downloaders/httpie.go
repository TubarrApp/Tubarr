package downloaders

import (
	"fmt"
	"os"
	"path/filepath"
)

// HttpieConfigDir creates a temporary httpie config directory (for HTTPIE_CONFIG_DIR) making httpie ignore stdin.
//
// Without a terminal on stdin, httpie reads it as a request body (turning yt-dlp's GET into a POST),
// and yt-dlp doesn't pass --external-downloader-args to httpie, so "--ignore-stdin" must come from config.
func HttpieConfigDir() (dir string, err error) {
	if dir, err = os.MkdirTemp("", "tubarr-httpie-*"); err != nil {
		return "", fmt.Errorf("failed to create httpie config directory: %w", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"default_options": ["--ignore-stdin"]}`), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("failed to write httpie config: %w", err)
	}
	return dir, nil
}
