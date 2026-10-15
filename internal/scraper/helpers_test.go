package scraper

import (
	"encoding/json"
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
