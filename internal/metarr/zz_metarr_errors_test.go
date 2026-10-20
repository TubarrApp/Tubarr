package metarr

import (
	"slices"
	"testing"
)

// TestMetarrErrors tests that Metarr's reported errors are read from its output once each, ignoring other lines.
func TestMetarrErrors(t *testing.T) {
	stdout := "final video path: /a.mp4\n" +
		"metarr error: video codec \"h264\" can't go in a webm file\n" +
		"metarr error: \n" +
		"some log line mentioning metarr error: inside\n" +
		"metarr error: video codec \"h264\" can't go in a webm file\n" +
		"metarr error: disk full"
	want := []string{`video codec "h264" can't go in a webm file`, "disk full"}
	if got := metarrErrors(stdout); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := metarrErrors("final video path: /a.mp4\n"); got != nil {
		t.Errorf("got %q from output with no errors, want none", got)
	}
}
