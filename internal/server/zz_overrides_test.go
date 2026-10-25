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

// TestParseOverrideLinesChecksMetarrArgs tests that Download As options are checked and normalized as saved settings are.
func TestParseOverrideLinesChecksMetarrArgs(t *testing.T) {
	bad := "xyz"
	if _, err := parseOverrideLines(&models.OverrideLines{}, &metarrArgsInput{OutputExt: &bad}); err == nil {
		t.Error("accepted Metarr output extension xyz")
	}

	upper := "MKV"
	o, err := parseOverrideLines(&models.OverrideLines{}, &metarrArgsInput{OutputExt: &upper})
	if err != nil {
		t.Fatal(err)
	}
	if o.OutputExt != "mkv" {
		t.Errorf("Metarr output extension %q not lowercased, got %q", upper, o.OutputExt)
	}
}

// TestParseOverrideLinesYtdlpOptions tests that Download As yt-dlp options are read one codec per line, normalized and
// checked as saved settings are.
func TestParseOverrideLinesYtdlpOptions(t *testing.T) {
	o, err := parseOverrideLines(&models.OverrideLines{
		YtdlpOutputExt:            "mkv",
		MaxFilesize:               "2G",
		ExtraYTDLPVideoArgs:       " --no-mtime ",
		YTDLPPreferredVideoCodecs: "AV1\n\nvp9",
		YTDLPPreferredAudioCodecs: "opus",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.YtdlpOutputExt != "mkv" || o.MaxFilesize != "2g" || o.ExtraYTDLPVideoArgs != "--no-mtime" ||
		strings.Join(o.YTDLPPreferredVideoCodecs, ",") != "av1,vp9" || strings.Join(o.YTDLPPreferredAudioCodecs, ",") != "opus" {
		t.Errorf("got %+v", o)
	}

	for _, bad := range []models.OverrideLines{{YtdlpOutputExt: "xyz"}, {MaxFilesize: "lots"}, {YTDLPPreferredVideoCodecs: "betamax"}} {
		if _, err := parseOverrideLines(&bad, nil); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

// TestCheckOverridesFit tests that Download As options are checked together with the channel URL's they leave blank.
func TestCheckOverridesFit(t *testing.T) {
	c := &models.Channel{URLModels: []*models.ChannelURL{
		{ID: 1, ChanURLMetarrArgs: &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}},
		{ID: 2, ChanURLMetarrArgs: &models.MetarrArgs{OutputExt: "webm"}},
	}}
	tests := []struct {
		name  string
		urlID int64
		o     models.MetarrArgs
		fits  bool
	}{
		{"webm over the URL's h264", 1, models.MetarrArgs{OutputExt: "webm"}, false},
		{"webm replacing the URL's h264 with vp9", 1, models.MetarrArgs{OutputExt: "webm", TranscodeVideoCodecs: []string{"vp9"}}, true},
		{"h264 under the URL's webm", 2, models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}, false},
		{"mp4 replacing the URL's webm", 2, models.MetarrArgs{OutputExt: "mp4", TranscodeVideoCodecs: []string{"h264"}}, true},
		{"unknown URL", 3, models.MetarrArgs{OutputExt: "webm"}, true},
	}
	for _, tt := range tests {
		err := checkOverridesFit(nil, c, tt.urlID, &models.VideoOverrides{MetarrArgs: tt.o})
		if (err == nil) != tt.fits {
			t.Errorf("%s: got error %v, want fits %t", tt.name, err, tt.fits)
		}
	}
}

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
