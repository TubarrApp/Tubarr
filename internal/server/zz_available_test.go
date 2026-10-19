package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/app"
	"tubarr/internal/domain/consts"
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

	// Download As is filled with the channel URL's directories and ops, ops without their channel URL prefix, and the
	// video's earlier overrides if it has any.
	type overrideOps struct {
		Defaults models.OverrideLines  `json:"defaults"`
		Saved    *models.OverrideLines `json:"saved"`
	}
	getOverrideOps := func(videoID int64) overrideOps {
		t.Helper()
		w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videoID), "", "")
		var got overrideOps
		if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
			t.Fatalf("override ops for video %d: %d %s", videoID, w.Code, w.Body.String())
		}
		return got
	}
	if got := getOverrideOps(videos[0].ID); got.Defaults.MetaOps != "artist:set:Cats" || got.Defaults.VideoDir != form.Get(jsonkeys.SettingsVideoDirectory) || got.Saved != nil {
		t.Errorf("override ops: got %+v, want the channel URL's and nothing saved", got)
	}
	if w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d", chanID, cuID+1000), "", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for another channel's URL, got %d", w.Code)
	}
	if w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=999999", chanID, cuID), "", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown video, got %d", w.Code)
	}

	// Overrides are rejected when invalid, or not for downloading one video, leaving the videos available.
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"invalid op":  fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"meta_ops":"artist:bogus:x"}}`, videos[0].ID),
		"file as dir": fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"video_dir":%q}}`, videos[0].ID, notADir),
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

	// Download As reports a video already taken elsewhere, rather than looking like it worked.
	if _, err := vs.ClaimVideos(chanID, []int64{videos[0].ID}, app.AvailableStatuses, consts.DLStatusQueued); err != nil {
		t.Fatal(err)
	}
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"filename":"x"}}`, videos[0].ID)); w.Code != http.StatusConflict {
		t.Errorf("already downloading: expected 409, got %d %s", w.Code, w.Body.String())
	}
	if _, err := vs.ClaimVideos(chanID, []int64{videos[0].ID}, []consts.DownloadStatus{consts.DLStatusQueued}, consts.DLStatusFailed); err != nil {
		t.Fatal(err)
	}

	// A video's earlier overrides come with the channel URL's defaults.
	metaOps, err := parsing.ParseMetaOps([]string{"album:set:Pets"})
	if err != nil {
		t.Fatal(err)
	}
	if err := vs.SetVideoOverrides([]int64{videos[0].ID}, &models.VideoOverrides{Filename: "Chosen", OutputDir: "/chosen", MetaOps: metaOps}); err != nil {
		t.Fatal(err)
	}
	if got := getOverrideOps(videos[0].ID); got.Saved == nil || got.Saved.Filename != "Chosen" || got.Saved.OutputDir != "/chosen" || got.Saved.MetaOps != "album:set:Pets" || got.Defaults.MetaOps != "artist:set:Cats" {
		t.Errorf("earlier overrides: got %+v", got)
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

	// Edit & Run stores a downloaded video's edited options before running Metarr, only for a video waiting for it.
	res, err := fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
		VALUES (?, ?, 'https://example.com/@review', 1, 0, 'https://example.com/v/done', ?)`, chanID, cuID, consts.MetarrStatusFailed)
	if err != nil {
		t.Fatal(err)
	}
	pendingID, _ := res.LastInsertId()
	runMetarr := func(body string) int {
		return serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/run-metarr", chanID), "application/json", body).Code
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d,%d],"overrides":{"output_dir":"/x"}}`, pendingID, videos[1].ID)); code != http.StatusBadRequest {
		t.Errorf("overrides for two videos: got %d, want 400", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"meta_ops":"artist:bogus:x"}}`, pendingID)); code != http.StatusBadRequest {
		t.Errorf("invalid op: got %d, want 400", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"output_dir":"/x"}}`, videos[1].ID)); code != http.StatusConflict {
		t.Errorf("video not waiting for Metarr: got %d, want 409", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"output_dir":"/fixed","meta_ops":"album:set:Pets"}}`, pendingID)); code != http.StatusAccepted {
		t.Fatalf("edit & run: got %d, want 202", code)
	}
	if o, err := vs.GetVideoOverrides(pendingID); err != nil || o == nil || o.OutputDir != "/fixed" || len(o.MetaOps) != 1 {
		t.Errorf("edit & run stored overrides %+v, %v", o, err)
	}

	// Edit & Re-Run takes a video Metarr already processed, only as a re-run of given videos.
	res, err = fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
		VALUES (?, ?, 'https://example.com/@review', 1, 0, 'https://example.com/v/processed', ?)`, chanID, cuID, consts.MetarrStatusDone)
	if err != nil {
		t.Fatal(err)
	}
	doneID, _ := res.LastInsertId()
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"meta_ops":"album:set:Fixed"}}`, doneID)); code != http.StatusConflict {
		t.Errorf("edit & run on a processed video: got %d, want 409", code)
	}
	if code := runMetarr(`{"rerun":true}`); code != http.StatusBadRequest {
		t.Errorf("re-run without videos: got %d, want 400", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"meta_ops":"album:set:Fixed"},"rerun":true}`, doneID)); code != http.StatusAccepted {
		t.Fatalf("edit & re-run: got %d, want 202", code)
	}
	if o, err := vs.GetVideoOverrides(doneID); err != nil || o == nil || len(o.MetaOps) != 1 || o.MetaOps[0].OpValue != "Fixed" {
		t.Errorf("edit & re-run stored overrides %+v, %v", o, err)
	}
}
