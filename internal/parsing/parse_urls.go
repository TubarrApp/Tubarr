package parsing

import (
	"bufio"
	"net/url"
	"os"
	"strings"
	"sync"
	"tubarr/internal/utils/logging"
)

type URLFileParser struct {
	Filepath string
	mu       sync.RWMutex
}

// NewURLFileParser returns an instance of a URLFileParser.
//
// This is used to parse URLs from a file.
func NewURLFileParser(fpath string) *URLFileParser {
	return &URLFileParser{
		Filepath: fpath,
	}
}

// ParseURLs returns an array of URLs from a file.
//
// Users should put a single URL on each line in the file for proper parsing.
// Hashtags should work to exclude lines (i.e. '# Comment').
func (up *URLFileParser) ParseURLs() ([]string, error) {

	up.mu.RLock()
	defer up.mu.RUnlock()

	f, err := os.Open(up.Filepath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	urls := make(map[string]struct{})
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		u := strings.TrimSpace(scanner.Text())
		if u == "" || strings.HasPrefix(u, "#") {
			continue
		}

		parsedURL, err := url.Parse(u)
		if err != nil {
			logging.E(0, "URL %q is invalid: %v", u, err)
			continue
		}
		urls[parsedURL.String()] = struct{}{}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	result := make([]string, 0, len(urls))
	for url := range urls {
		result = append(result, url)
	}

	return result, nil
}
