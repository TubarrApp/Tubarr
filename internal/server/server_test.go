package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// TestMetarrArgsJSONMap tests the metarrArgsJSONMap function.
func TestMetarrArgsJSONMap(t *testing.T) {
	t.Parallel()

	metaOps := []models.MetaOps{
		{Field: "title", OpType: consts.OpReplace, OpFindString: "foo", OpValue: "bar"},
		{ChannelURL: "https://example.com/channel", Field: "director", OpType: "set", OpValue: "mr-cat"},
	}
	filenameOps := []models.FilenameOps{
		{OpType: "prefix", OpValue: "[CAT]"},
		{ChannelURL: "https://example.com/channel", OpType: consts.OpReplace, OpFindString: "foo", OpValue: "bar"},
	}
	filteredMetaOps := []models.FilteredMetaOps{
		{
			Filters: []models.Filters{
				{Field: "title", ContainsOmits: "contains", Value: "cat", MustAny: "any"},
			},
			MetaOps: []models.MetaOps{
				{Field: "title", OpType: "set", OpValue: "mr-cat"},
			},
		},
	}
	filteredFilenameOps := []models.FilteredFilenameOps{
		{
			Filters: []models.Filters{
				{Field: "title", ContainsOmits: "contains", Value: "dog", MustAny: "any"},
			},
			FilenameOps: []models.FilenameOps{
				{OpType: "prefix", OpValue: "[DOG]"},
			},
		},
	}

	args := &models.MetarrArgs{
		OutputDir:             "/tmp/out",
		OutputExt:             ".mkv",
		RenameStyle:           "fixes-only",
		Concurrency:           4,
		MaxCPU:                75.5,
		MinFreeMem:            "2G",
		TranscodeGPU:          "cuda",
		TranscodeVideoCodecs:  []string{"h264", "hevc"},
		TranscodeAudioCodecs:  []string{"aac"},
		TranscodeQuality:      "22",
		TranscodeVideoFilter:  "scale=1280:720",
		ExtraFFmpegArgs:       "-preset slow",
		MetaOps:               metaOps,
		FilenameOps:           filenameOps,
		FilteredMetaOps:       filteredMetaOps,
		FilteredFilenameOps:   filteredFilenameOps,
		TranscodeGPUDirectory: "/dev/dri",
	}

	got := metarrArgsJSONMap(args)
	want := map[string]any{
		jsonkeys.MetarrOutputDirectory:      "/tmp/out",
		jsonkeys.MetarrOutputExt:            ".mkv",
		jsonkeys.MetarrRenameStyle:          "fixes-only",
		jsonkeys.MetarrConcurrency:          4,
		jsonkeys.MetarrMaxCPU:               75.5,
		jsonkeys.MetarrMinFreeMem:           "2G",
		jsonkeys.MetarrGPU:                  "cuda",
		jsonkeys.MetarrVideoCodecs:          []string{"h264", "hevc"},
		jsonkeys.MetarrAudioCodecs:          []string{"aac"},
		jsonkeys.MetarrTranscodeQuality:     "22",
		jsonkeys.MetarrTranscodeVideoFilter: "scale=1280:720",
		jsonkeys.MetarrExtraFFmpegArgs:      "-preset slow",
		jsonkeys.MetarrMetaOps:              "title:replace:foo:bar\nhttps://example.com/channel|director:set:mr-cat",
		jsonkeys.MetarrFilenameOps:          "prefix:[CAT]\nhttps://example.com/channel|replace:foo:bar",
		jsonkeys.MetarrFilteredMetaOps:      "title:contains:cat:any|title:set:mr-cat",
		jsonkeys.MetarrFilteredFilenameOps:  "title:contains:dog:any|prefix:[DOG]",
	}
	assertMapEquals(t, got, want)
}

// TestSettingsJSONMap tests the settingsJSONMap function.
func TestSettingsJSONMap(t *testing.T) {
	t.Parallel()

	settings := &models.Settings{
		VideoDir:    "/videos",
		JSONDir:     "/json",
		Concurrency: 3,
		CrawlFreq:   15,
		Retries:     2,
		MaxFilesize: "10m",
		FromDate:    "20250101",
		ToDate:      "20250202",
		FilterFile:  "/tmp/filters.txt",
		Filters: []models.Filters{
			{ChannelURL: "https://example.com/feed", Field: "title", ContainsOmits: "contains", Value: "cat", MustAny: "any"},
			{Field: "description", ContainsOmits: "omits", Value: "boring", MustAny: "must"},
		},
		YtdlpOutputExt:         "mp4",
		CookiesFromBrowser:     "firefox",
		ExternalDownloader:     "aria2c",
		ExternalDownloaderArgs: "--max-connection-per-server=5",
		ExtraYTDLPVideoArgs:    "--limit-rate 1M",
		ExtraYTDLPMetaArgs:     "--write-info-json",
		MetaFilterMoveOpFile:   "/tmp/moveops.txt",
		MetaFilterMoveOps: []models.MetaFilterMoveOps{
			{ChannelURL: "https://example.com/feed", Field: "title", ContainsValue: "cat", OutputDir: "/cats"},
		},
		UseGlobalCookies: true,
		Paused:           true,
	}

	got := settingsJSONMap(settings)
	want := map[string]any{
		jsonkeys.SettingsVideoDirectory:         "/videos",
		jsonkeys.SettingsJSONDirectory:          "/json",
		jsonkeys.SettingsMaxConcurrency:         3,
		jsonkeys.SettingsCrawlFreq:              15,
		jsonkeys.SettingsDownloadRetries:        2,
		jsonkeys.SettingsMaxFilesize:            "10m",
		jsonkeys.SettingsFromDate:               "20250101",
		jsonkeys.SettingsToDate:                 "20250202",
		jsonkeys.SettingsFilterFile:             "/tmp/filters.txt",
		jsonkeys.SettingsFilters:                "https://example.com/feed|title:contains:cat:any\ndescription:omits:boring:must",
		jsonkeys.SettingsYtdlpOutputExt:         "mp4",
		jsonkeys.SettingsCookiesFromBrowser:     "firefox",
		jsonkeys.SettingsExternalDownloader:     "aria2c",
		jsonkeys.SettingsExternalDownloaderArgs: "--max-connection-per-server=5",
		jsonkeys.SettingsExtraYtdlpVideoArgs:    "--limit-rate 1M",
		jsonkeys.SettingsExtraYtdlpMetaArgs:     "--write-info-json",
		jsonkeys.SettingsMoveOpsFile:            "/tmp/moveops.txt",
		jsonkeys.SettingsMoveOps:                "https://example.com/feed|title:cat:/cats",
		jsonkeys.SettingsUseGlobalCookies:       true,
		"paused":                                true,
	}
	assertMapEquals(t, got, want)
}

// TestParseSettingsFromMap tests the parseSettingsFromMap function.
func TestParseSettingsFromMap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	jsonDir := filepath.Join(root, "json")
	videoDir := filepath.Join(root, "videos")
	if err := os.MkdirAll(jsonDir, sharedconsts.PermsGenericDir); err != nil {
		t.Fatalf("failed to make json dir: %v", err)
	}
	if err := os.MkdirAll(videoDir, sharedconsts.PermsGenericDir); err != nil {
		t.Fatalf("failed to make video dir: %v", err)
	}
	filterFile := touchFile(t, root, "filters-*.txt")
	moveOpsFile := touchFile(t, root, "moveops-*.txt")

	data := map[string]any{
		jsonkeys.SettingsCookiesFromBrowser:     "firefox",
		jsonkeys.SettingsExternalDownloader:     "aria2c",
		jsonkeys.SettingsExternalDownloaderArgs: "--max-speed=1M",
		jsonkeys.SettingsExtraYtdlpVideoArgs:    "--limit-rate 1M",
		jsonkeys.SettingsExtraYtdlpMetaArgs:     "--write-info-json",
		jsonkeys.SettingsFilterFile:             filterFile,
		jsonkeys.SettingsMoveOpsFile:            moveOpsFile,
		jsonkeys.SettingsJSONDirectory:          jsonDir,
		jsonkeys.SettingsVideoDirectory:         videoDir,
		jsonkeys.SettingsMaxFilesize:            "10M",
		jsonkeys.SettingsYtdlpOutputExt:         "mp4",
		jsonkeys.SettingsFromDate:               "20250102",
		jsonkeys.SettingsToDate:                 "20250403",
		jsonkeys.SettingsMaxConcurrency:         0,
		jsonkeys.SettingsCrawlFreq:              -5,
		jsonkeys.SettingsDownloadRetries:        2,
		jsonkeys.SettingsUseGlobalCookies:       true,
		jsonkeys.SettingsFilters:                "title:contains:cat:any\n",
		jsonkeys.SettingsMoveOps:                "title:cat:/cats",
	}

	settings, err := parseSettingsFromMap(data)
	if err != nil {
		t.Fatalf("parseSettingsFromMap() unexpected error: %v", err)
	}

	if settings.CookiesFromBrowser != "firefox" {
		t.Fatalf("CookiesFromBrowser mismatch: got %q", settings.CookiesFromBrowser)
	}
	if settings.ExternalDownloader != "aria2c" {
		t.Fatalf("ExternalDownloader mismatch: got %q", settings.ExternalDownloader)
	}
	if settings.ExternalDownloaderArgs != "--max-speed=1M" {
		t.Fatalf("ExternalDownloaderArgs mismatch: got %q", settings.ExternalDownloaderArgs)
	}
	if settings.ExtraYTDLPVideoArgs != "--limit-rate 1M" {
		t.Fatalf("ExtraYTDLPVideoArgs mismatch: got %q", settings.ExtraYTDLPVideoArgs)
	}
	if settings.ExtraYTDLPMetaArgs != "--write-info-json" {
		t.Fatalf("ExtraYTDLPMetaArgs mismatch: got %q", settings.ExtraYTDLPMetaArgs)
	}
	if settings.FilterFile != filterFile {
		t.Fatalf("FilterFile mismatch: got %q want %q", settings.FilterFile, filterFile)
	}
	if settings.MetaFilterMoveOpFile != moveOpsFile {
		t.Fatalf("MetaFilterMoveOpFile mismatch: got %q want %q", settings.MetaFilterMoveOpFile, moveOpsFile)
	}
	if settings.JSONDir != jsonDir {
		t.Fatalf("JSONDir mismatch: got %q want %q", settings.JSONDir, jsonDir)
	}
	if settings.VideoDir != videoDir {
		t.Fatalf("VideoDir mismatch: got %q want %q", settings.VideoDir, videoDir)
	}
	if settings.MaxFilesize != "10m" {
		t.Fatalf("MaxFilesize mismatch: got %q want %q", settings.MaxFilesize, "10m")
	}
	if settings.YtdlpOutputExt != "mp4" {
		t.Fatalf("YtdlpOutputExt mismatch: got %q", settings.YtdlpOutputExt)
	}
	if settings.FromDate != "20250102" || settings.ToDate != "20250403" {
		t.Fatalf("date mismatch: from %q to %q", settings.FromDate, settings.ToDate)
	}
	if settings.Concurrency != 1 {
		t.Fatalf("Concurrency mismatch: got %d want %d", settings.Concurrency, 1)
	}
	if settings.CrawlFreq != 0 {
		t.Fatalf("CrawlFreq mismatch: got %d want 0", settings.CrawlFreq)
	}
	if settings.Retries != 2 {
		t.Fatalf("Retries mismatch: got %d want 2", settings.Retries)
	}
	if !settings.UseGlobalCookies {
		t.Fatalf("UseGlobalCookies expected true")
	}
	if len(settings.Filters) != 1 {
		t.Fatalf("Filters length mismatch: got %d want 1", len(settings.Filters))
	}
	if f := settings.Filters[0]; f.Field != "title" || f.ContainsOmits != "contains" || f.Value != "cat" || f.MustAny != "any" {
		t.Fatalf("Filters parsed incorrectly: %+v", f)
	}
	if len(settings.MetaFilterMoveOps) != 1 {
		t.Fatalf("MetaFilterMoveOps length mismatch: got %d want 1", len(settings.MetaFilterMoveOps))
	}
	if m := settings.MetaFilterMoveOps[0]; m.Field != "title" || m.ContainsValue != "cat" || m.OutputDir != "/cats" {
		t.Fatalf("MetaFilterMoveOps parsed incorrectly: %+v", m)
	}
}

// TestParseMetarrArgsFromMap tests the parseMetarrArgsFromMap function.
func TestParseMetarrArgsFromMap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filenameOpsFile := touchFile(t, root, "filename-*.txt")
	filteredFilenameOpsFile := touchFile(t, root, "filtered-filename-*.txt")
	metaOpsFile := touchFile(t, root, "meta-*.txt")
	filteredMetaOpsFile := touchFile(t, root, "filtered-meta-*.txt")
	outputDir := filepath.Join(root, "output")
	gpuDir := filepath.Join(root, "gpu")
	if err := os.MkdirAll(outputDir, sharedconsts.PermsGenericDir); err != nil {
		t.Fatalf("failed to make output dir: %v", err)
	}
	if err := os.MkdirAll(gpuDir, sharedconsts.PermsGenericDir); err != nil {
		t.Fatalf("failed to make gpu dir: %v", err)
	}

	data := map[string]any{
		jsonkeys.MetarrOutputExt:               ".mkv",
		jsonkeys.MetarrFilenameOpsFile:         filenameOpsFile,
		jsonkeys.MetarrFilteredFilenameOpsFile: filteredFilenameOpsFile,
		jsonkeys.MetarrMetaOpsFile:             metaOpsFile,
		jsonkeys.MetarrFilteredMetaOpsFile:     filteredMetaOpsFile,
		jsonkeys.MetarrExtraFFmpegArgs:         "-preset slow",
		jsonkeys.MetarrRenameStyle:             "underscores",
		jsonkeys.MetarrMinFreeMem:              "2g",
		jsonkeys.MetarrGPUDirectory:            gpuDir,
		jsonkeys.MetarrGPU:                     "cuda",
		jsonkeys.MetarrOutputDirectory:         outputDir,
		jsonkeys.MetarrTranscodeVideoFilter:    "scale=1280:720",
		jsonkeys.MetarrVideoCodecs:             []string{"h264", "hevc"},
		jsonkeys.MetarrAudioCodecs:             []string{"aac", "copy"},
		jsonkeys.MetarrTranscodeQuality:        "18",
		jsonkeys.MetarrConcurrency:             3,
		jsonkeys.MetarrMaxCPU:                  75.5,
		jsonkeys.MetarrFilenameOps:             "prefix:[CAT]\nreplace:foo:bar",
		jsonkeys.MetarrFilteredFilenameOps:     "title:contains:cat:any|prefix:[CAT]",
		jsonkeys.MetarrMetaOps:                 "title:set:awesome",
		jsonkeys.MetarrFilteredMetaOps:         "title:contains:dog:any|director:set:doggo",
	}

	args, err := parseMetarrArgsFromMap(data)
	if err != nil {
		t.Fatalf("parseMetarrArgsFromMap() unexpected error: %v", err)
	}

	if args.OutputExt != ".mkv" {
		t.Fatalf("OutputExt mismatch: got %q", args.OutputExt)
	}
	if args.FilenameOpsFile != filenameOpsFile || args.FilteredFilenameOpsFile != filteredFilenameOpsFile {
		t.Fatalf("filename ops file mismatch: %+v", args)
	}
	if args.MetaOpsFile != metaOpsFile || args.FilteredMetaOpsFile != filteredMetaOpsFile {
		t.Fatalf("meta ops file mismatch: %+v", args)
	}
	if args.ExtraFFmpegArgs != "-preset slow" {
		t.Fatalf("ExtraFFmpegArgs mismatch: got %q", args.ExtraFFmpegArgs)
	}
	if args.RenameStyle != "underscores" {
		t.Fatalf("RenameStyle mismatch: got %q", args.RenameStyle)
	}
	if args.MinFreeMem != "2g" {
		t.Fatalf("MinFreeMem mismatch: got %q want %q", args.MinFreeMem, "2g")
	}
	if args.TranscodeGPUDirectory != gpuDir {
		t.Fatalf("TranscodeGPUDirectory mismatch: got %q want %q", args.TranscodeGPUDirectory, gpuDir)
	}
	if args.TranscodeGPU != "cuda" {
		t.Fatalf("TranscodeGPU mismatch: got %q", args.TranscodeGPU)
	}
	if args.OutputDir != outputDir {
		t.Fatalf("OutputDir mismatch: got %q want %q", args.OutputDir, outputDir)
	}
	if args.TranscodeVideoFilter != "scale=1280:720" {
		t.Fatalf("TranscodeVideoFilter mismatch: got %q", args.TranscodeVideoFilter)
	}
	if !reflect.DeepEqual(args.TranscodeVideoCodecs, []string{"h264", "hevc"}) {
		t.Fatalf("TranscodeVideoCodecs mismatch: %+v", args.TranscodeVideoCodecs)
	}
	if !reflect.DeepEqual(args.TranscodeAudioCodecs, []string{"aac", "copy"}) {
		t.Fatalf("TranscodeAudioCodecs mismatch: %+v", args.TranscodeAudioCodecs)
	}
	if args.TranscodeQuality != "18" {
		t.Fatalf("TranscodeQuality mismatch: got %q", args.TranscodeQuality)
	}
	if args.Concurrency != 3 {
		t.Fatalf("Concurrency mismatch: got %d want %d", args.Concurrency, 3)
	}
	if args.MaxCPU != 75.5 {
		t.Fatalf("MaxCPU mismatch: got %v want %v", args.MaxCPU, 75.5)
	}
	if len(args.FilenameOps) != 2 {
		t.Fatalf("FilenameOps length mismatch: %d", len(args.FilenameOps))
	}
	if args.FilenameOps[0].OpType != "prefix" || args.FilenameOps[1].OpType != consts.OpReplace {
		t.Fatalf("FilenameOps parsed incorrectly: %+v", args.FilenameOps)
	}
	if len(args.FilteredFilenameOps) != 1 {
		t.Fatalf("FilteredFilenameOps length mismatch: %d", len(args.FilteredFilenameOps))
	}
	if len(args.FilteredFilenameOps[0].Filters) != 1 || len(args.FilteredFilenameOps[0].FilenameOps) != 1 {
		t.Fatalf("FilteredFilenameOps content mismatch: %+v", args.FilteredFilenameOps[0])
	}
	if len(args.MetaOps) != 1 || args.MetaOps[0].Field != "title" || args.MetaOps[0].OpType != "set" {
		t.Fatalf("MetaOps parsed incorrectly: %+v", args.MetaOps)
	}
	if len(args.FilteredMetaOps) != 1 {
		t.Fatalf("FilteredMetaOps length mismatch: %d", len(args.FilteredMetaOps))
	}
	if len(args.FilteredMetaOps[0].Filters) != 1 || len(args.FilteredMetaOps[0].MetaOps) != 1 {
		t.Fatalf("FilteredMetaOps content mismatch: %+v", args.FilteredMetaOps[0])
	}
}

// TestHandleCreateChannelSuccess exercises the happy path of handleCreateChannel.
func TestHandleCreateChannelSuccess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	videoDir := filepath.Join(tmpDir, "videos")
	jsonDir := filepath.Join(tmpDir, "json")
	metarrDir := filepath.Join(tmpDir, "metarr")

	for _, dir := range []string{videoDir, jsonDir, metarrDir} {
		if err := os.MkdirAll(dir, sharedconsts.PermsGenericDir); err != nil {
			t.Fatalf("failed to create dir %q: %v", dir, err)
		}
	}

	form := url.Values{}
	form.Set("name", "Test Channel")
	form.Set("urls", "https://example.com/feed")
	form.Set(jsonkeys.SettingsVideoDirectory, videoDir)
	form.Set(jsonkeys.SettingsJSONDirectory, jsonDir)
	form.Set(jsonkeys.SettingsMaxFilesize, "10M")
	//form.Set(jsonkeys.SettingsFromDate, "20240101")
	//form.Set(jsonkeys.SettingsToDate, "2024y02m02d")
	form.Set(jsonkeys.SettingsToDate, "ToDaY")
	form.Set(jsonkeys.SettingsMaxConcurrency, "2")
	form.Set(jsonkeys.SettingsCrawlFreq, "15")
	form.Set(jsonkeys.SettingsDownloadRetries, "1")

	form.Set(jsonkeys.MetarrOutputDirectory, metarrDir)
	form.Set(jsonkeys.MetarrMinFreeMem, "512M")
	form.Set(jsonkeys.MetarrConcurrency, "1")
	form.Set(jsonkeys.MetarrMaxCPU, "75.5")
	form.Set(jsonkeys.MetarrTranscodeQuality, "20")
	form.Set(jsonkeys.MetarrTranscodeVideoFilter, "scale=1280:720")
	form.Set(jsonkeys.MetarrRenameStyle, "fixes-only")
	form.Set(jsonkeys.MetarrExtraFFmpegArgs, "-preset slow")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mockStore := &mockChannelStore{nextID: 77}
	ss := serverStore{cs: mockStore}

	ss.handleCreateChannel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status code: got %d want %d", rec.Code, http.StatusCreated)
	}

	wantBody := fmt.Sprintf("Channel %q added successfully", "Test Channel")
	if gotBody := strings.TrimSpace(rec.Body.String()); gotBody != wantBody {
		t.Fatalf("response body mismatch: got %q want %q", gotBody, wantBody)
	}

	if mockStore.addChannelCalls != 1 {
		t.Fatalf("AddChannel called %d times, want 1", mockStore.addChannelCalls)
	}

	ch := mockStore.addedChannel
	if ch == nil {
		t.Fatalf("expected channel to be passed to AddChannel")
	}
	if ch.ID != mockStore.nextID {
		t.Fatalf("channel ID mismatch: got %d want %d", ch.ID, mockStore.nextID)
	}
	if ch.Name != "Test Channel" {
		t.Fatalf("channel name mismatch: got %q", ch.Name)
	}
	if ch.CreatedAt.IsZero() || ch.LastScan.IsZero() || ch.UpdatedAt.IsZero() {
		t.Fatalf("channel timestamps not initialized: %+v", ch)
	}
	if len(ch.URLModels) != 1 {
		t.Fatalf("URL model count mismatch: got %d want 1", len(ch.URLModels))
	}

	urlModel := ch.URLModels[0]
	if urlModel.URL != "https://example.com/feed" {
		t.Fatalf("channel URL mismatch: got %q", urlModel.URL)
	}
	if urlModel.LastScan.IsZero() || urlModel.CreatedAt.IsZero() || urlModel.UpdatedAt.IsZero() {
		t.Fatalf("channel URL timestamps not initialized: %+v", urlModel)
	}
	if urlModel.ChanURLSettings != nil {
		t.Fatalf("did not expect URL-specific settings, got %+v", urlModel.ChanURLSettings)
	}

	if ch.ChanSettings == nil {
		t.Fatalf("ChanSettings should not be nil")
	}
	if ch.ChanSettings.VideoDir != videoDir {
		t.Fatalf("VideoDir mismatch: got %q want %q", ch.ChanSettings.VideoDir, videoDir)
	}
	if ch.ChanSettings.JSONDir != jsonDir {
		t.Fatalf("JSONDir mismatch: got %q want %q", ch.ChanSettings.JSONDir, jsonDir)
	}
	if ch.ChanSettings.MaxFilesize != "10m" {
		t.Fatalf("MaxFilesize mismatch: got %q want %q", ch.ChanSettings.MaxFilesize, "10m")
	}
	if ch.ChanSettings.CrawlFreq != 15 {
		t.Fatalf("CrawlFreq mismatch: got %d want 15", ch.ChanSettings.CrawlFreq)
	}
	if ch.ChanSettings.Retries != 1 {
		t.Fatalf("Retries mismatch: got %d want 1", ch.ChanSettings.Retries)
	}
	if ch.ChanSettings.Concurrency != 2 {
		t.Fatalf("Concurrency mismatch: got %d want 2", ch.ChanSettings.Concurrency)
	}
	if ch.ChanSettings.FromDate != "20240101" &&
		ch.ChanSettings.ToDate != "20240202" &&
		ch.ChanSettings.ToDate != time.Now().Format("20060102") {

		t.Fatalf("date window mismatch: from %q to %q", ch.ChanSettings.FromDate, ch.ChanSettings.ToDate)
	}

	if ch.ChanMetarrArgs == nil {
		t.Fatalf("ChanMetarrArgs should not be nil")
	}
	if ch.ChanMetarrArgs.OutputDir != metarrDir {
		t.Fatalf("Metarr output dir mismatch: got %q want %q", ch.ChanMetarrArgs.OutputDir, metarrDir)
	}
	if ch.ChanMetarrArgs.Concurrency != 1 {
		t.Fatalf("Metarr concurrency mismatch: got %d want 1", ch.ChanMetarrArgs.Concurrency)
	}
	if ch.ChanMetarrArgs.TranscodeVideoFilter != "scale=1280:720" {
		t.Fatalf("Metarr video filter mismatch: got %q", ch.ChanMetarrArgs.TranscodeVideoFilter)
	}
	if ch.ChanMetarrArgs.TranscodeQuality != "20" {
		t.Fatalf("Metarr quality mismatch: got %q want %q", ch.ChanMetarrArgs.TranscodeQuality, "20")
	}
	if ch.ChanMetarrArgs.MaxCPU != 75.5 {
		t.Fatalf("Metarr max CPU mismatch: got %v want %v", ch.ChanMetarrArgs.MaxCPU, 75.5)
	}
	if ch.ChanMetarrArgs.ExtraFFmpegArgs != "-preset slow" {
		t.Fatalf("Metarr FFmpeg args mismatch: got %q want %q", ch.ChanMetarrArgs.ExtraFFmpegArgs, "-preset slow")
	}
	if ch.ChanMetarrArgs.MinFreeMem == "" {
		t.Fatalf("MinFreeMem should be set")
	}
}

// ****** Private ********************************************************************************************************

type mockChannelStore struct {
	addChannelCalls int
	addedChannel    *models.Channel
	nextID          int64
	addChannelErr   error
}

func (m *mockChannelStore) GetDB() *sql.DB {
	return nil
}

func (m *mockChannelStore) AddAuth(int64, map[string]*models.ChannelAccessDetails) error {
	panic("AddAuth not implemented")
}

func (m *mockChannelStore) AddChannel(c *models.Channel) (int64, error) {
	m.addChannelCalls++
	m.addedChannel = c
	if m.addChannelErr != nil {
		return 0, m.addChannelErr
	}
	if m.nextID == 0 {
		m.nextID = 1
	}
	return m.nextID, nil
}

func (m *mockChannelStore) AddChannelURL(int64, *models.ChannelURL, bool) (int64, error) {
	panic("AddChannelURL not implemented")
}

func (m *mockChannelStore) AddNotifyURLs(int64, []*models.Notification) error {
	panic("AddNotifyURLs not implemented")
}

func (m *mockChannelStore) AddURLToIgnore(int64, string) error {
	panic("AddURLToIgnore not implemented")
}

func (m *mockChannelStore) UpdateChannelFromConfig(*models.Channel) error {
	panic("UpdateChannelFromConfig not implemented")
}

func (m *mockChannelStore) UpdateChannelValue(string, string, string, any) error {
	panic("UpdateChannelValue not implemented")
}

func (m *mockChannelStore) UpdateChannelURLSettings(*models.ChannelURL) error {
	panic("UpdateChannelURLSettings not implemented")
}

func (m *mockChannelStore) UpdateChannelMetarrArgsJSON(string, string, func(*models.MetarrArgs) error) (int64, error) {
	panic("UpdateChannelMetarrArgsJSON not implemented")
}

func (m *mockChannelStore) UpdateChannelSettingsJSON(string, string, func(*models.Settings) error) (int64, error) {
	panic("UpdateChannelSettingsJSON not implemented")
}

func (m *mockChannelStore) UpdateLastScan(int64) error {
	panic("UpdateLastScan not implemented")
}

func (m *mockChannelStore) DeleteChannel(string, string) error {
	panic("DeleteChannel not implemented")
}

func (m *mockChannelStore) DeleteChannelURL(int64) error {
	panic("DeleteChannelURL not implemented")
}

func (m *mockChannelStore) DeleteVideosByURLs(int64, []string) error {
	panic("DeleteVideosByURLs not implemented")
}

func (m *mockChannelStore) DeleteNotifyURLs(int64, []string, []string) error {
	panic("DeleteNotifyURLs not implemented")
}

func (m *mockChannelStore) GetAllChannels(bool) ([]*models.Channel, bool, error) {
	panic("GetAllChannels not implemented")
}

func (m *mockChannelStore) GetDownloadedOrIgnoredVideos(*models.Channel) ([]*models.Video, bool, error) {
	panic("GetDownloadedOrIgnoredVideos not implemented")
}

func (m *mockChannelStore) GetDownloadedOrIgnoredVideoURLs(*models.Channel) ([]string, error) {
	panic("GetDownloadedOrIgnoredVideoURLs not implemented")
}

func (m *mockChannelStore) GetAuth(int64, string) (string, string, string, error) {
	panic("GetAuth not implemented")
}

func (m *mockChannelStore) GetChannelID(string, string) (int64, error) {
	panic("GetChannelID not implemented")
}

func (m *mockChannelStore) GetChannelModel(string, string, bool) (*models.Channel, bool, error) {
	panic("GetChannelModel not implemented")
}

func (m *mockChannelStore) GetChannelURLModel(int64, string, bool) (*models.ChannelURL, bool, error) {
	panic("GetChannelURLModel not implemented")
}

func (m *mockChannelStore) GetChannelURLModels(*models.Channel, bool) ([]*models.ChannelURL, error) {
	panic("GetChannelURLModels not implemented")
}

func (m *mockChannelStore) GetNotifyURLs(int64) ([]*models.Notification, error) {
	panic("GetNotifyURLs not implemented")
}

func (m *mockChannelStore) CheckOrUnlockChannel(*models.Channel) (bool, error) {
	panic("CheckOrUnlockChannel not implemented")
}

func (m *mockChannelStore) DisplaySettings(*models.Channel) {
	panic("DisplaySettings not implemented")
}

// touchFile creates an empty temporary file in the specified directory with the given pattern.
func touchFile(t *testing.T, dir, pattern string) string {
	t.Helper()
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}
	return f.Name()
}

// assertMapEquals checks if two maps are equal and fails the test if they are not.
func assertMapEquals(t *testing.T, got, want map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("map length mismatch: got %d want %d. map: %#v", len(got), len(want), got)
	}
	for key, wantVal := range want {
		gotVal, ok := got[key]
		if !ok {
			t.Fatalf("missing key %q in map %#v", key, got)
		}
		if !reflect.DeepEqual(gotVal, wantVal) {
			t.Fatalf("value mismatch for key %q: got %#v want %#v", key, gotVal, wantVal)
		}
	}
}
