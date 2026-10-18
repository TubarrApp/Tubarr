package repo

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestURLLastScanMigration tests that after upgrading, channel URLs start at their channel's last crawl, only once, and
// that crawling a URL restarts its crawl frequency.
func TestURLLastScanMigration(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "Migrated", "https://example.com/@migrated")
	id, err := cs.AddChannel(c)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := cs.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	reopen := func() {
		t.Helper()
		db, err := database.InitDB()
		if err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	}
	urlScan := func() (urlID int64, ts time.Time) {
		t.Helper()
		if err := cs.db.QueryRow(`SELECT id, last_scan FROM channel_urls WHERE channel_id = ?`, id).Scan(&urlID, &ts); err != nil {
			t.Fatal(err)
		}
		return urlID, ts
	}

	// A database from before the migration.
	old := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	exec(`UPDATE channels SET last_scan = ? WHERE id = ?`, old, id)
	exec(`DELETE FROM settings WHERE key = 'migrated-url-last-scan'`)
	reopen()
	if _, got := urlScan(); !got.Equal(old) {
		t.Errorf("after upgrading, the URL's last scan is %s, want the channel's %s", got, old)
	}

	exec(`UPDATE channels SET last_scan = ? WHERE id = ?`, time.Now(), id)
	reopen()
	urlID, got := urlScan()
	if !got.Equal(old) {
		t.Errorf("the migration ran again, setting the URL's last scan to %s", got)
	}

	if err := cs.UpdateChannelURLsLastScan([]int64{urlID}); err != nil {
		t.Fatal(err)
	}
	if _, got := urlScan(); time.Since(got) > time.Minute {
		t.Errorf("after a crawl, the URL's last scan is %s, want now", got)
	}
}

// TestInvalidChannelLoadsWithError tests that a channel whose saved settings fail validation loads with its LoadErr,
// without stopping other channels loading.
func TestInvalidChannelLoadsWithError(t *testing.T) {
	cs := newTestChannelStore(t)
	if _, err := cs.AddChannel(testChannel(t, "Healthy", "https://example.com/@a")); err != nil {
		t.Fatal(err)
	}
	id, err := cs.AddChannel(testChannel(t, "Broken", "https://example.com/@b"))
	if err != nil {
		t.Fatal(err)
	}
	// E.g. a value a newer version stopped accepting.
	if _, err := cs.db.Exec(`UPDATE channels SET settings = ? WHERE id = ?`, []byte(`{"video_directory":"`+t.TempDir()+`","ytdlp_output_ext":"bogus"}`), id); err != nil {
		t.Fatal(err)
	}

	chans, _, err := cs.GetAllChannels(true)
	if err != nil || len(chans) != 2 {
		t.Fatalf("GetAllChannels = %d channels, %v; want both", len(chans), err)
	}
	for _, c := range chans {
		if broken := c.Name == "Broken"; broken != (c.LoadErr != nil) {
			t.Errorf("channel %q LoadErr = %v", c.Name, c.LoadErr)
		}
	}
	c, found, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
	if !found || err != nil || c == nil {
		t.Fatalf("GetChannelModel = found %v, %v; want it loaded so it can be fixed", found, err)
	}
	if c.LoadErr == nil || !strings.Contains(c.LoadErr.Error(), "edit page") {
		t.Errorf("LoadErr = %v, want one telling the user where to fix it", c.LoadErr)
	}
}

// TestURLSettingsInheritBlankFields tests that a URL with one setting of its own inherits every other from its channel,
// through both URL loaders, without sharing the channel's settings or its pause.
func TestURLSettingsInheritBlankFields(t *testing.T) {
	cs := newTestChannelStore(t)
	dir := t.TempDir()
	file := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	c := testChannel(t, "Inherits", "https://example.com/@inherits")
	c.ChanSettings.Paused = true
	c.ChanSettings.CrawlFreq = 90
	c.ChanSettings.YTDLPPreferredVideoCodecs = []string{"av1"}
	c.ChanSettings.YTDLPPreferredAudioCodecs = []string{"opus"}
	c.ChanMetarrArgs = &models.MetarrArgs{
		TranscodeGPU:     "vaapi",
		TranscodeGPUNode: file("renderD128"),
		FilteredMetaOps: []models.FilteredMetaOps{{
			Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}},
			MetaOps: []models.MetaOps{{Field: "artist", OpType: "set", OpValue: "Cat"}},
		}},
		MetaOpsFile:             file("meta-ops.txt"),
		FilteredMetaOpsFile:     file("filtered-meta-ops.txt"),
		FilenameOpsFile:         file("filename-ops.txt"),
		FilteredFilenameOpsFile: file("filtered-filename-ops.txt"),
	}
	c.URLModels[0].ChanURLSettings = &models.Settings{VideoDir: filepath.Join(dir, "own")}
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{OutputDir: filepath.Join(dir, "own-out")}
	id, err := cs.AddChannel(c)
	if err != nil {
		t.Fatal(err)
	}

	check := func(loader string, cu *models.ChannelURL) {
		t.Helper()
		s, m, want := cu.ChanURLSettings, cu.ChanURLMetarrArgs, c.ChanMetarrArgs
		if s.VideoDir != filepath.Join(dir, "own") || m.OutputDir != filepath.Join(dir, "own-out") {
			t.Errorf("%s: the URL's own settings were replaced: %q, %q", loader, s.VideoDir, m.OutputDir)
		}
		if s.CrawlFreq != 90 || !slices.Equal(s.YTDLPPreferredVideoCodecs, []string{"av1"}) || !slices.Equal(s.YTDLPPreferredAudioCodecs, []string{"opus"}) {
			t.Errorf("%s: settings didn't inherit: crawl frequency %d, codecs %v %v", loader, s.CrawlFreq, s.YTDLPPreferredVideoCodecs, s.YTDLPPreferredAudioCodecs)
		}
		if m.TranscodeGPUNode != want.TranscodeGPUNode || len(m.FilteredMetaOps) != 1 || m.MetaOpsFile != want.MetaOpsFile ||
			m.FilteredMetaOpsFile != want.FilteredMetaOpsFile || m.FilenameOpsFile != want.FilenameOpsFile || m.FilteredFilenameOpsFile != want.FilteredFilenameOpsFile {
			t.Errorf("%s: Metarr settings didn't inherit: %+v", loader, m)
		}
		if s.Paused {
			t.Errorf("%s: the URL inherited the channel's pause", loader)
		}
	}

	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	check("GetChannelURLModels", loaded.URLModels[0])
	cu, _, err := cs.GetChannelURLModel(id, "https://example.com/@inherits", true)
	if err != nil {
		t.Fatal(err)
	}
	check("GetChannelURLModel", cu)

	// A URL with no settings of its own gets a copy of the channel's, so changing it can't change the channel's.
	bare := &models.ChannelURL{URL: "https://example.com/@bare"}
	if _, err := cs.AddChannelURL(id, bare, false); err != nil {
		t.Fatal(err)
	}
	cu, _, err = cs.GetChannelURLModel(id, "https://example.com/@bare", true)
	if err != nil {
		t.Fatal(err)
	}
	if cu.ChanURLSettings.Paused {
		t.Error("a URL without settings inherited the channel's pause")
	}
	cu.ChanURLMetarrArgs.OutputExt = "changed"
	if again, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true); err != nil || again.ChanMetarrArgs.OutputExt == "changed" {
		t.Errorf("changing a URL's inherited settings changed the channel's (%v)", err)
	}
}
