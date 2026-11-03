package repo

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"tubarr/internal/models"
)

// TestDeleteFilesTakesNFO tests that deleting a video's files takes the NFO file named for it and nothing else, and that
// deleting a video alone leaves all its files.
func TestDeleteFilesTakesNFO(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	dir := t.TempDir()

	// write makes the named files in the test's folder.
	write := func(names ...string) {
		t.Helper()
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// addVideo saves a finished video with the given files.
	addVideo := func(url, videoFile, jsonFile string) {
		t.Helper()
		v := &models.Video{URL: url, ChannelURL: "https://example.com/@a", VideoFilePath: filepath.Join(dir, videoFile), JSONFilePath: filepath.Join(dir, jsonFile)}
		v.MarkVideoAsComplete()
		if _, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID); err != nil {
			t.Fatal(err)
		}
		if err := vs.UpdateVideo(v, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	// remaining returns the files left in the test's folder.
	remaining := func() []string {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	// Neighbours with similar names, which aren't the video's.
	neighbours := []string{"Ep.nfo", "Ep. 5 - Cats extra.nfo", "Ep. 5 - Cats.mp4.nfo", "other.mp4", "other.nfo"}
	write(neighbours...)

	// Deleting the video alone leaves its files.
	write("kept.mp4", "kept.json", "kept.nfo")
	addVideo("https://example.com/v/kept", "kept.mp4", "kept.json")
	if err := cs.DeleteVideosByURLs(c.ID, []string{"https://example.com/v/kept"}, false); err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(slices.Values(append([]string{"kept.mp4", "kept.json", "kept.nfo"}, neighbours...)))
	if got := remaining(); !slices.Equal(got, want) {
		t.Errorf("deleting without files:\ngot  %q\nwant %q", got, want)
	}

	// Deleting with files takes the video, its metadata and its NFO, only the last dot starting the extension.
	write("Ep. 5 - Cats.mp4", "Ep. 5 - Cats.json", "Ep. 5 - Cats.nfo")
	addVideo("https://example.com/v/dotted", "Ep. 5 - Cats.mp4", "Ep. 5 - Cats.json")
	if err := cs.DeleteVideosByURLs(c.ID, []string{"https://example.com/v/dotted"}, true); err != nil {
		t.Fatal(err)
	}
	if got := remaining(); !slices.Equal(got, want) {
		t.Errorf("deleting with files:\ngot  %q\nwant %q", got, want)
	}

	// A video without an NFO file deletes as before.
	write("plain.mp4", "plain.json")
	addVideo("https://example.com/v/plain", "plain.mp4", "plain.json")
	if err := cs.DeleteVideosByURLs(c.ID, []string{"https://example.com/v/plain"}, true); err != nil {
		t.Fatal(err)
	}
	if got := remaining(); !slices.Equal(got, want) {
		t.Errorf("deleting a video with no NFO file:\ngot  %q\nwant %q", got, want)
	}
}

// TestVideoFilePathsNFONeedsVideo tests a video with no saved video path gets no NFO path, which would otherwise be a
// file called ".nfo" in the working directory.
func TestVideoFilePathsNFONeedsVideo(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	const videoURL = "https://example.com/v/json-only"
	v := &models.Video{URL: videoURL, ChannelURL: "https://example.com/@a", JSONFilePath: "/json/only.json"}
	if _, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		t.Fatal(err)
	}

	got, err := cs.videoFilePaths(c.ID, []string{videoURL})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/json/only.json"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
