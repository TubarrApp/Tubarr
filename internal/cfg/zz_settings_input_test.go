package cfg

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"tubarr/internal/app"
	"tubarr/internal/cmd"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// settingsKey is one channel setting, as a config file key or update flag.
type settingsKey struct {
	key    string
	field  string // The Settings or MetarrArgs field it sets.
	metarr bool   // A MetarrArgs field (in a URL's 'metarr' config section).
	val    any    // A string, int, float64, bool or []string.
	empty  any    // The value clearing it.
}

// settingsPath applies settings one way, returning the channel's or URL's settings as saved.
//
// base is applied when the channel is added, for the paths changing an existing channel.
type settingsPath struct {
	name  string
	apply func(t *testing.T, cs *repo.ChannelStore, name string, base, in []settingsKey) (*models.Settings, *models.MetarrArgs, error)
}

// settingsPaths are the ways a channel's or URL's settings are read from config files and flags.
var settingsPaths = []settingsPath{
	{"add", func(t *testing.T, cs *repo.ChannelStore, name string, _, in []settingsKey) (*models.Settings, *models.MetarrArgs, error) {
		c, err := addSettingsChannel(t, cs, name, in, nil)
		if err != nil {
			return nil, nil, err
		}
		return c.ChanSettings, c.ChanMetarrArgs, nil
	}},
	{"add url-settings", func(t *testing.T, cs *repo.ChannelStore, name string, _, in []settingsKey) (*models.Settings, *models.MetarrArgs, error) {
		c, err := addSettingsChannel(t, cs, name, nil, in)
		if err != nil {
			return nil, nil, err
		}
		return urlSettings(c)
	}},
	{"reload", func(t *testing.T, cs *repo.ChannelStore, name string, base, in []settingsKey) (*models.Settings, *models.MetarrArgs, error) {
		c, err := addSettingsChannel(t, cs, name, base, nil)
		if err != nil {
			t.Fatal(err)
		}
		c.ChannelConfigFile = writeSettingsConfig(t, settingsMap(in))
		if err := cs.UpdateChannelFromConfig(c); err != nil {
			return nil, nil, err
		}
		c = savedSettingsChannel(t, cs, name)
		return c.ChanSettings, c.ChanMetarrArgs, nil
	}},
	{"update-channel", func(t *testing.T, cs *repo.ChannelStore, name string, base, in []settingsKey) (*models.Settings, *models.MetarrArgs, error) {
		if _, err := addSettingsChannel(t, cs, name, base, nil); err != nil {
			t.Fatal(err)
		}
		if err := runSettingsCmd(updateChannelSettingsCmd(cs), name, in); err != nil {
			return nil, nil, err
		}
		c := savedSettingsChannel(t, cs, name)
		return c.ChanSettings, c.ChanMetarrArgs, nil
	}},
	{"update-url-settings", func(t *testing.T, cs *repo.ChannelStore, name string, base, in []settingsKey) (*models.Settings, *models.MetarrArgs, error) {
		if _, err := addSettingsChannel(t, cs, name, nil, base); err != nil {
			t.Fatal(err)
		}
		if err := runSettingsCmd(updateChannelURLSettingsCmd(cs), name, in); err != nil {
			return nil, nil, err
		}
		return urlSettings(savedSettingsChannel(t, cs, name))
	}},
}

// TestSettingsInputPaths tests that every way of reading settings gives the same settings for the same input.
func TestSettingsInputPaths(t *testing.T) {
	in, wantS, wantM := settingsFixture(t)

	// The fields a path can't set, with the value it leaves.
	known := map[string]map[string]string{}

	for _, p := range settingsPaths {
		t.Run(p.name, func(t *testing.T) {
			s, m, err := p.apply(t, newSettingsTestStore(t), "chan", nil, in)
			if err != nil {
				t.Fatal(err)
			}
			got := fieldDiffs("", s, wantS)
			maps.Copy(got, fieldDiffs("Metarr.", m, wantM))
			if !maps.Equal(got, known[p.name]) {
				t.Errorf("fields differing from the expected settings:\n got %v\nwant %v", got, known[p.name])
			}
		})
	}
}

// TestSettingsInputEmptyValues tests what giving each setting its empty value does, on a channel with every setting set.
func TestSettingsInputEmptyValues(t *testing.T) {
	full, wantS, wantM := settingsFixture(t)

	// The settings a path doesn't clear, and what it does instead. A channel needs a video directory, a blank JSON
	// directory means the video directory, and VAAPI (set on every channel and URL here) needs a GPU node.
	channel := map[string]string{
		keys.JSONDir:  "set to the video directory",
		keys.VideoDir: "error",
	}
	updated := maps.Clone(channel)
	updated[keys.TranscodeGPUNode] = "error"
	known := map[string]map[string]string{
		"add":                 channel,
		"reload":              updated,
		"update-channel":      updated,
		"update-url-settings": {keys.TranscodeGPUNode: "error"},
	}

	for _, p := range settingsPaths {
		t.Run(p.name, func(t *testing.T) {
			cs := newSettingsTestStore(t)
			got := map[string]string{}
			for i, k := range full {
				k.val = k.empty
				s, m, err := p.apply(t, cs, fmt.Sprintf("c%d", i), full, []settingsKey{k})
				if err != nil {
					t.Logf("%s: %v", k.key, err)
					got[k.key] = "error"
					continue
				}
				gotField := reflect.ValueOf(s).Elem().FieldByName(k.field)
				fullField := reflect.ValueOf(wantS).Elem().FieldByName(k.field)
				if k.metarr {
					gotField = reflect.ValueOf(m).Elem().FieldByName(k.field)
					fullField = reflect.ValueOf(wantM).Elem().FieldByName(k.field)
				}
				switch {
				case isCleared(gotField):
				case sameValue(gotField, fullField):
					got[k.key] = "kept"
				case gotField.Kind() == reflect.String && gotField.String() == s.VideoDir:
					got[k.key] = "set to the video directory"
				default:
					got[k.key] = "set to " + showValue(gotField)
				}
			}
			if !maps.Equal(got, known[p.name]) {
				t.Errorf("settings not cleared:\n got %v\nwant %v", got, known[p.name])
			}
		})
	}
}

// TestClearSettingNamesEveryFlag tests that --clear-setting accepts every URL settings flag, with or without "--".
func TestClearSettingNamesEveryFlag(t *testing.T) {
	c := &cobra.Command{}
	var f models.ChannelFlagValues
	setChannelSettingsFlags(c, &f)
	cmd.SetFileDirFlags(c, nil, &f.JSONDir, &f.VideoDir)
	c.Flags().VisitAll(func(fl *pflag.Flag) {
		for _, name := range []string{fl.Name, "--" + fl.Name} {
			if err := clearURLSettings(&models.Settings{}, &models.MetarrArgs{}, []string{name}); err != nil {
				t.Error(err)
			}
		}
	})
	if err := clearURLSettings(&models.Settings{}, &models.MetarrArgs{}, []string{keys.Name}); err == nil {
		t.Errorf("--%s accepted %q, which isn't a setting", keys.ClearSetting, keys.Name)
	}
}

// TestClearSettingClearsEachURLSetting tests that --clear-setting clears each setting of a URL with every setting set.
func TestClearSettingClearsEachURLSetting(t *testing.T) {
	full, _, _ := settingsFixture(t)
	cs := newSettingsTestStore(t)
	for i, k := range full {
		name := fmt.Sprintf("c%d", i)
		if _, err := addSettingsChannel(t, cs, name, nil, full); err != nil {
			t.Fatal(err)
		}
		err := runSettingsCmd(updateChannelURLSettingsCmd(cs), name, []settingsKey{{key: keys.ClearSetting, val: k.key}})
		if k.key == keys.TranscodeGPUNode { // The URL's GPU still needs a node.
			if err == nil {
				t.Errorf("%s: cleared, leaving a GPU without the node it needs", k.key)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", k.key, err)
			continue
		}
		s, m, err := urlSettings(savedSettingsChannel(t, cs, name))
		if err != nil {
			t.Fatal(err)
		}
		field := reflect.ValueOf(s).Elem().FieldByName(k.field)
		if k.metarr {
			field = reflect.ValueOf(m).Elem().FieldByName(k.field)
		}
		if !field.IsZero() {
			t.Errorf("%s: not cleared, got %s", k.key, showValue(field))
		}
	}
}

// TestURLSettingsSaveAsLoad tests that URL settings a channel can't load are refused when saved.
func TestURLSettingsSaveAsLoad(t *testing.T) {
	cs := newSettingsTestStore(t)
	_, err := addSettingsChannel(t, cs, "chan", nil, []settingsKey{{key: keys.MOutputExt, metarr: true, val: "xyz"}})
	if err != nil {
		return
	}
	if c := savedSettingsChannel(t, cs, "chan"); c.LoadErr != nil {
		t.Errorf("saved URL settings the channel can't load: %v", c.LoadErr)
	}
}

// settingsFixture returns every channel setting with a valid value, and the settings they should give.
func settingsFixture(t *testing.T) ([]settingsKey, *models.Settings, *models.MetarrArgs) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("uses VAAPI, which is Linux only")
	}
	dir := t.TempDir()
	mkdir := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.Mkdir(p, 0o750); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mkfile := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	videoDir, jsonDir, moveDir, outDir := mkdir("videos"), mkdir("json"), mkdir("moved"), mkdir("out")
	var (
		filters             = []string{"title:contains:cat:must"}
		moveOps             = []string{"title:contains:cat||" + moveDir}
		filenameOps         = []string{"prefix:[CAT] "}
		filteredFilenameOps = []string{"title:contains:cat||prefix:[CAT] "}
		metaOps             = []string{"title:set:Cats"}
		filteredMetaOps     = []string{"title:contains:cat||title:set:Cats"}
	)
	filterFile := mkfile("filters.txt", filters[0]+"\n")
	moveFile := mkfile("moves.txt", moveOps[0]+"\n")
	filenameOpsFile := mkfile("filename-ops.txt", filenameOps[0]+"\n")
	filteredFilenameOpsFile := mkfile("filtered-filename-ops.txt", filteredFilenameOps[0]+"\n")
	metaOpsFile := mkfile("meta-ops.txt", metaOps[0]+"\n")
	filteredMetaOpsFile := mkfile("filtered-meta-ops.txt", filteredMetaOps[0]+"\n")
	gpuNode := mkfile("renderD128", "")

	in := []settingsKey{
		{keys.DownloadConcurrency, "DownloadConcurrency", false, 3, 0},
		{keys.CookiesFromBrowser, "CookiesFromBrowser", false, "firefox", ""},
		{keys.CrawlFreq, "CrawlFreq", false, 45, 0},
		{keys.ExternalDownloader, "ExternalDownloader", false, "aria2c", ""},
		{keys.ExternalDownloaderArgs, "ExternalDownloaderArgs", false, "-x 4", ""},
		{keys.MaxFilesize, "MaxFilesize", false, "100MB", ""},
		{keys.DLRetries, "Retries", false, 2, 0},
		{keys.UseGlobalCookies, "UseGlobalCookies", false, true, false},
		{keys.YtdlpOutputExt, "YtdlpOutputExt", false, "MKV", ""},
		{keys.YTDLPPreferredVideoCodecs, "YTDLPPreferredVideoCodecs", false, []string{"AV1"}, []string{}},
		{keys.YTDLPPreferredAudioCodecs, "YTDLPPreferredAudioCodecs", false, []string{"Opus"}, []string{}},
		{keys.ExtraYTDLPVideoArgs, "ExtraYTDLPVideoArgs", false, "--no-part", ""},
		{keys.ExtraYTDLPMetaArgs, "ExtraYTDLPMetaArgs", false, "--no-cache-dir", ""},
		{keys.ExtraYTDLPCrawlArgs, "ExtraYTDLPCrawlArgs", false, "--lazy-playlist", ""},
		{keys.FilterOpsInput, "Filters", false, filters, []string{}},
		{keys.FilterOpsFile, "FilterFile", false, filterFile, ""},
		{keys.MoveOps, "MetaFilterMoveOps", false, moveOps, []string{}},
		{keys.MoveOpsFile, "MetaFilterMoveOpFile", false, moveFile, ""},
		{keys.FromDate, "FromDate", false, "2024-01-02", ""},
		{keys.ToDate, "ToDate", false, "2025-02-03", ""},
		{keys.JSONDir, "JSONDir", false, jsonDir, ""},
		{keys.VideoDir, "VideoDir", false, videoDir, ""},
		{keys.Pause, "Paused", false, true, false},
		{keys.AutoDownload, "AutoDownload", false, true, false},

		{keys.MOutputExt, "OutputExt", true, "MKV", ""},
		{keys.MFilenameOps, "FilenameOps", true, filenameOps, []string{}},
		{keys.MFilenameOpsFile, "FilenameOpsFile", true, filenameOpsFile, ""},
		{keys.MFilteredFilenameOps, "FilteredFilenameOps", true, filteredFilenameOps, []string{}},
		{keys.MFilteredFilenameOpsFile, "FilteredFilenameOpsFile", true, filteredFilenameOpsFile, ""},
		{keys.MMetaOps, "MetaOps", true, metaOps, []string{}},
		{keys.MMetaOpsFile, "MetaOpsFile", true, metaOpsFile, ""},
		{keys.MFilteredMetaOps, "FilteredMetaOps", true, filteredMetaOps, []string{}},
		{keys.MFilteredMetaOpsFile, "FilteredMetaOpsFile", true, filteredMetaOpsFile, ""},
		{keys.MRenameStyle, "RenameStyle", true, "spaces", ""},
		{keys.MExtraFFmpegArgs, "ExtraFFmpegArgs", true, "-map 0", ""},
		{keys.MOutputDir, "OutputDir", true, outDir, ""},
		{keys.MMetaPurge, "MetaPurge", true, "JSON", ""},
		{keys.MConcurrency, "MetarrConcurrency", true, 2, 0},
		{keys.MMaxCPU, "MaxCPU", true, 50.0, 0.0},
		{keys.MMinFreeMem, "MinFreeMem", true, "2G", ""},
		{keys.TranscodeGPU, "TranscodeGPU", true, "VAAPI", ""},
		{keys.TranscodeGPUNode, "TranscodeGPUNode", true, gpuNode, ""},
		{keys.TranscodeVideoFilter, "TranscodeVideoFilter", true, "scale=1280:-2", ""},
		{keys.TranscodeCodec, "TranscodeVideoCodecs", true, []string{"h264"}, []string{}},
		{keys.TranscodeAudioCodec, "TranscodeAudioCodecs", true, []string{"aac"}, []string{}},
		{keys.TranscodeQuality, "TranscodeQuality", true, "23", ""},
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	yes := true
	wantS := &models.Settings{
		DownloadConcurrency:       3,
		CookiesFromBrowser:        "firefox",
		CrawlFreq:                 45,
		ExternalDownloader:        "aria2c",
		ExternalDownloaderArgs:    "-x 4",
		MaxFilesize:               "100m",
		Retries:                   2,
		UseGlobalCookies:          &yes,
		YtdlpOutputExt:            "mkv",
		YTDLPPreferredVideoCodecs: []string{"av1"},
		YTDLPPreferredAudioCodecs: []string{"opus"},
		ExtraYTDLPVideoArgs:       "--no-part",
		ExtraYTDLPMetaArgs:        "--no-cache-dir",
		ExtraYTDLPCrawlArgs:       "--lazy-playlist",
		FilterFile:                filterFile,
		MetaFilterMoveOpFile:      moveFile,
		FromDate:                  "20240102",
		ToDate:                    "20250203",
		JSONDir:                   jsonDir,
		VideoDir:                  videoDir,
		Paused:                    true,
		AutoDownload:              &yes,
	}
	var err error
	wantS.Filters, err = parsing.ParseFilterOps(filters, true)
	must(err)
	wantS.MetaFilterMoveOps, err = parsing.ParseMetaFilterMoveOps(moveOps)
	must(err)

	wantM := &models.MetarrArgs{
		OutputExt:               "mkv",
		FilenameOpsFile:         filenameOpsFile,
		FilteredFilenameOpsFile: filteredFilenameOpsFile,
		RenameStyle:             "spaces",
		MetaOpsFile:             metaOpsFile,
		FilteredMetaOpsFile:     filteredMetaOpsFile,
		OutputDir:               outDir,
		MetaPurge:               "json",
		MetarrConcurrency:       2,
		MaxCPU:                  50,
		MinFreeMem:              "2G",
		TranscodeGPU:            "vaapi",
		TranscodeGPUNode:        gpuNode,
		TranscodeVideoFilter:    "scale=1280:-2",
		TranscodeVideoCodecs:    []string{"h264"},
		TranscodeAudioCodecs:    []string{"aac"},
		TranscodeQuality:        "23",
		ExtraFFmpegArgs:         "-map 0",
	}
	wantM.FilenameOps, err = parsing.ParseFilenameOps(filenameOps)
	must(err)
	wantM.FilteredFilenameOps, err = parsing.ParseFilteredFilenameOps(filteredFilenameOps)
	must(err)
	wantM.MetaOps, err = parsing.ParseMetaOps(metaOps)
	must(err)
	wantM.FilteredMetaOps, err = parsing.ParseFilteredMetaOps(filteredMetaOps)
	must(err)

	return in, wantS, wantM
}

// newSettingsTestStore returns a channel store on a fresh database.
func newSettingsTestStore(t *testing.T) *repo.ChannelStore {
	t.Helper()
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cs, err := repo.GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// addSettingsChannel adds channel name from a config file, with chanKeys as its settings and urlKeys as its URL's.
func addSettingsChannel(t *testing.T, cs *repo.ChannelStore, name string, chanKeys, urlKeys []settingsKey) (*models.Channel, error) {
	t.Helper()
	url := "https://example.com/@" + name
	cfg := map[string]any{
		keys.Name:     name,
		keys.URL:      []string{url},
		keys.VideoDir: t.TempDir(),
	}
	maps.Copy(cfg, settingsMap(chanKeys))
	if urlKeys != nil {
		settings, metarr := map[string]any{}, map[string]any{}
		for _, k := range urlKeys {
			if k.metarr {
				metarr[k.key] = k.val
			} else {
				settings[k.key] = k.val
			}
		}
		cfg["url-settings"] = map[string]any{url: map[string]any{"settings": settings, "metarr": metarr}}
	}
	input, err := parsing.LoadChannelInputFile(writeSettingsConfig(t, cfg))
	if err != nil {
		return nil, err
	}
	if _, err := app.AddChannelFromInput(cs, input); err != nil {
		return nil, err
	}
	return savedSettingsChannel(t, cs, name), nil
}

// savedSettingsChannel loads channel name as saved.
func savedSettingsChannel(t *testing.T, cs *repo.ChannelStore, name string) *models.Channel {
	t.Helper()
	c, found, err := cs.GetChannelModel(consts.QChanName, name, false)
	if err != nil || !found {
		t.Fatalf("loading channel %q: found %t, error %v", name, found, err)
	}
	return c
}

// urlSettings returns the settings of the channel's one URL, empty if it has none of its own.
func urlSettings(c *models.Channel) (*models.Settings, *models.MetarrArgs, error) {
	if len(c.URLModels) != 1 {
		return nil, nil, fmt.Errorf("channel has %d URLs, want 1", len(c.URLModels))
	}
	s, m := c.URLModels[0].ChanURLSettings, c.URLModels[0].ChanURLMetarrArgs
	if s == nil {
		s = &models.Settings{}
	}
	if m == nil {
		m = &models.MetarrArgs{}
	}
	return s, m, nil
}

// settingsMap returns settings as config file keys and values.
func settingsMap(in []settingsKey) map[string]any {
	m := make(map[string]any, len(in))
	for _, k := range in {
		m[k.key] = k.val
	}
	return m
}

// writeSettingsConfig writes cfg as a JSON channel config file, returning its path.
func writeSettingsConfig(t *testing.T, cfg map[string]any) string {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "channel.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runSettingsCmd runs an update command on channel name with settings in as flags, skipping any it has no flag for.
func runSettingsCmd(c *cobra.Command, name string, in []settingsKey) error {
	args := []string{"--" + keys.Name + "=" + name}
	for _, k := range in {
		if c.Flags().Lookup(k.key) == nil {
			continue
		}
		vals, ok := k.val.([]string)
		if !ok {
			args = append(args, fmt.Sprintf("--%s=%v", k.key, k.val))
			continue
		}
		if len(vals) == 0 {
			args = append(args, "--"+k.key+"=")
		}
		for _, v := range vals {
			args = append(args, "--"+k.key+"="+v)
		}
	}
	c.SetArgs(args)
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	return c.Execute()
}

// fieldDiffs returns got's fields that differ from want's, by prefixed name, with got's values.
func fieldDiffs(prefix string, got, want any) map[string]string {
	g, w := reflect.ValueOf(got).Elem(), reflect.ValueOf(want).Elem()
	diffs := map[string]string{}
	for i := range g.NumField() {
		if !sameValue(g.Field(i), w.Field(i)) {
			diffs[prefix+g.Type().Field(i).Name] = showValue(g.Field(i))
		}
	}
	return diffs
}

// sameValue reports whether a and b are equal, treating nil and empty slices alike.
func sameValue(a, b reflect.Value) bool {
	if a.Kind() == reflect.Slice && a.Len() == 0 && b.Len() == 0 {
		return true
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}

// isCleared reports whether v is its zero value, an empty slice, or a pointer to a zero value.
func isCleared(v reflect.Value) bool {
	switch {
	case v.Kind() == reflect.Slice:
		return v.Len() == 0
	case v.Kind() == reflect.Pointer && !v.IsNil():
		return v.Elem().IsZero()
	}
	return v.IsZero()
}

// showValue formats v, following a pointer.
func showValue(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "<nil>"
		}
		v = v.Elem()
	}
	return fmt.Sprintf("%+v", v.Interface())
}
