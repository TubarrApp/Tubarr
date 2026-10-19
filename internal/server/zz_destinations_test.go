package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"
	"tubarr/internal/repo"
)

// TestUnavailableDestinationsRefused tests that downloads which can't reach their folders are refused when asked for,
// rather than failing later or being dropped: a Download As folder that doesn't exist, and a URL to download while the
// channel's drive is unmounted.
func TestUnavailableDestinationsRefused(t *testing.T) {
	setupFuzzServer(t)
	videoDir := filepath.Join(t.TempDir(), "videos")
	form := url.Values{
		"name":                           {"Unmounted"},
		"urls":                           {"https://example.com/@unmounted"},
		jsonkeys.SettingsVideoDirectory:  {videoDir},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
	}
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	var chanID, cuID int64
	if err := fuzzServer.db.QueryRow("SELECT c.id, u.id FROM channels c JOIN channel_urls u ON u.channel_id = c.id WHERE c.name = 'Unmounted'").Scan(&chanID, &cuID); err != nil {
		t.Fatal(err)
	}
	defer deleteChannelNamed(t, "Unmounted")

	// Download As to a folder that doesn't exist.
	vs := repo.GetVideoStore(fuzzServer.db)
	if _, err := vs.AddAvailableVideos([]*models.Video{{ChannelURLID: cuID, ChannelURL: "https://example.com/@unmounted", URL: "https://example.com/v/u1"}}, chanID); err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM videos WHERE channel_id = ? AND url = 'https://example.com/v/u1'", chanID).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(videoDir, "missing")
	body := map[string]any{"action": "download", "video_ids": []int64{videoID}, "overrides": map[string]string{"video_dir": missing}}
	if w := sendJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), body); w.Code != http.StatusBadRequest {
		t.Errorf("Download As to a missing folder: %d %s, want %d", w.Code, w.Body.String(), http.StatusBadRequest)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Errorf("Download As created the missing folder %q", missing)
	}

	// A URL to download while the drive holding the video folder is unmounted.
	if err := os.Remove(videoDir); err != nil {
		t.Fatal(err)
	}
	body = map[string]any{"videos": []map[string]any{{"url": "https://example.com/v/u2", "channel_url_id": 0}}}
	if w := sendJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/download-urls", chanID), body); w.Code != http.StatusConflict {
		t.Errorf("download URL with the drive unmounted: %d %s, want %d", w.Code, w.Body.String(), http.StatusConflict)
	}
	if _, err := os.Stat(videoDir); err == nil {
		t.Errorf("the unmounted video folder %q was recreated", videoDir)
	}
}
