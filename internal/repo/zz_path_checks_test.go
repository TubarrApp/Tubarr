package repo

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// addChannelMissingDirs adds a channel with a Metarr output folder, then removes it and the video folder, as if their
// drive were unmounted.
func addChannelMissingDirs(t *testing.T, cs *ChannelStore) *models.Channel {
	t.Helper()
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ChanMetarrArgs.OutputDir = t.TempDir()
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{c.ChanSettings.VideoDir, c.ChanMetarrArgs.OutputDir} {
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

// writeTestFile writes an operations file, returning its path.
func writeTestFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ops.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestChannelUpdateChecksChangedPaths tests that a channel update is only stopped by a file or folder it changes being
// unavailable.
func TestChannelUpdateChecksChangedPaths(t *testing.T) {
	cs := newTestChannelStore(t)
	c := addChannelMissingDirs(t, cs)
	id := strconv.FormatInt(c.ID, 10)
	missing := filepath.Join(t.TempDir(), "missing.txt")

	setFilterFile := func(path string) error {
		_, err := cs.UpdateChannelSettingsJSON(consts.QChanID, id, func(s *models.Settings) error {
			s.FilterFile = path
			return nil
		})
		return err
	}
	if err := setFilterFile(writeTestFile(t, "title:contains:cat:must\n")); err != nil {
		t.Errorf("changing the filter file with the unchanged video folder missing: %v", err)
	}
	if err := setFilterFile(missing); err == nil {
		t.Error("changed the filter file to a missing one")
	}

	setMetaOpsFile := func(path string) error {
		_, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanID, id, func(m *models.MetarrArgs) error {
			m.MetaOpsFile = path
			return nil
		})
		return err
	}
	if err := setMetaOpsFile(writeTestFile(t, "title:set:Cats\n")); err != nil {
		t.Errorf("changing the meta ops file with the unchanged output folder missing: %v", err)
	}
	if err := setMetaOpsFile(missing); err == nil {
		t.Error("changed the meta ops file to a missing one")
	}
}

// TestConfigReloadChecksChangedPaths tests that reloading a config file is only stopped by a file or folder it changes
// being unavailable.
func TestConfigReloadChecksChangedPaths(t *testing.T) {
	cs := newTestChannelStore(t)
	c := addChannelMissingDirs(t, cs)

	c.ChannelConfigFile = writeChannelConfig(t, "crawl-freq: 30\n")
	if err := cs.UpdateChannelFromConfig(c); err != nil {
		t.Errorf("reloading with the unchanged video folder missing: %v", err)
	}
	if c.ChanSettings.CrawlFreq != 30 {
		t.Errorf("crawl frequency %d, want 30", c.ChanSettings.CrawlFreq)
	}

	c.ChannelConfigFile = writeChannelConfig(t, "filter-ops-file: '"+filepath.Join(t.TempDir(), "missing.txt")+"'\n")
	if err := cs.UpdateChannelFromConfig(c); err == nil {
		t.Error("reloaded a filter file that's missing")
	}
}
