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
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"
	"tubarr/internal/repo"
)

// TestEditAndRunFollowsChannelURLOps tests Edit & Run keeps the channel URL's ops with a video's, so reopening it fills
// in the ops files' lines fresh, keeping a line added for the video.
func TestEditAndRunFollowsChannelURLOps(t *testing.T) {
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
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	form := url.Values{
		"name":                           {"Follow Ops Channel"},
		"urls":                           {"https://example.com/@followops"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
		jsonkeys.MetarrMetaOps:           {"artist:set:Cats"},
		jsonkeys.MetarrMetaOpsFile:       {write("meta.txt", "album:set:Old\n")},
		jsonkeys.MetarrFilenameOpsFile:   {write("filename.txt", "suffix: [Old]\n")},
	}
	if w := serve(http.MethodPost, "/api/v1/channels/add", "application/x-www-form-urlencoded", form.Encode()); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	var chanID, cuID int64
	if err := fuzzServer.db.QueryRow("SELECT c.id, u.id FROM channels c JOIN channel_urls u ON u.channel_id = c.id WHERE c.name = 'Follow Ops Channel' AND u.url = 'https://example.com/@followops'").Scan(&chanID, &cuID); err != nil {
		t.Fatal(err)
	}
	res, err := fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
		VALUES (?, ?, 'https://example.com/@followops', 1, 0, 'https://example.com/v/follow', ?)`, chanID, cuID, consts.MetarrStatusFailed)
	if err != nil {
		t.Fatal(err)
	}
	videoID, _ := res.LastInsertId()

	type overrideOps struct {
		Defaults models.OverrideLines  `json:"defaults"`
		Saved    *models.OverrideLines `json:"saved"`
	}
	open := func() overrideOps {
		t.Helper()
		w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videoID), "", "")
		var got overrideOps
		if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
			t.Fatalf("override ops: %d %s", w.Code, w.Body.String())
		}
		return got
	}

	// Edit & Run with the meta ops left as the channel URL's and a filename op added.
	first := open()
	body, err := json.Marshal(map[string]any{
		"video_ids": []int64{videoID},
		"overrides": models.OverrideLines{MetaOps: first.Defaults.MetaOps, FilenameOps: first.Defaults.FilenameOps + "\nprefix:[Mine] "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/run-metarr", chanID), "application/json", string(body)); w.Code != http.StatusAccepted {
		t.Fatalf("edit & run: %d %s", w.Code, w.Body.String())
	}
	o, err := repo.GetVideoStore(fuzzServer.db).GetVideoOverrides(videoID)
	if err != nil || o == nil || o.ChannelURLOps == nil || o.ChannelURLOps.MetaOps != first.Defaults.MetaOps {
		t.Fatalf("stored overrides %+v, %v, want the channel URL's ops kept with them", o, err)
	}

	// The ops files change.
	write("meta.txt", "album:set:New\n")
	write("filename.txt", "suffix: [New]\n")
	second := open()
	if !strings.Contains(second.Defaults.MetaOps, "album:set:New") {
		t.Fatalf("channel URL's meta ops %q, want the file's new op", second.Defaults.MetaOps)
	}
	if second.Saved == nil {
		t.Fatal("reopened with nothing saved")
	}
	if second.Saved.MetaOps != second.Defaults.MetaOps {
		t.Errorf("meta ops left as the channel URL's: got %q, want its ops now, %q", second.Saved.MetaOps, second.Defaults.MetaOps)
	}
	if want := "prefix:[Mine] \nsuffix: [New]"; second.Saved.FilenameOps != want {
		t.Errorf("filename ops with an added op: got %q, want %q, the added op kept and the file's suffix swapped", second.Saved.FilenameOps, want)
	}
}
