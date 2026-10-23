package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"
	"tubarr/internal/repo"
	"tubarr/internal/state"
)

// TestDeleteChannelStopsItsJob tests that deleting a channel aborts its running crawl or download job.
func TestDeleteChannelStopsItsJob(t *testing.T) {
	setupFuzzServer(t)
	form := url.Values{
		"name":                           {"Delete Me"},
		"urls":                           {"https://example.com/@deleteme"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, req)
	if w.Code >= 300 {
		t.Fatalf("adding the channel failed: %d %s", w.Code, w.Body.String())
	}
	var id int64
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", consts.QChanID, consts.DBChannels, consts.QChanName)
	if err := fuzzServer.db.QueryRow(query, "Delete Me").Scan(&id); err != nil {
		t.Fatal(err)
	}

	ctx, unlock, ok := state.TryLockChannel(context.Background(), id) // Stands in for a running job.
	if !ok {
		t.Fatal("couldn't lock the new channel's job")
	}
	defer unlock()

	w = httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	if ctx.Err() == nil {
		t.Error("deleting the channel left its running job going")
	}
}

// TestEditRenamesChannelURL tests an edited channel URL keeps its videos, and orphaned videos are reconnected.
func TestEditRenamesChannelURL(t *testing.T) {
	setupFuzzServer(t)
	const mobile, desktop = "https://m.example.com/@renamed/videos", "https://www.example.com/@renamed/videos"
	form := func(name, u string) url.Values {
		return url.Values{
			"name":                           {name},
			"urls":                           {u},
			jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
			jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
			jsonkeys.SettingsMaxConcurrency:  {"1"},
			jsonkeys.SettingsCrawlFreq:       {"30"},
			jsonkeys.SettingsDownloadRetries: {"1"},
			jsonkeys.SettingsAutoDownload:    {"false"},
		}
	}
	send := func(method, path string, values url.Values) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		if w.Code >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
	}
	s, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	// addWithVideo adds a channel with the mobile address, and a crawled video for it.
	addWithVideo := func(name string) (chanID, cuID, videoID int64) {
		t.Helper()
		send(http.MethodPost, "/api/v1/channels/add", form(name, mobile))
		c, _, err := s.ChannelStore().GetChannelModel(consts.QChanName, name, true)
		if err != nil {
			t.Fatal(err)
		}
		cu := c.URLModels[0]
		videos := []*models.Video{{ChannelURLID: cu.ID, ChannelURL: cu.URL, URL: "https://example.com/v/" + name}}
		if _, err := s.VideoStore().AddAvailableVideos(videos, c.ID); err != nil {
			t.Fatal(err)
		}
		return c.ID, cu.ID, videos[0].ID
	}
	videoChannelURL := func(videoID int64) (id sql.NullInt64, u string) {
		t.Helper()
		if err := fuzzServer.db.QueryRow("SELECT channel_url_id, channel_url FROM videos WHERE id = ?", videoID).Scan(&id, &u); err != nil {
			t.Fatal(err)
		}
		return id, u
	}

	// The edit page sends the address change as a rename.
	chanID, cuID, videoID := addWithVideo("Renamed In Place")
	edit := form("Renamed In Place", desktop)
	edit.Set("url_renames", fmt.Sprintf(`{%q:%q}`, mobile, desktop))
	send(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", chanID), edit)
	var renamedID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = ? AND url = ?", chanID, desktop).Scan(&renamedID); err != nil || renamedID != cuID {
		t.Errorf("renamed URL has ID %d (%v), want its original %d", renamedID, err, cuID)
	}
	if id, u := videoChannelURL(videoID); id.Int64 != cuID || u != desktop {
		t.Errorf("video's channel URL = %d %q, want %d %q", id.Int64, u, cuID, desktop)
	}

	// Without the rename the old URL is replaced, orphaning its video until it's reconnected by address.
	chanID, _, videoID = addWithVideo("Replaced")
	send(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", chanID), form("Replaced", desktop))
	if id, _ := videoChannelURL(videoID); id.Valid {
		t.Fatalf("video kept channel URL %d after its URL was replaced", id.Int64)
	}
	if _, err := s.VideoStore().ReconnectOrphanedVideos(); err != nil {
		t.Fatal(err)
	}
	var newID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = ? AND url = ?", chanID, desktop).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	if id, u := videoChannelURL(videoID); id.Int64 != newID || u != desktop {
		t.Errorf("orphaned video reconnected to %d %q, want %d %q", id.Int64, u, newID, desktop)
	}
}
