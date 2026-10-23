package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"tubarr/internal/contracts"
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

// TestURLSaveRefusesMissingFile tests that adding or updating a URL whose own settings name a missing file is refused
// as invalid, not as a server fault.
func TestURLSaveRefusesMissingFile(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	missing := &models.MetarrArgs{FilteredMetaOpsFile: filepath.Join(t.TempDir(), "missing.txt")}

	added := &models.ChannelURL{URL: "https://example.com/@b", ChanURLMetarrArgs: missing}
	if _, err := cs.AddChannelURL(c.ID, added, false); !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Errorf("AddChannelURL with a missing ops file: got %v, want an invalid channel error", err)
	}

	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ChanURLMetarrArgs = missing
	if err := cs.UpdateChannelURLSettings(urls[0]); !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Errorf("UpdateChannelURLSettings with a missing ops file: got %v, want an invalid channel error", err)
	}
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

// TestMissingFilterFileDoesNotBlockChannels tests a channel whose filter file disappears still loads and can be paused,
// and doesn't stop other channels loading, while setting a missing file is still rejected.
func TestMissingFilterFileDoesNotBlockChannels(t *testing.T) {
	cs := newTestChannelStore(t)
	if _, err := cs.AddChannel(testChannel(t, "Healthy", "https://example.com/@a")); err != nil {
		t.Fatal(err)
	}
	filterFile := filepath.Join(t.TempDir(), "filters.txt")
	if err := os.WriteFile(filterFile, []byte("title:contains:cat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := testChannel(t, "Uses filter file", "https://example.com/@b")
	c.ChanSettings.FilterFile = filterFile
	id, err := cs.AddChannel(c)
	if err != nil {
		t.Fatal(err)
	}
	idStr := strconv.FormatInt(id, 10)

	if err := os.Remove(filterFile); err != nil {
		t.Fatal(err)
	}
	if chans, _, err := cs.GetAllChannels(true); err != nil || len(chans) != 2 {
		t.Fatalf("GetAllChannels = %d channels, %v; want both", len(chans), err)
	}
	if _, found, err := cs.GetChannelModel(consts.QChanID, idStr, true); !found || err != nil {
		t.Fatalf("GetChannelModel = found %v, %v; want it loaded so it can be fixed", found, err)
	}

	// Unrelated updates (e.g. pausing) work, but setting a missing path doesn't.
	if _, err := cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
		s.Paused = true
		return nil
	}); err != nil {
		t.Errorf("pausing the channel = %v, want it to work", err)
	}
	if _, err := cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
		s.FilterFile = filepath.Join(t.TempDir(), "also-missing.txt")
		return nil
	}); err == nil {
		t.Error("setting a missing filter file succeeded, want it rejected")
	}
}

// TestCheckDownloadDirs tests that saving refuses a missing folder it adds without creating it, but accepts unchanged
// ones, so e.g. a channel can be saved while an unchanged folder's drive is unmounted.
func TestCheckDownloadDirs(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	// Existing and templated (with an existing fixed part) folders are accepted.
	s := &models.Settings{VideoDir: dir, JSONDir: filepath.Join(dir, "{{channel_name}}")}
	if err := checkDownloadDirs(nil, s); err != nil {
		t.Fatalf("existing folders: %v", err)
	}

	// New missing folders are refused and not created, whether plain, templated or a move op's.
	for name, added := range map[string]*models.Settings{
		"video":     {VideoDir: missing},
		"templated": {JSONDir: filepath.Join(missing, "{{channel_name}}")},
		"move op":   {MetaFilterMoveOps: []models.MetaFilterMoveOps{{Field: "title", Value: "cats", OutputDir: missing}}},
	} {
		if err := checkDownloadDirs(nil, added); err == nil {
			t.Errorf("%s: missing folder accepted", name)
		}
	}
	if exists(missing) {
		t.Fatal("checking created the missing folder")
	}

	// Saved again unchanged while a folder is missing: accepted, and still not created.
	unchanged := &models.Settings{VideoDir: missing}
	if err := checkDownloadDirs(unchanged, unchanged); err != nil {
		t.Errorf("unchanged missing folder: %v", err)
	}

	// Metarr output folder: a new missing one is refused, an unchanged one accepted.
	if err := checkOutputDir(nil, &models.MetarrArgs{OutputDir: missing}); err == nil {
		t.Error("output folder: missing folder accepted")
	}
	if err := checkOutputDir(&models.MetarrArgs{OutputDir: missing}, &models.MetarrArgs{OutputDir: missing}); err != nil {
		t.Errorf("output folder: unchanged missing folder: %v", err)
	}
	if exists(missing) {
		t.Error("checking created the missing folder")
	}
}
