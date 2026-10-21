package repo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/viper"
)

// writeChannelConfig writes a channel config file holding body, returning its path.
func writeChannelConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "channel.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// reloadChannelConfig adds a channel, then applies the config file at path as a later run does.
func reloadChannelConfig(t *testing.T, path string) *models.Channel {
	t.Helper()
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.ChannelConfigFile = path
	if err := cs.UpdateChannelFromConfig(c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestConfigPauseKeyAgrees tests that adding a channel from a config file and reloading that file read the same pause key.
func TestConfigPauseKeyAgrees(t *testing.T) {
	for _, key := range []string{"pause", "pause-toggle"} {
		t.Run(key, func(t *testing.T) {
			path := writeChannelConfig(t, key+": true\n")

			v := viper.New()
			if err := file.LoadConfigFile(v, path); err != nil {
				t.Fatal(err)
			}
			var input models.ChannelInput
			if err := parsing.LoadViperIntoStruct(v, &input); err != nil {
				t.Fatal(err)
			}
			added := input.Pause != nil && *input.Pause

			reloaded := reloadChannelConfig(t, path).ChanSettings.Paused
			if added != reloaded {
				t.Errorf("%q: adding the channel reads paused=%t, reloading the file reads paused=%t", key, added, reloaded)
			}
		})
	}
}

// TestConfigReloadReadsURLSettings tests that reloading a channel config file applies its url-settings section.
func TestConfigReloadReadsURLSettings(t *testing.T) {
	path := writeChannelConfig(t, strings.Join([]string{
		"url-settings:",
		"  'https://example.com/@a':",
		"    settings:",
		"      crawl-freq: 45",
		"    metarr:",
		"      metarr-rename-style: spaces",
	}, "\n")+"\n")

	c := reloadChannelConfig(t, path)
	urls := c.URLModels
	if len(urls) != 1 {
		t.Fatalf("got %d URLs, want 1", len(urls))
	}
	if got := urls[0].ChanURLSettings; got == nil || got.CrawlFreq != 45 {
		t.Errorf("url-settings crawl-freq not applied, got settings %+v", got)
	}
	if got := urls[0].ChanURLMetarrArgs; got == nil || got.RenameStyle != "spaces" {
		t.Errorf("url-settings metarr-rename-style not applied, got Metarr args %+v", got)
	}
}

// TestConfigReloadKeepsSavedURLSettings tests that reloading url-settings already saved changes no URL, though
// saving normalizes them (here, lowercasing the extension).
func TestConfigReloadKeepsSavedURLSettings(t *testing.T) {
	path := writeChannelConfig(t, strings.Join([]string{
		"url-settings:",
		"  'https://example.com/@a':",
		"    metarr:",
		"      metarr-output-ext: MKV",
	}, "\n")+"\n")
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.ChannelConfigFile = path
	if err := cs.UpdateChannelFromConfig(c); err != nil {
		t.Fatal(err)
	}

	input, err := parsing.LoadChannelInputFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := cs.applyConfigURLSettings(c, path, input.URLSettings)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Errorf("reloading saved url-settings changed %d URL(s), want 0", len(changed))
	}
}

// TestConfigReloadReadsAllFields tests that reloading a channel config file applies the settings adding a channel reads.
func TestConfigReloadReadsAllFields(t *testing.T) {
	moveDir := t.TempDir()
	opsFile := filepath.Join(t.TempDir(), "filtered-filename-ops.txt")
	if err := os.WriteFile(opsFile, []byte("title:contains:a|prefix:[A] \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeChannelConfig(t, strings.Join([]string{
		"filter-ops:",
		"  - 'title:contains:cat:must'",
		"move-ops:",
		"  - 'title:contains:cat|" + moveDir + "'",
		"ytdlp-preferred-video-codecs: [av1]",
		"ytdlp-preferred-audio-codecs: [opus]",
		"metarr-filtered-filename-ops:",
		"  - 'title:contains:cat|prefix:[CAT] '",
		"metarr-filtered-filename-ops-file: '" + opsFile + "'",
	}, "\n")+"\n")

	c := reloadChannelConfig(t, path)
	s, m := c.ChanSettings, c.ChanMetarrArgs
	if len(s.Filters) != 1 {
		t.Errorf("filter-ops: got %d filters, want 1", len(s.Filters))
	}
	if len(s.MetaFilterMoveOps) != 1 {
		t.Errorf("move-ops: got %d move ops, want 1", len(s.MetaFilterMoveOps))
	}
	if !slices.Equal(s.YTDLPPreferredVideoCodecs, []string{"av1"}) {
		t.Errorf("ytdlp-preferred-video-codecs: got %v, want [av1]", s.YTDLPPreferredVideoCodecs)
	}
	if !slices.Equal(s.YTDLPPreferredAudioCodecs, []string{"opus"}) {
		t.Errorf("ytdlp-preferred-audio-codecs: got %v, want [opus]", s.YTDLPPreferredAudioCodecs)
	}
	if len(m.FilteredFilenameOps) != 1 {
		t.Errorf("metarr-filtered-filename-ops: got %d ops, want 1", len(m.FilteredFilenameOps))
	}
	if m.FilteredFilenameOpsFile != opsFile {
		t.Errorf("metarr-filtered-filename-ops-file: got %q, want %q", m.FilteredFilenameOpsFile, opsFile)
	}
}
