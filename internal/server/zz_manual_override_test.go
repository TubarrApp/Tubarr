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
	"tubarr/internal/app"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"
	"tubarr/internal/repo"
)

// TestManualAvailableOverrideOps tests Download As opens for a manual video whose channel URL is gone, using the channel's
// settings as its download does.
func TestManualAvailableOverrideOps(t *testing.T) {
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
		"name":                           {"Manual Override Channel"},
		"urls":                           {"https://example.com/@manualoverride"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.MetarrMetaOps:           {"artist:set:Cats"},
	}
	if w := serve(http.MethodPost, "/api/v1/channels/add", "application/x-www-form-urlencoded", form.Encode()); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	s, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	c, found, err := s.ChannelStore().GetChannelModel(consts.QChanName, "Manual Override Channel", true)
	if err != nil || !found {
		t.Fatalf("loading channel: %v %v", found, err)
	}

	if _, _, err := app.AddManualAvailable(s, c, []models.ManualVideo{{URL: "https://example.com/v/manual"}}); err != nil {
		t.Fatal(err)
	}
	// Saving a channel's edit form once deleted its manual downloads URL, which isn't listed in the form, leaving its
	// videos with no channel URL.
	if _, err := fuzzServer.db.Exec("DELETE FROM channel_urls WHERE channel_id = ? AND is_manual = 1", c.ID); err != nil {
		t.Fatal(err)
	}

	w := serve(http.MethodGet, "/api/v1/videos/available", "", "")
	var videos []models.AvailableVideo
	if err := json.Unmarshal(w.Body.Bytes(), &videos); err != nil {
		t.Fatal(err)
	}
	var v *models.AvailableVideo
	for i := range videos {
		if videos[i].URL == "https://example.com/v/manual" {
			v = &videos[i]
		}
	}
	if v == nil {
		t.Fatalf("manual video isn't listed as available: %+v", videos)
	}

	w = serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", v.ChannelID, v.ChannelURLID, v.ID), "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("override ops for a manual video (channel URL ID %d): %d %s", v.ChannelURLID, w.Code, w.Body.String())
	}
	var got struct {
		Defaults models.OverrideLines `json:"defaults"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Defaults.MetaOps != "artist:set:Cats" {
		t.Errorf("manual video's Download As defaults = %+v, want the channel's ops", got.Defaults)
	}
}
