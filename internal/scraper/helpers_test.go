package scraper

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestVideoEntriesNestedTabs checks videos are found inside a channel's tab playlists, as yt-dlp returns for a channel root URL.
func TestVideoEntriesNestedTabs(t *testing.T) {
	const out = `{"entries":[
		{"_type":"playlist","url":null,"entries":[{"url":"https://www.youtube.com/watch?v=a"},{"url":"https://www.youtube.com/watch?v=b"}]},
		{"_type":"playlist","url":null,"entries":[{"url":"https://www.youtube.com/shorts/c"}]},
		{"_type":"url","url":"https://www.youtube.com/watch?v=d"}
	]}`
	var result ytDlpEntry
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, e := range result.videoEntries() {
		got = append(got, e.URL)
	}
	want := []string{
		"https://www.youtube.com/watch?v=a",
		"https://www.youtube.com/watch?v=b",
		"https://www.youtube.com/shorts/c",
		"https://www.youtube.com/watch?v=d",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
