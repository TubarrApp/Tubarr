package parsing

import (
	"path/filepath"
	"testing"
	"tubarr/internal/file"
	"tubarr/internal/models"

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
		var in models.ChannelInputPtrs
		if err := LoadViperIntoStruct(v, &in); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		urlSettings, err := ParseURLSettingsFromViper(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if NilOrZeroValue(in.Concurrency) != 3 || NilOrZeroValue(in.CrawlFreq) != 30 || len(NilOrZeroValue(in.MetaOps)) != 3 || len(urlSettings) != 1 {
			t.Errorf("%s: download concurrency %d, crawl frequency %d, %d meta ops, %d URL settings; want 3, 30, 3, 1",
				name, NilOrZeroValue(in.Concurrency), NilOrZeroValue(in.CrawlFreq), len(NilOrZeroValue(in.MetaOps)), len(urlSettings))
		}
	}
}

// TestDownloadConcurrencyAliases tests the old config keys for download concurrency still load.
func TestDownloadConcurrencyAliases(t *testing.T) {
	for _, key := range []string{"download-concurrency", "download_concurrency", "concurrency-limit", "concurrency"} {
		v := viper.New()
		v.Set(key, 4)
		var in models.ChannelInputPtrs
		if err := LoadViperIntoStruct(v, &in); err != nil || NilOrZeroValue(in.Concurrency) != 4 {
			t.Errorf("config key %q: download concurrency %d, %v; want 4", key, NilOrZeroValue(in.Concurrency), err)
		}
	}
}

// TestParseNotificationsBareURL tests a notification can be a bare URL, named after itself, and must be http(s).
func TestParseNotificationsBareURL(t *testing.T) {
	got, err := ParseNotifications([]string{"http://jellyfin.local/Library/Refresh"})
	if err != nil || len(got) != 1 || got[0].NotifyURL != "http://jellyfin.local/Library/Refresh" || got[0].Name != got[0].NotifyURL {
		t.Errorf("bare URL = %+v, %v", got, err)
	}
	if _, err := ParseNotifications([]string{"Plex Library"}); err == nil {
		t.Error("a name alone was accepted as a notification URL")
	}
	if _, err := ParseNotifications([]string{"https://localhost:32400/library/sections/< YOUR LIBRARY NUMBER >/refresh|Plex"}); err == nil {
		t.Error("a URL containing spaces and angle brackets was accepted")
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
