package parsing

import (
	"path/filepath"
	"slices"
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
		var in models.ChannelInput
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

// TestDownloadConcurrencyKey tests only the download-concurrency config key sets download concurrency.
func TestDownloadConcurrencyKey(t *testing.T) {
	for key, want := range map[string]int{"download-concurrency": 4, "download_concurrency": 4, "concurrency-limit": 0, "concurrency": 0} {
		v := viper.New()
		v.Set(key, 4)
		var in models.ChannelInput
		if err := LoadViperIntoStruct(v, &in); err != nil || NilOrZeroValue(in.Concurrency) != want {
			t.Errorf("config key %q: download concurrency %d, %v; want %d", key, NilOrZeroValue(in.Concurrency), err, want)
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

// TestParseNotificationsBareURL tests a notification can be a bare URL, and must be http(s).
func TestParseNotificationsBareURL(t *testing.T) {
	got, err := ParseNotifications([]string{"http://jellyfin.local/Library/Refresh"})
	if err != nil || len(got) != 1 || got[0].NotifyURL != "http://jellyfin.local/Library/Refresh" || got[0].ChannelURL != "" {
		t.Errorf("bare URL = %+v, %v", got, err)
	}
	if _, err := ParseNotifications([]string{"Plex Library"}); err == nil {
		t.Error("a name alone was accepted as a notification URL")
	}
	if _, err := ParseNotifications([]string{"https://localhost:32400/library/sections/< YOUR LIBRARY NUMBER >/refresh"}); err == nil {
		t.Error("a URL containing spaces and angle brackets was accepted")
	}
}

// TestParseNotificationsChannelScoped tests "Channel URL|Notify URL" scopes a notification, and that both must be http(s) URLs.
func TestParseNotificationsChannelScoped(t *testing.T) {
	got, err := ParseNotifications([]string{"https://tubesite.com/@chan|http://plex:32400/refresh?X-Plex-Token=a"})
	if err != nil || len(got) != 1 || got[0].ChannelURL != "https://tubesite.com/@chan" || got[0].NotifyURL != "http://plex:32400/refresh?X-Plex-Token=a" {
		t.Errorf("channel scoped = %+v, %v", got, err)
	}
	for _, bad := range []string{
		"http://plex:32400/refresh|Plex",                            // Old "Notify URL|Name" form.
		"https://tubesite.com/@chan|http://plex:32400/refresh|Plex", // Old "Channel URL|Notify URL|Name" form.
		"tubesite|http://plex:32400/refresh",                        // Channel URL isn't a URL.
	} {
		if _, err := ParseNotifications([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
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
