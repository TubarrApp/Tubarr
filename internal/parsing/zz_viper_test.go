package parsing

import (
	"path/filepath"
	"slices"
	"testing"
	"tubarr/internal/file"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/spf13/viper"
)

// TestExampleConfigFiles tests the example channel config files load their download concurrency, crawl frequency,
// operations and per-URL settings.
func TestExampleConfigFiles(t *testing.T) {
	for _, name := range []string{"example.yaml", "example.toml", "example.json"} {
		v := viper.New()
		if err := file.LoadConfigFile(v, filepath.Join("..", "..", "web", "src", "files", name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var in models.ChannelInput
		if err := LoadViperIntoStruct(v, &in); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		urlSettings, err := ParseURLSettingsFromViper(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if NilOrZeroValue(in.DownloadConcurrency) != 3 || NilOrZeroValue(in.CrawlFreq) != 30 || len(NilOrZeroValue(in.MetaOps)) != 3 || len(urlSettings) != 1 {
			t.Errorf("%s: download concurrency %d, crawl frequency %d, %d meta ops, %d URL settings; want 3, 30, 3, 1",
				name, NilOrZeroValue(in.DownloadConcurrency), NilOrZeroValue(in.CrawlFreq), len(NilOrZeroValue(in.MetaOps)), len(urlSettings))
		}
	}
}

// TestProfileConfigKeys tests that the profile key names a channel's profile, and a URL's in its url-settings.
func TestProfileConfigKeys(t *testing.T) {
	const url = "https://example.com/@a"
	v := viper.New()
	v.Set("profile", "music")
	v.Set("url-settings", map[string]any{url: map[string]any{"profile": "podcasts"}})
	var in models.ChannelInput
	if err := LoadViperIntoStruct(v, &in); err != nil {
		t.Fatal(err)
	}
	urlSettings, err := ParseURLSettingsFromViper(v)
	if err != nil {
		t.Fatal(err)
	}
	if got := NilOrZeroValue(in.Profile); got != "music" {
		t.Errorf("channel profile %q, want music", got)
	}
	if o := urlSettings[url]; o == nil || NilOrZeroValue(o.Profile) != "podcasts" {
		t.Errorf("URL settings %+v, want profile podcasts", o)
	}

	v.Set("url-settings", map[string]any{url: map[string]any{"profile": []any{"podcasts"}}})
	if _, err := ParseURLSettingsFromViper(v); err == nil {
		t.Error("read a URL profile given as a list")
	}
}

// TestDownloadConcurrencyKey tests only the download-concurrency config key sets download concurrency.
func TestDownloadConcurrencyKey(t *testing.T) {
	for key, want := range map[string]int{"download-concurrency": 4, "download_concurrency": 4, "concurrency-limit": 0, "concurrency": 0} {
		v := viper.New()
		v.Set(key, 4)
		var in models.ChannelInput
		if err := LoadViperIntoStruct(v, &in); err != nil || NilOrZeroValue(in.DownloadConcurrency) != want {
			t.Errorf("config key %q: download concurrency %d, %v; want %d", key, NilOrZeroValue(in.DownloadConcurrency), err, want)
		}
	}
}

// TestGivenKeys tests that GivenKeys lists the keys of the fields given, including those of embedded inputs.
func TestGivenKeys(t *testing.T) {
	name, freq, ext := "chan", 5, "mkv"
	var in models.ChannelInput
	in.Name, in.CrawlFreq, in.MetarrExt = &name, &freq, &ext
	want := []string{"channel-name", "crawl-freq", "metarr-output-ext"}
	if got := GivenKeys(&in); !slices.Equal(got, want) {
		t.Errorf("GivenKeys = %v, want %v", got, want)
	}
}

// TestURLSettingsName tests a URL's name is read from its url-settings entry, trimmed, and must be text.
func TestURLSettingsName(t *testing.T) {
	v := viper.New()
	v.Set("url-settings", map[string]any{"https://example.com/@a": map[string]any{"name": " Main "}})
	urlSettings, err := ParseURLSettingsFromViper(v)
	if err != nil {
		t.Fatal(err)
	}
	if o := urlSettings["https://example.com/@a"]; o == nil || o.Name == nil || *o.Name != "Main" {
		t.Errorf("got %+v, want name %q", o, "Main")
	}

	v.Set("url-settings", map[string]any{"https://example.com/@a": map[string]any{"name": 3}})
	if _, err := ParseURLSettingsFromViper(v); err == nil {
		t.Error("a name that isn't text was accepted")
	}
}

// TestApplySameAndKeep tests the 'Same as input' extensions and 'Keep' metadata files are accepted and stored as chosen.
func TestApplySameAndKeep(t *testing.T) {
	same := sharedconsts.ExtSame
	keep := "keep"

	// yt-dlp's extension.
	var s models.Settings
	if err := ApplySettingsInput(&s, &models.SettingsInput{YTDLPOutputExt: &same}); err != nil || s.YtdlpOutputExt != same {
		t.Errorf("yt-dlp extension: got %q, %v; want %q", s.YtdlpOutputExt, err, same)
	}

	// Metarr's extension and metadata files.
	var m models.MetarrArgs
	if err := ApplyMetarrArgsInput(&m, &models.MetarrArgsInput{MetarrExt: &same, MetaPurge: &keep}); err != nil || m.OutputExt != same || m.MetaPurge != keep {
		t.Errorf("Metarr args: got output extension %q and purge %q, %v; want %q and %q", m.OutputExt, m.MetaPurge, err, same, keep)
	}
}

// TestConvertConfigValueRejectsMapsAndLists tests that a map or list given for a string setting is rejected, rather than
// turned into text like "map[a:1]" (which a directory setting would then create as a folder).
func TestConvertConfigValueRejectsMapsAndLists(t *testing.T) {
	for _, v := range []any{map[string]any{"a": 1}, []any{"x"}, nil} {
		if got, ok := convertConfigValue[string](v); ok {
			t.Errorf("convertConfigValue[string](%#v) = %q, want rejected", v, got)
		}
	}
	if _, ok := convertConfigValue[[]string]([]any{"a", map[string]any{"b": 1}}); ok {
		t.Error("expected a list containing a map to be rejected as []string")
	}
	for v, want := range map[any]string{"dir": "dir", 123: "123", true: "true", 1.5: "1.5"} {
		if got, ok := convertConfigValue[string](v); !ok || got != want {
			t.Errorf("convertConfigValue[string](%#v) = %q, %v, want %q", v, got, ok, want)
		}
	}
}
