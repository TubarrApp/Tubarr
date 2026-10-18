package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
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

	ctx, unlock, ok := state.TryLockCrawl(context.Background(), id) // Stands in for a running crawl.
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
