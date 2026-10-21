package scraper

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"
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

	entries := result.videoEntries()
	got := make([]string, 0, len(entries))
	for _, e := range entries {
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

// TestListedInOrder checks crawled videos come back in the order the channel lists them, with a repeated URL kept at
// its first position and its details merged.
func TestListedInOrder(t *testing.T) {
	iterationCount := 30
	want := make([]string, 0, iterationCount)
	urls := make(map[string]*listedVideo)
	for i := range iterationCount {
		u := fmt.Sprintf("https://example.com/v/%d", i)
		want = append(want, u)
		mergeListed(urls, u, &listedVideo{})
	}
	mergeListed(urls, want[3], &listedVideo{Title: "Repeat"})

	if got := listedInOrder(urls); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if lv := urls[want[3]]; lv.Order != 3 || lv.Title != "Repeat" {
		t.Errorf("repeated URL: got order %d, title %q, want 3, %q", lv.Order, lv.Title, "Repeat")
	}
}

// TestListedFlatEntry checks a flat playlist entry's title, best thumbnail, duration, and date are kept.
func TestListedFlatEntry(t *testing.T) {
	const out = `{"entries":[
		{"url":"a","title":"A","duration":312.5,"upload_date":"20260102","thumbnails":[{"url":"small.jpg"},{"url":"best.jpg"}]},
		{"url":"b","thumbnail":"main.jpg","timestamp":1767312000},
		{"url":"c"}
	]}`
	var result ytDlpEntry
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	entries := result.videoEntries()

	a := entries[0].listed()
	if a.Title != "A" || a.ThumbnailURL != "best.jpg" || a.Duration != 312 || !a.UploadDate.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("entry a: got %+v", a)
	}
	b := entries[1].listed()
	if b.ThumbnailURL != "main.jpg" || !b.UploadDate.Equal(time.Unix(1767312000, 0)) {
		t.Errorf("entry b: got %+v", b)
	}
	if c := entries[2].listed(); *c != (listedVideo{}) {
		t.Errorf("entry c: expected no details, got %+v", c)
	}
}
