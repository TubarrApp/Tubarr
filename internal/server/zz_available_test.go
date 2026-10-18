package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
)

// TestAvailableVideosAPI tests a channel added without auto download, and listing and dismissing its available videos.
func TestAvailableVideosAPI(t *testing.T) {
	setupFuzzServer(t)
	serve := func(method, path, contentType, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}

	form := url.Values{
		"name":                           {"Review Channel"},
		"urls":                           {"https://example.com/@review"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
		jsonkeys.MetarrMetaOps:           {"https://example.com/@review|artist:set:Cats"},
	}
	if w := serve(http.MethodPost, "/api/v1/channels/add", "application/x-www-form-urlencoded", form.Encode()); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}

	var chanID, cuID int64
	if err := fuzzServer.db.QueryRow("SELECT c.id, u.id FROM channels c JOIN channel_urls u ON u.channel_id = c.id WHERE c.name = 'Review Channel' AND u.url = 'https://example.com/@review'").Scan(&chanID, &cuID); err != nil {
		t.Fatal(err)
	}

	// The setting is stored and returned.
	w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d", chanID), "", "")
	var channel struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &channel); err != nil {
		t.Fatal(err)
	}
	if channel.Settings[jsonkeys.SettingsAutoDownload] != false {
		t.Errorf("expected auto_download false, got settings %v", channel.Settings)
	}

	vs := repo.GetVideoStore(fuzzServer.db)
	if _, err := vs.AddAvailableVideos([]*models.Video{
		{ChannelURLID: cuID, ChannelURL: "https://example.com/@review", URL: "https://example.com/v/a", Title: "A", ThumbnailURL: "https://example.com/a.jpg"},
		{ChannelURLID: cuID, ChannelURL: "https://example.com/@review", URL: "https://example.com/v/b", Title: "B"},
	}, chanID); err != nil {
		t.Fatal(err)
	}

	listAvailable := func() []models.AvailableVideo {
		t.Helper()
		w := serve(http.MethodGet, "/api/v1/videos/available", "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("listing available videos: %d %s", w.Code, w.Body.String())
		}
		var videos []models.AvailableVideo
		if err := json.Unmarshal(w.Body.Bytes(), &videos); err != nil {
			t.Fatal(err)
		}
		return videos
	}
	videos := listAvailable()
	if len(videos) != 2 || videos[0].ChannelName != "Review Channel" {
		t.Fatalf("expected 2 available videos from Review Channel, got %+v", videos)
	}

	// Download As is filled with the channel URL's ops, without their channel URL prefix.
	w = serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d", chanID, cuID), "", "")
	var lines models.OverrideLines
	if err := json.Unmarshal(w.Body.Bytes(), &lines); w.Code != http.StatusOK || err != nil || lines.MetaOps != "artist:set:Cats" {
		t.Errorf("override ops: %d %s, want meta ops %q", w.Code, w.Body.String(), "artist:set:Cats")
	}
	if w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d", chanID, cuID+1000), "", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for another channel's URL, got %d", w.Code)
	}

	// Overrides are rejected when invalid, or not for downloading one video, leaving the videos available.
	for name, body := range map[string]string{
		"invalid op":  fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"meta_ops":"artist:bogus:x"}}`, videos[0].ID),
		"two videos":  fmt.Sprintf(`{"action":"download","video_ids":[%d,%d],"overrides":{"filename":"x"}}`, videos[0].ID, videos[1].ID),
		"dismiss":     fmt.Sprintf(`{"action":"dismiss","video_ids":[%d],"overrides":{"filename":"x"}}`, videos[0].ID),
		"unknown key": fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"name":"x"}}`, videos[0].ID),
	} {
		if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d %s", name, w.Code, w.Body.String())
		}
	}
	if remaining := listAvailable(); len(remaining) != 2 {
		t.Fatalf("expected rejected overrides to leave 2 videos available, got %d", len(remaining))
	}

	// A video's earlier overrides fill the dialog in place of the channel URL's ops.
	metaOps, err := parsing.ParseMetaOps([]string{"album:set:Pets"})
	if err != nil {
		t.Fatal(err)
	}
	if err := vs.SetVideoOverrides([]int64{videos[0].ID}, &models.VideoOverrides{Filename: "Chosen", MetaOps: metaOps}); err != nil {
		t.Fatal(err)
	}
	w = serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videos[0].ID), "", "")
	lines = models.OverrideLines{}
	if err := json.Unmarshal(w.Body.Bytes(), &lines); err != nil || lines.Filename != "Chosen" || lines.MetaOps != "album:set:Pets" {
		t.Errorf("earlier overrides: %d %s", w.Code, w.Body.String())
	}
	w = serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videos[1].ID), "", "")
	lines = models.OverrideLines{}
	if err := json.Unmarshal(w.Body.Bytes(), &lines); err != nil || lines.MetaOps != "artist:set:Cats" {
		t.Errorf("video without overrides: %d %s, want the channel URL's ops", w.Code, w.Body.String())
	}
	if w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=999999", chanID, cuID), "", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown video, got %d", w.Code)
	}

	// Unknown actions are rejected, and dismissing removes the video from the list.
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"delete","video_ids":[%d]}`, videos[0].ID)); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an unknown action, got %d", w.Code)
	}
	w = serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"dismiss","video_ids":[%d]}`, videos[0].ID))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("dismissing: %d %s", w.Code, w.Body.String())
	}
	if remaining := listAvailable(); len(remaining) != 1 || remaining[0].ID != videos[1].ID {
		t.Errorf("expected only video %d to remain, got %+v", videos[1].ID, remaining)
	}
}
