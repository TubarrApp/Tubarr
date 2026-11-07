package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/queue"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// slotHoldingYtdlp puts a fake yt-dlp in PATH that never finishes a "hold-slot" video's metadata, finding nothing else.
func slotHoldingYtdlp(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *hold-slot*) exec sleep 60 ;; esac\necho '{\"entries\":[]}'\n"
	if err := os.WriteFile(filepath.Join(bin, sharedconsts.ProgramYTDLP), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	abstractions.Set(keys.SkipAllWaits, true)
	t.Cleanup(func() { abstractions.Set(keys.SkipAllWaits, nil) })
}

// queueWaitingVideo adds a one-at-a-time channel and queues two of its videos: one holds the slot, the other waits.
func queueWaitingVideo(t *testing.T, form url.Values) int64 {
	t.Helper()
	slotHoldingYtdlp(t)
	id := addWebChannel(t, form)

	// Not due a crawl, so the crawl watchdog leaves it alone.
	if _, err := fuzzServer.db.Exec("UPDATE channel_urls SET last_scan = ? WHERE channel_id = ?", time.Now(), id); err != nil {
		t.Fatal(err)
	}

	c, found, err := fuzzServer.ss.cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
	if err != nil || !found || c == nil || len(c.URLModels) == 0 {
		t.Fatalf("loading channel %d = %t, %v", id, found, err)
	}
	cu := c.URLModels[0]
	reqs := []queue.Request{
		{Video: &models.Video{URL: cu.URL + "/hold-slot-1", ChannelURLID: cu.ID}, ByUser: true},
		{Video: &models.Video{URL: cu.URL + "/hold-slot-2", ChannelURLID: cu.ID}, ByUser: true},
	}
	if queued, err := fuzzServer.ss.dq.Add(c, reqs...); err != nil || len(queued) != 2 {
		t.Fatalf("Add = %d queued, %v; want both", len(queued), err)
	}
	t.Cleanup(func() {
		fuzzServer.ss.dq.Abort(id)
		waitForStages(t, id, 0, 0)
	})
	waitForStages(t, id, 2, 1)
	return id
}

// waitForStages waits until the router's queue has the given counts of the channel's videos, and of those not started.
func waitForStages(t *testing.T, channelID int64, queued, waiting int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		var stages []string
		n := 0
		for _, e := range fuzzServer.ss.dq.Entries() {
			if e.ChannelID == channelID && e.DownloadStatus != nil {
				stages = append(stages, e.DownloadStatus.Stage)
				if e.DownloadStatus.Stage == "Waiting to start" {
					n++
				}
			}
		}
		if len(stages) == queued && n == waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("channel's queued videos are at stages %q, want %d with %d waiting to start", stages, queued, waiting)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestChannelSaveStartsWaitingVideo tests raising a channel's download concurrency starts its waiting video at once.
func TestChannelSaveStartsWaitingVideo(t *testing.T) {
	setupFuzzServer(t)
	form := webChannelForm(t, "LiveLimitChannel", "https://example.com/@livelimitchannel")
	form.Set(jsonkeys.SettingsMaxConcurrency, "1")
	id := queueWaitingVideo(t, form)

	form.Set(jsonkeys.SettingsMaxConcurrency, "2")
	if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusOK {
		t.Fatalf("saving the channel = %d %s", w.Code, w.Body.String())
	}
	waitForStages(t, id, 2, 0)
}

// TestProfileSaveStartsWaitingVideo tests raising a profile's download concurrency starts its channel's waiting video.
func TestProfileSaveStartsWaitingVideo(t *testing.T) {
	setupFuzzServer(t)
	profile := url.Values{"name": {"Live Limit Profile"}, jsonkeys.SettingsMaxConcurrency: {"1"}}
	if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", profile); w.Code != http.StatusCreated {
		t.Fatalf("adding the profile = %d %s", w.Code, w.Body.String())
	}
	var profileID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", profile.Get("name")).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
	})

	form := webChannelForm(t, "LiveLimitProfile", "https://example.com/@livelimitprofile")
	form.Set("profile_id", strconv.FormatInt(profileID, 10))
	id := queueWaitingVideo(t, form)

	profile.Set(jsonkeys.SettingsMaxConcurrency, "2")
	if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/profiles/%d/update", profileID), profile); w.Code != http.StatusOK {
		t.Fatalf("saving the profile = %d %s", w.Code, w.Body.String())
	}
	waitForStages(t, id, 2, 0)
}

// TestWatchdogAppliesLimitSavedElsewhere tests the crawl watchdog applies a limit saved outside the edit page.
func TestWatchdogAppliesLimitSavedElsewhere(t *testing.T) {
	setupFuzzServer(t)
	form := webChannelForm(t, "LiveLimitWatchdog", "https://example.com/@livelimitwatchdog")
	form.Set(jsonkeys.SettingsMaxConcurrency, "1")
	id := queueWaitingVideo(t, form)

	// Saved straight to the database, without telling the queues.
	if _, err := fuzzServer.ss.cs.UpdateChannelSettingsJSON(consts.QChanID, strconv.FormatInt(id, 10), func(s *models.Settings) error {
		s.DownloadConcurrency = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fuzzServer.ss.crawlDueChannels(t.Context(), map[int64]string{})
	waitForStages(t, id, 2, 0)
}
