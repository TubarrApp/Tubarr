package scraper

import (
	"slices"
	"testing"
)

// TestIgnoreDownloadedURLs tests that known URLs are dropped whatever their scheme or trailing slash, keeping order.
func TestIgnoreDownloadedURLs(t *testing.T) {
	existing := []string{"http://a.example/v/1/", "https://a.example/v/3"}
	got := ignoreDownloadedURLs([]string{
		"https://a.example/v/1",
		"https://a.example/v/2",
		"http://a.example/v/3/",
		"https://a.example/v/4",
	}, existing)
	if want := []string{"https://a.example/v/2", "https://a.example/v/4"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
