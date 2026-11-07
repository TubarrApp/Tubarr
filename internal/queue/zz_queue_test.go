package queue

import (
	"io"
	"os"
	"testing"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// TestMain sets up the test environment for the queue package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// TestManualChannelURL tests a channel URL is found by its ID, and an ID the channel doesn't have is refused.
func TestManualChannelURL(t *testing.T) {
	c := &models.Channel{Name: "Cats", URLModels: []*models.ChannelURL{{ID: 4, URL: "https://example.com/@cats"}}}
	if cu, err := ManualChannelURL(nil, c, 4); err != nil || cu != c.URLModels[0] {
		t.Errorf("ManualChannelURL(4) = %v, %v; want the channel's URL", cu, err)
	}
	if _, err := ManualChannelURL(nil, c, 5); err == nil {
		t.Error("ManualChannelURL(5) found a URL the channel doesn't have")
	}
}

// TestWithYtdlpOverrides tests a video's Download As yt-dlp options replace its channel URL's, blank ones keeping the
// channel URL's, without changing the channel URL itself.
func TestWithYtdlpOverrides(t *testing.T) {
	cu := &models.ChannelURL{URL: "https://example.com/@cats", ChanURLSettings: &models.Settings{
		YtdlpOutputExt:            "mp4",
		MaxFilesize:               "1g",
		MaxResolution:             "1080",
		YTDLPPreferredVideoCodecs: []string{"h264"},
		YTDLPPreferredAudioCodecs: []string{"aac"},
	}}
	got := withYtdlpOverrides(cu, &models.VideoOverrides{YtdlpOutputExt: "mkv", MaxResolution: "720", YTDLPPreferredVideoCodecs: []string{"av1", "vp9"}})

	s := got.ChanURLSettings
	if s.YtdlpOutputExt != "mkv" || s.MaxFilesize != "1g" || s.MaxResolution != "720" || len(s.YTDLPPreferredVideoCodecs) != 2 || s.YTDLPPreferredAudioCodecs[0] != "aac" {
		t.Errorf("got settings %+v", s)
	}
	if got.URL != cu.URL {
		t.Errorf("got URL %q, want the channel URL's", got.URL)
	}
	if cu.ChanURLSettings.YtdlpOutputExt != "mp4" || len(cu.ChanURLSettings.YTDLPPreferredVideoCodecs) != 1 {
		t.Errorf("the channel URL's own settings changed: %+v", cu.ChanURLSettings)
	}
}

// waitFor waits until done reports true.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(time.Millisecond)
	}
}
