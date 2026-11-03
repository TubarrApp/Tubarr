package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"tubarr/internal/domain/keys"
	"tubarr/internal/queue"
	"tubarr/internal/repo"
	"tubarr/internal/state"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// crawlConfigChannel adds a due channel reading a config file, with a fake yt-dlp finding no videos, returning its ID and
// the fake's run record and the config file's path.
func crawlConfigChannel(t *testing.T, name string) (id int64, runs, cfg string) {
	t.Helper()
	setupFuzzServer(t)

	// yt-dlp finds nothing.
	bin := t.TempDir()
	runs = filepath.Join(bin, "runs")
	script := "#!/bin/sh\necho run >> '" + runs + "'\necho '{\"entries\":[]}'\n"
	if err := os.WriteFile(filepath.Join(bin, sharedconsts.ProgramYTDLP), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	abstractions.Set(keys.SkipAllWaits, true)
	t.Cleanup(func() { abstractions.Set(keys.SkipAllWaits, nil) })

	cfg = wiringFile(t, name+".yaml", "channel-name: "+name+"\n")
	form := webChannelForm(t, name, "https://example.com/@"+strings.ToLower(name))
	form.Set("channel_config_file", cfg)
	id = addWebChannel(t, form)

	// Due, as last crawled long ago.
	if _, err := fuzzServer.db.Exec("UPDATE channel_urls SET last_scan = ? WHERE channel_id = ?", time.Now().Add(-48*time.Hour), id); err != nil {
		t.Fatal(err)
	}
	return id, runs, cfg
}

// crawlConfigStore returns a server store over the test database.
func crawlConfigStore(t *testing.T) *serverStore {
	t.Helper()
	store, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	pp := queue.NewPostProcessingQueue(store)
	return &serverStore{
		s:  store,
		cs: store.ChannelStore(),
		ds: store.DownloadStore(),
		vs: store.VideoStore(),
		dq: queue.NewDownloadQueue(t.Context(), store, pp),
		pp: pp,
		db: fuzzServer.db,
	}
}

// waitForCrawl waits until the fake yt-dlp has run and the channel's crawl is over.
func waitForCrawl(t *testing.T, id int64, runs string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for {
		_, err := os.Stat(runs)
		active, stopping := state.CrawlingChannels()
		if err == nil && !slices.Contains(active, id) && !slices.Contains(stopping, id) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("the crawl never ran or finished")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestScheduledCrawlReadsConfigFile tests the web scheduler applies an edited config file when a crawl is due.
func TestScheduledCrawlReadsConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ScheduledConfig")
	if err := os.WriteFile(cfg, []byte("channel-name: ScheduledConfig\ncrawl-freq: 77\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	crawlConfigStore(t).crawlDueChannels(t.Context(), map[int64]string{})
	waitForCrawl(t, id, runs)

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	if v := formValue(got.Settings["crawl_freq"]); v != "77" {
		t.Errorf("crawl_freq = %q after the scheduled crawl, want the file's 77", v)
	}
}

// TestScheduledCrawlHonorsPauseInConfigFile tests a config file pausing the channel stops the crawl due.
func TestScheduledCrawlHonorsPauseInConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ScheduledPause")
	if err := os.WriteFile(cfg, []byte("channel-name: ScheduledPause\n"+keys.Pause+": true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	crawlConfigStore(t).crawlDueChannels(t.Context(), map[int64]string{})

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	if v := formValue(got.Settings["paused"]); v != "true" {
		t.Errorf("paused = %q, want the file's true", v)
	}
	active, _ := state.CrawlingChannels()
	if _, err := os.Stat(runs); err == nil || slices.Contains(active, id) {
		t.Error("the paused channel was crawled")
	}
}

// TestScheduledCrawlUnpausesFromConfigFile tests a config file can unpause a channel saved as paused.
func TestScheduledCrawlUnpausesFromConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ScheduledUnpause")
	if w := sendRequest(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/toggle-pause", id), "", ""); w.Code != http.StatusOK {
		t.Fatalf("pausing = %d %s", w.Code, w.Body.String())
	}
	if err := os.WriteFile(cfg, []byte("channel-name: ScheduledUnpause\n"+keys.Pause+": false\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	crawlConfigStore(t).crawlDueChannels(t.Context(), map[int64]string{})
	waitForCrawl(t, id, runs)
}

// TestCrawlButtonReadsConfigFile tests the Crawl button applies an edited config file.
func TestCrawlButtonReadsConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ButtonConfig")
	if err := os.WriteFile(cfg, []byte("channel-name: ButtonConfig\ncrawl-freq: 88\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if w := sendRequest(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/crawl", id), "", ""); w.Code != http.StatusAccepted {
		t.Fatalf("crawl = %d %s", w.Code, w.Body.String())
	}
	waitForCrawl(t, id, runs)

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	if v := formValue(got.Settings["crawl_freq"]); v != "88" {
		t.Errorf("crawl_freq = %q after the crawl, want the file's 88", v)
	}
}
