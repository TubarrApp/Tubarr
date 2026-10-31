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
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
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
		err := checkOverridesFit(&repo.ChannelStore{}, c, tt.urlID, &models.VideoOverrides{MetarrArgs: tt.o})
		if (err == nil) != tt.fits {
			t.Errorf("%s: got error %v, want fits %t", tt.name, err, tt.fits)
		}
	}
}

// TestMatchesChannelURL tests that Download As options are only overrides where they differ from the channel URL's, so
// the dialog's defaults sent back unchanged aren't kept.
func TestMatchesChannelURL(t *testing.T) {
	metaOps, err := parsing.ParseMetaOps([]string{"title:prefix:[Cats] "})
	if err != nil {
		t.Fatal(err)
	}
	filteredMetaOps, err := parsing.ParseFilteredMetaOps([]string{"title:contains:cat||genre:set:Cats"})
	if err != nil {
		t.Fatal(err)
	}
	filenameOps, err := parsing.ParseFilenameOps([]string{"date-tag:prefix:ymd"})
	if err != nil {
		t.Fatal(err)
	}
	filteredFilenameOps, err := parsing.ParseFilteredFilenameOps([]string{"title:contains:cat||prefix:[Cats] "})
	if err != nil {
		t.Fatal(err)
	}
	cu := &models.ChannelURL{
		ID:              1,
		ChanURLSettings: &models.Settings{VideoDir: "/videos", YtdlpOutputExt: "mkv"},
		ChanURLMetarrArgs: &models.MetarrArgs{
			MetaOps:             metaOps,
			FilteredMetaOps:     filteredMetaOps,
			FilenameOps:         filenameOps,
			FilteredFilenameOps: filteredFilenameOps,
			OutputExt:           "mp4",
			OutputDir:           "/out",
		},
	}
	c := &models.Channel{URLModels: []*models.ChannelURL{cu}}
	cs := &repo.ChannelStore{}

	// The dialog's defaults sent back as Revert to Defaults leaves them: the channel URL's ops, the rest blank.
	defaults, err := metarr.ChannelURLOverrideLines(cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := parseOverrideLines(&models.OverrideLines{
		MetaOps:             defaults.MetaOps,
		FilteredMetaOps:     defaults.FilteredMetaOps,
		FilenameOps:         defaults.FilenameOps,
		FilteredFilenameOps: defaults.FilteredFilenameOps,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !matchesChannelURL(cs, c, cu.ID, sent) {
		t.Fatalf("the dialog's defaults %+v didn't match the channel URL's", defaults)
	}

	// Anything else set must be the channel URL's own value.
	tests := []struct {
		name    string
		o       models.VideoOverrides
		matches bool
	}{
		{"the channel URL's values typed in", models.VideoOverrides{VideoDir: "/videos", YtdlpOutputExt: "mkv", MetarrArgs: models.MetarrArgs{OutputExt: "mp4"}}, true},
		{"a filename", models.VideoOverrides{Filename: "Chosen"}, false},
		{"a stripped thumbnail", models.VideoOverrides{StripThumbnail: true}, false},
		{"another video directory", models.VideoOverrides{VideoDir: "/elsewhere"}, false},
		{"preferred video codecs", models.VideoOverrides{YTDLPPreferredVideoCodecs: []string{"av1"}}, false},
		{"another output extension", models.VideoOverrides{MetarrArgs: models.MetarrArgs{OutputExt: "mkv"}}, false},
		{"a transcode quality", models.VideoOverrides{MetarrArgs: models.MetarrArgs{TranscodeQuality: "23"}}, false},
		{"the channel URL's output directory, which beats move ops", models.VideoOverrides{MetarrArgs: models.MetarrArgs{OutputDir: "/out"}}, false},
	}
	for _, tt := range tests {
		o := tt.o
		o.MetaOps = sent.MetaOps
		o.FilteredMetaOps = sent.FilteredMetaOps
		o.FilenameOps = sent.FilenameOps
		o.FilteredFilenameOps = sent.FilteredFilenameOps
		if got := matchesChannelURL(cs, c, cu.ID, &o); got != tt.matches {
			t.Errorf("%s: got match %t, want %t", tt.name, got, tt.matches)
		}
	}

	// The op boxes replace the channel URL's ops, so emptying or adding to them makes overrides.
	moreOps, err := parsing.ParseMetaOps([]string{"title:prefix:[Cats] ", "album:set:Pets"})
	if err != nil {
		t.Fatal(err)
	}
	for name, ops := range map[string][]models.MetaOps{"emptied": nil, "added to": moreOps} {
		o := *sent
		o.MetaOps = ops
		if matchesChannelURL(cs, c, cu.ID, &o) {
			t.Errorf("meta ops %s matched the channel URL's", name)
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
