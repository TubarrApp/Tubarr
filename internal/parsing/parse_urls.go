package parsing

import (
	"bufio"
	"net/url"
	"os"
	"strings"
	"sync"
	"tubarr/internal/utils/logging"
)

type URLParser struct {
	Filepath string
	mu       sync.RWMutex
}

func NewURLParser(fpath string) *URLParser {
	return &URLParser{
		Filepath: fpath,
	}
}

// ParseURLs returns an array of URLs from a file.
// Users should put a single URL on each line in the file.
func (up *URLParser) ParseURLs() ([]string, error) {

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
		if u == "" {
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
