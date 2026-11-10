package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/contracts"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/viper"
)

// ******************************** Available *****************************************************************************************

// TestAvailableVideos checks Available videos are stored once, count as seen by crawls, and can only be claimed once.
func TestAvailableVideos(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()
	if res, err = db.Exec("INSERT INTO channel_urls (channel_id, url) VALUES (?, 'https://example.com/@a')", chanID); err != nil {
		t.Fatal(err)
	}
	cuID, _ := res.LastInsertId()
	c := &models.Channel{ID: chanID, Name: "Test"}
	vs := GetVideoStore(db)
	cs, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}

	newVideos := func() []*models.Video {
		return []*models.Video{
			{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: "https://example.com/v/1", Metadata: models.VideoMetadata{Title: "One", Duration: 60}},
			{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: "https://example.com/v/2"},
		}
	}
	if added, err := vs.AddAvailableVideos(newVideos(), chanID); err != nil || len(added) != 2 {
		t.Fatalf("first add: got %d, %v, want 2 added", len(added), err)
	}

	// A second crawl finding the same videos adds nothing.
	if added, err := vs.AddAvailableVideos(newVideos(), chanID); err != nil || len(added) != 0 {
		t.Fatalf("second add: got %d, %v, want 0 added", len(added), err)
	}

	// Available videos count as seen, so crawls don't find them as new.
	seen, _, err := cs.GetDownloadedOrIgnoredVideoURLs(c)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(seen)
	if want := []string{"https://example.com/v/1", "https://example.com/v/2"}; !slices.Equal(seen, want) {
		t.Errorf("seen URLs: got %v, want %v", seen, want)
	}

	available, err := vs.GetAvailableVideos(app.AvailableStatuses...)
	if err != nil || len(available) != 2 {
		t.Fatalf("got %d available videos, %v, want 2", len(available), err)
	}
	var one *models.AvailableVideo
	for _, v := range available {
		if v.URL == "https://example.com/v/1" {
			one = v
		}
	}
	if one == nil || one.Metadata.Title != "One" || one.Metadata.Duration != 60 || one.ChannelName != "Test" || one.DownloadStatus.Status != consts.DLStatusAvailable {
		t.Fatalf("video 1: got %+v", one)
	}

	// Queuing twice only succeeds once.
	ids := []int64{available[0].ID, available[1].ID}
	for _, id := range ids {
		v := &models.Video{ID: id}
		if claimed, err := vs.QueueVideo(chanID, v, true); err != nil || !claimed {
			t.Fatalf("first queue of video %d: got %t, %v, want claimed", id, claimed, err)
		}
		// Queued videos keep their title, so the Downloads page can show it before the metadata arrives.
		if v.URL == "https://example.com/v/1" && v.Metadata.Title != "One" {
			t.Errorf("queued video 1's title = %q, want \"One\"", v.Metadata.Title)
		}
		if claimed, err := vs.QueueVideo(chanID, &models.Video{ID: id}, true); err != nil || claimed {
			t.Fatalf("second queue of video %d: got %t, %v, want refused", id, claimed, err)
		}
	}

	// Queued videos count as seen, and a crawl re-finding them leaves them alone.
	if added, err := vs.AddAvailableVideos(newVideos(), chanID); err != nil || len(added) != 0 {
		t.Fatalf("add while downloading: got %d, %v, want 0 added", len(added), err)
	}
	if available, _ := vs.GetAvailableVideos(app.AvailableStatuses...); len(available) != 0 {
		t.Errorf("expected no available videos while downloading, got %d", len(available))
	}

	// Videos a download didn't reach go back to failed, and can be dismissed.
	if reset, err := vs.ClaimVideos(chanID, ids, []consts.DownloadStatus{consts.DLStatusQueued}, consts.DLStatusFailed); err != nil || len(reset) != 2 {
		t.Fatalf("reset: got %d, %v, want 2", len(reset), err)
	}
	if dismissed, err := vs.ClaimVideos(chanID, ids[:1], app.AvailableStatuses, consts.DLStatusIgnored); err != nil || len(dismissed) != 1 {
		t.Fatalf("dismiss: got %d, %v, want 1", len(dismissed), err)
	}
	// The user downloaded both, so crawls leave the failed one to them, as they do the dismissed one.
	if seen, failed, _ := cs.GetDownloadedOrIgnoredVideoURLs(c); len(seen) != 2 || len(failed) != 0 {
		t.Errorf("expected the dismissed and failed videos seen and none retried, got %v and %v", seen, failed)
	}

	// Ignored videos are listed only when asked for, and restoring returns them to Available.
	if available, _ := vs.GetAvailableVideos(app.AvailableStatuses...); len(available) != 1 {
		t.Errorf("expected only the failed video listed, got %d", len(available))
	}
	withIgnored, _ := vs.GetAvailableVideos(consts.DLStatusIgnored)
	if len(withIgnored) != 1 || withIgnored[0].URL != "https://example.com/v/1" {
		t.Fatalf("expected the dismissed video listed as ignored, got %+v", withIgnored)
	}
	if restored, err := vs.ClaimVideos(chanID, ids, []consts.DownloadStatus{consts.DLStatusIgnored}, consts.DLStatusAvailable); err != nil || len(restored) != 1 || restored[0].Ignored {
		t.Fatalf("restore: got %+v, %v, want the one ignored video, no longer ignored", restored, err)
	}
	if available, _ := vs.GetAvailableVideos(consts.DLStatusAvailable); len(available) != 1 || available[0].URL != "https://example.com/v/1" {
		t.Errorf("expected the restored video listed as Available, got %+v", available)
	}
	if again, err := vs.ClaimVideos(chanID, ids[:1], []consts.DownloadStatus{consts.DLStatusIgnored}, consts.DLStatusAvailable); err != nil || len(again) != 0 {
		t.Errorf("second restore: got %d, %v, want 0", len(again), err)
	}
	if seen, _, _ := cs.GetDownloadedOrIgnoredVideoURLs(c); !slices.Contains(seen, "https://example.com/v/1") {
		t.Errorf("expected the restored video still seen by crawls, got %v", seen)
	}
	if dismissed, err := vs.ClaimVideos(chanID, ids[:1], app.AvailableStatuses, consts.DLStatusIgnored); err != nil || len(dismissed) != 1 {
		t.Fatalf("dismiss again: got %d, %v, want 1", len(dismissed), err)
	}

	// Canceled videos count as seen, so crawls never retry them.
	if canceled, err := vs.ClaimVideos(chanID, ids[1:], app.AvailableStatuses, consts.DLStatusCanceled); err != nil || len(canceled) != 1 {
		t.Fatalf("cancel: got %d, %v, want 1", len(canceled), err)
	}
	if seen, failed, _ := cs.GetDownloadedOrIgnoredVideoURLs(c); len(seen) != 2 || len(failed) != 0 {
		t.Errorf("expected both videos seen and none failed, got %v and %v", seen, failed)
	}

	// Videos can't be claimed through another channel.
	if claimed, err := vs.ClaimVideos(chanID+1, ids[1:], app.AvailableStatuses, consts.DLStatusIgnored); err != nil || len(claimed) != 0 {
		t.Errorf("other channel claim: got %d, %v, want 0", len(claimed), err)
	}
}

// TestCrawlDLRetries checks crawls retry the failed videos they queued until their retries run out, and leave a failed
// video to the user once the user downloads it or a crawl finds its URL not downloading automatically.
func TestCrawlDLRetries(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()
	var urlIDs []int64
	for _, u := range []string{"https://example.com/@a", "https://example.com/@b"} {
		if res, err = db.Exec("INSERT INTO channel_urls (channel_id, url) VALUES (?, ?)", chanID, u); err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		urlIDs = append(urlIDs, id)
	}
	c := &models.Channel{ID: chanID, Name: "Test"}
	vs := GetVideoStore(db)
	cs, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}

	// fail queues a video as a crawl or the user does, then fails its download.
	fail := func(v *models.Video, byUser bool) {
		t.Helper()
		if claimed, err := vs.QueueVideo(chanID, v, byUser); err != nil || !claimed {
			t.Fatalf("queue %q: got %t, %v, want claimed", v.URL, claimed, err)
		}
		if reset, err := vs.ClaimVideos(chanID, []int64{v.ID}, []consts.DownloadStatus{consts.DLStatusQueued}, consts.DLStatusFailed); err != nil || len(reset) != 1 {
			t.Fatalf("fail %q: got %d, %v, want 1", v.URL, len(reset), err)
		}
	}
	// retried returns the failed videos' URLs crawls would retry.
	retried := func() []string {
		t.Helper()
		_, failed, err := cs.GetDownloadedOrIgnoredVideoURLs(c)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(failed)
		return failed
	}

	// A video a crawl queued is retried by crawls until its retries run out.
	crawled := &models.Video{ChannelURLID: urlIDs[0], ChannelURL: "https://example.com/@a", URL: "https://example.com/v/1"}
	fail(crawled, false)
	for range consts.MaxCrawlDLRetries {
		if got := retried(); !slices.Equal(got, []string{crawled.URL}) {
			t.Fatalf("expected the crawled video retried, got %v", got)
		}
		if err := cs.AddCrawlDLRetries(chanID, []string{crawled.URL}); err != nil {
			t.Fatal(err)
		}
	}
	if got := retried(); len(got) != 0 {
		t.Errorf("expected none retried once out of crawl retries, got %v", got)
	}

	// Once the user downloads a video, crawls leave it to them if it fails, whether it was stored or a new URL.
	fail(&models.Video{ID: crawled.ID}, true)
	fail(&models.Video{ChannelURLID: urlIDs[0], ChannelURL: "https://example.com/@a", URL: "https://example.com/v/2"}, true)
	if got := retried(); len(got) != 0 {
		t.Errorf("expected the user's failed videos left to them, got %v retried", got)
	}
	available, err := vs.GetAvailableVideos(consts.DLStatusFailed)
	if err != nil || len(available) != 2 {
		t.Fatalf("got %d failed videos, %v, want 2", len(available), err)
	}
	for _, v := range available {
		if v.CrawlDLRetries != consts.MaxCrawlDLRetries {
			t.Errorf("video %q listed with %d crawl retries used, want all %d", v.URL, v.CrawlDLRetries, consts.MaxCrawlDLRetries)
		}
	}

	// A crawl of a URL not downloading automatically leaves its failed videos to the user, and no other URL's.
	onA := &models.Video{ChannelURLID: urlIDs[0], ChannelURL: "https://example.com/@a", URL: "https://example.com/v/3"}
	onB := &models.Video{ChannelURLID: urlIDs[1], ChannelURL: "https://example.com/@b", URL: "https://example.com/v/4"}
	fail(onA, false)
	fail(onB, false)
	if err := cs.UseUpCrawlDLRetries(urlIDs[0]); err != nil {
		t.Fatal(err)
	}
	if got := retried(); !slices.Equal(got, []string{onB.URL}) {
		t.Errorf("expected only the other URL's failed video retried, got %v", got)
	}
}

// TestVideoOverrides checks Download As overrides are stored, kept through the download's video updates, loaded for
// Run Metarr, and cleared.
func TestVideoOverrides(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()
	if res, err = db.Exec("INSERT INTO channel_urls (channel_id, url) VALUES (?, 'https://example.com/@a')", chanID); err != nil {
		t.Fatal(err)
	}
	cuID, _ := res.LastInsertId()
	vs := GetVideoStore(db)

	v := &models.Video{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: "https://example.com/v/1", Metadata: models.VideoMetadata{Title: "One"}}
	if _, err := vs.AddAvailableVideos([]*models.Video{v}, chanID); err != nil {
		t.Fatal(err)
	}
	if o, err := vs.GetVideoOverrides(v.ID); err != nil || o != nil {
		t.Fatalf("new video: got overrides %+v, %v, want none", o, err)
	}

	metaOps, err := parsing.ParseMetaOps([]string{"artist:set:Cats"})
	if err != nil {
		t.Fatal(err)
	}
	if err := vs.SetVideoOverrides([]int64{v.ID}, &models.VideoOverrides{Filename: "Chosen", MetaOps: metaOps}); err != nil {
		t.Fatal(err)
	}

	// The download stores the video without its overrides loaded, which must not clear them.
	v.Finished = true
	v.MetarrStatus = consts.MetarrStatusPending
	if err := vs.UpdateVideo(v, chanID); err != nil {
		t.Fatal(err)
	}
	awaiting, err := vs.GetVideosAwaitingMetarr(chanID, false)
	if err != nil || len(awaiting) != 1 {
		t.Fatalf("got %d videos awaiting Metarr, %v, want 1", len(awaiting), err)
	}
	if o := awaiting[0].Overrides; o == nil || o.Filename != "Chosen" || len(o.MetaOps) != 1 || o.MetaOps[0].OpValue != "Cats" {
		t.Errorf("video awaiting Metarr: got overrides %+v", o)
	}

	// A video Metarr processed is only returned for re-runs.
	v.MetarrStatus = consts.MetarrStatusDone
	if err := vs.UpdateVideo(v, chanID); err != nil {
		t.Fatal(err)
	}
	if awaiting, err := vs.GetVideosAwaitingMetarr(chanID, false); err != nil || len(awaiting) != 0 {
		t.Errorf("got %d videos awaiting Metarr after it ran, %v, want 0", len(awaiting), err)
	}
	if awaiting, err := vs.GetVideosAwaitingMetarr(chanID, true); err != nil || len(awaiting) != 1 {
		t.Errorf("got %d videos for a re-run, %v, want 1", len(awaiting), err)
	}

	if err := vs.SetVideoOverrides([]int64{v.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if o, err := vs.GetVideoOverrides(v.ID); err != nil || o != nil {
		t.Errorf("cleared: got overrides %+v, %v, want none", o, err)
	}
}

// TestQueueClearsLastError checks a failed video keeps why it failed in the Available list, and queuing it again clears it.
func TestQueueClearsLastError(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()
	if res, err = db.Exec("INSERT INTO channel_urls (channel_id, url) VALUES (?, 'https://example.com/@a')", chanID); err != nil {
		t.Fatal(err)
	}
	cuID, _ := res.LastInsertId()
	vs := GetVideoStore(db)

	videos := []*models.Video{
		{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: "https://example.com/v/1"},
		{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: "https://example.com/v/2"},
	}
	if _, err := vs.AddAvailableVideos(videos, chanID); err != nil {
		t.Fatal(err)
	}
	ids := []int64{videos[0].ID, videos[1].ID}
	errorsByURL := func() map[string]string {
		t.Helper()
		available, err := vs.GetAvailableVideos(app.AvailableStatuses...)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, v := range available {
			got[v.URL] = v.DownloadStatus.ErrorMsg
		}
		return got
	}

	// Video 1 fails with a reason, shown in the Available list.
	ds := GetDownloadStore(db)
	failed := models.StatusUpdate{VideoID: ids[0], VideoURL: videos[0].URL, Status: consts.DLStatusFailed, Error: errors.New("domain blocked")}
	if err := ds.UpdateDownloadStatus(t.Context(), failed); err != nil {
		t.Fatal(err)
	}
	if got := errorsByURL(); got[videos[0].URL] != "domain blocked" || got[videos[1].URL] != "" {
		t.Errorf("after failing: got errors %v", got)
	}

	// Trying again clears the last attempt's error.
	if claimed, err := vs.QueueVideo(chanID, &models.Video{ID: ids[0]}, true); err != nil || !claimed {
		t.Fatalf("queue: got %t, %v, want claimed", claimed, err)
	}
	if _, err := vs.ClaimVideos(chanID, ids[:1], []consts.DownloadStatus{consts.DLStatusQueued}, consts.DLStatusCanceled); err != nil {
		t.Fatal(err)
	}
	if got := errorsByURL(); got[videos[0].URL] != "" {
		t.Errorf("after trying again: got errors %v, want none", got)
	}
}

// TestAvailableVideosListingOrder checks undated videos are listed in the order their channel lists them, with later
// crawls' videos above earlier ones.
func TestAvailableVideosListingOrder(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()
	if res, err = db.Exec("INSERT INTO channel_urls (channel_id, url) VALUES (?, 'https://example.com/@a')", chanID); err != nil {
		t.Fatal(err)
	}
	cuID, _ := res.LastInsertId()
	vs := GetVideoStore(db)

	// Each crawl's videos in listing order, newest first.
	batch := func(urls ...string) []*models.Video {
		videos := make([]*models.Video, 0, len(urls))
		for _, u := range urls {
			videos = append(videos, &models.Video{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: u})
		}
		return videos
	}
	if _, err := vs.AddAvailableVideos(batch("old/3", "old/2", "old/1", "old/0"), chanID); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.AddAvailableVideos(batch("new/1", "new/0"), chanID); err != nil {
		t.Fatal(err)
	}

	available, err := vs.GetAvailableVideos(app.AvailableStatuses...)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(available))
	for _, v := range available {
		got = append(got, v.URL)
	}
	if want := []string{"new/1", "new/0", "old/3", "old/2", "old/1", "old/0"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ******************************** Channel loading ***********************************************************************************

// TestOutputDirectoryTagsFilledOnLoad tests merged loads fill the tags once, including inherited ones and manual downloads, while saved settings keep them.
func TestOutputDirectoryTagsFilledOnLoad(t *testing.T) {
	const (
		inherits = "https://example.com/@inherits"
		shorts   = "https://example.com/@shorts"
		own      = "https://example.com/@own"
	)
	cs := newTestChannelStore(t)
	c := testChannel(t, "Tags", inherits, shorts, own)
	videoDir := c.ChanSettings.VideoDir
	ownVideoDir := t.TempDir()
	c.ChanMetarrArgs.OutputDir = "{{channel_output_directory}}/Processed"
	c.URLModels[1].ChanURLMetarrArgs = &models.MetarrArgs{OutputDir: "{{channel_output_directory}}/Shorts"}
	c.URLModels[2].ChanURLSettings = &models.Settings{VideoDir: ownVideoDir}
	c.URLModels[2].ChanURLMetarrArgs = &models.MetarrArgs{OutputDir: "{{channel_url_output_directory}}/Processed"}
	id, err := cs.AddChannel(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AddChannelURL(id, &models.ChannelURL{URL: consts.ManualDownloadsCol}, true); err != nil {
		t.Fatal(err)
	}

	// The directories jobs use.
	merged, found, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
	if err != nil || !found {
		t.Fatalf("merged load: found %t, %v", found, err)
	}
	if got := merged.ChanMetarrArgs.OutputDir; got != filepath.Join(videoDir, "Processed") {
		t.Errorf("channel output directory = %q", got)
	}
	want := map[string]string{
		inherits:                  filepath.Join(videoDir, "Processed"),
		shorts:                    filepath.Join(videoDir, "Processed", "Shorts"),
		own:                       filepath.Join(ownVideoDir, "Processed"),
		consts.ManualDownloadsCol: filepath.Join(videoDir, "Processed"),
	}
	if len(merged.URLModels) != len(want) {
		t.Fatalf("got %d channel URLs, want %d", len(merged.URLModels), len(want))
	}
	for _, cu := range merged.URLModels {
		if got := cu.ChanURLMetarrArgs.OutputDir; got != want[cu.URL] {
			t.Errorf("%s: got %q, want %q", cu.URL, got, want[cu.URL])
		}
	}

	// The directories saves and the edit forms use.
	saved, found, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), false)
	if err != nil || !found {
		t.Fatalf("saved load: found %t, %v", found, err)
	}
	if got := saved.ChanMetarrArgs.OutputDir; got != "{{channel_output_directory}}/Processed" {
		t.Errorf("saved channel output directory = %q", got)
	}
	for _, cu := range saved.URLModels {
		if cu.URL == shorts && cu.ChanURLMetarrArgs.OutputDir != "{{channel_output_directory}}/Shorts" {
			t.Errorf("saved channel URL output directory = %q", cu.ChanURLMetarrArgs.OutputDir)
		}
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
	for _, d := range []string{"own", "own-out"} { // Folders must exist to be saved.
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
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

// ******************************** Config reload *************************************************************************************

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
	if err := os.WriteFile(opsFile, []byte("title:contains:a||prefix:[A] \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeChannelConfig(t, strings.Join([]string{
		"filter-ops:",
		"  - 'title:contains:cat:must'",
		"move-ops:",
		"  - 'title:contains:cat||" + moveDir + "'",
		"ytdlp-preferred-video-codecs: [av1]",
		"ytdlp-preferred-audio-codecs: [opus]",
		"metarr-filtered-filename-ops:",
		"  - 'title:contains:cat||prefix:[CAT] '",
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

// ******************************** Deleting files ************************************************************************************

// TestDeleteFilesTakesNFO tests that deleting a video's files takes the NFO file named for it and nothing else, and that
// deleting a video alone leaves all its files.
func TestDeleteFilesTakesNFO(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	dir := t.TempDir()

	// write makes the named files in the test's folder.
	write := func(names ...string) {
		t.Helper()
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// addVideo saves a finished video with the given files.
	addVideo := func(url, videoFile, jsonFile string) {
		t.Helper()
		v := &models.Video{URL: url, ChannelURL: "https://example.com/@a", Files: models.VideoFiles{VideoFilePath: filepath.Join(dir, videoFile), JSONFilePath: filepath.Join(dir, jsonFile)}}
		v.MarkVideoAsComplete()
		if _, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID); err != nil {
			t.Fatal(err)
		}
		if err := vs.UpdateVideo(v, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	// remaining returns the files left in the test's folder.
	remaining := func() []string {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	// Neighbours with similar names, which aren't the video's.
	neighbours := []string{"Ep.nfo", "Ep. 5 - Cats extra.nfo", "Ep. 5 - Cats.mp4.nfo", "other.mp4", "other.nfo"}
	write(neighbours...)

	// Deleting the video alone leaves its files.
	write("kept.mp4", "kept.json", "kept.nfo")
	addVideo("https://example.com/v/kept", "kept.mp4", "kept.json")
	if err := cs.DeleteVideosByURLs(c.ID, []string{"https://example.com/v/kept"}, false); err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(slices.Values(append([]string{"kept.mp4", "kept.json", "kept.nfo"}, neighbours...)))
	if got := remaining(); !slices.Equal(got, want) {
		t.Errorf("deleting without files:\ngot  %q\nwant %q", got, want)
	}

	// Deleting with files takes the video, its metadata and its NFO, only the last dot starting the extension.
	write("Ep. 5 - Cats.mp4", "Ep. 5 - Cats.json", "Ep. 5 - Cats.nfo")
	addVideo("https://example.com/v/dotted", "Ep. 5 - Cats.mp4", "Ep. 5 - Cats.json")
	if err := cs.DeleteVideosByURLs(c.ID, []string{"https://example.com/v/dotted"}, true); err != nil {
		t.Fatal(err)
	}
	if got := remaining(); !slices.Equal(got, want) {
		t.Errorf("deleting with files:\ngot  %q\nwant %q", got, want)
	}

	// A video without an NFO file deletes as before.
	write("plain.mp4", "plain.json")
	addVideo("https://example.com/v/plain", "plain.mp4", "plain.json")
	if err := cs.DeleteVideosByURLs(c.ID, []string{"https://example.com/v/plain"}, true); err != nil {
		t.Fatal(err)
	}
	if got := remaining(); !slices.Equal(got, want) {
		t.Errorf("deleting a video with no NFO file:\ngot  %q\nwant %q", got, want)
	}
}

// TestVideoFilePathsNFONeedsVideo tests a video with no saved video path gets no NFO path, which would otherwise be a
// file called ".nfo" in the working directory.
func TestVideoFilePathsNFONeedsVideo(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	const videoURL = "https://example.com/v/json-only"
	v := &models.Video{URL: videoURL, ChannelURL: "https://example.com/@a", Files: models.VideoFiles{JSONFilePath: "/json/only.json"}}
	if _, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		t.Fatal(err)
	}

	got, err := cs.videoFilePaths(c.ID, []string{videoURL})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/json/only.json"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ******************************** Downloads *****************************************************************************************

// TestQueueVideo tests which statuses a video the user chose, and a crawled one, can be queued from.
func TestQueueVideo(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	statusOf := func(id int64) consts.DownloadStatus {
		t.Helper()
		var got consts.DownloadStatus
		if err := cs.db.QueryRow("SELECT "+consts.QDLStatus+" FROM "+consts.DBDownloads+" WHERE "+consts.QDLVidID+" = ?", id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	for _, tc := range []struct {
		status        consts.DownloadStatus
		byUser, crawl bool // Whether a user's and a crawl's request claim it.
	}{
		{consts.DLStatusAvailable, true, false},
		{consts.DLStatusFailed, true, true},
		{consts.DLStatusCanceled, true, false},
		{consts.DLStatusCompleted, true, false},
		{consts.DLStatusIgnored, true, false},
		{consts.DLStatusQueued, false, false},
		{consts.DLStatusDownloading, false, false},
	} {
		for _, byUser := range []bool{true, false} {
			url := fmt.Sprintf("https://example.com/v/%s-%t", tc.status, byUser)
			id, err := vs.AddVideo(&models.Video{URL: url, DownloadStatus: models.DLStatus{Status: tc.status}}, c.ID, c.URLModels[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.crawl
			if byUser {
				want = tc.byUser
			}
			v := &models.Video{URL: url}
			if claimed, err := vs.QueueVideo(c.ID, v, byUser); err != nil || claimed != want || v.ID != id {
				t.Errorf("%s by user %t: claimed %t, %v (ID %d); want %t (ID %d)", tc.status, byUser, claimed, err, v.ID, want, id)
			}
			if want && statusOf(id) != consts.DLStatusQueued {
				t.Errorf("%s by user %t: stored status %q, want Queued", tc.status, byUser, statusOf(id))
			}
		}
	}

	// A URL the channel doesn't have is stored as queued.
	v := &models.Video{URL: "https://example.com/v/new", ChannelURLID: c.URLModels[0].ID, Metadata: models.VideoMetadata{Title: "New"}}
	if claimed, err := vs.QueueVideo(c.ID, v, false); err != nil || !claimed || v.ID == 0 || statusOf(v.ID) != consts.DLStatusQueued {
		t.Errorf("new URL: claimed %t, %v (ID %d); want it stored as queued", claimed, err, v.ID)
	}

	// Only a video in the channel can be claimed by its ID.
	if claimed, err := vs.QueueVideo(c.ID+1, &models.Video{ID: v.ID}, true); err != nil || claimed {
		t.Errorf("other channel's claim: %t, %v; want refused", claimed, err)
	}
}

func TestNormalizeDownloadStatus(t *testing.T) {
	tests := []struct {
		name       string
		pct        float64
		status     consts.DownloadStatus
		wantPct    float64
		wantStatus consts.DownloadStatus
	}{
		{"queued kept", 0, consts.DLStatusQueued, 0, consts.DLStatusQueued},
		{"downloading kept", 42.5, consts.DLStatusDownloading, 42.5, consts.DLStatusDownloading},
		{"canceled kept", 10, consts.DLStatusCanceled, 10, consts.DLStatusCanceled},
		{"full stream stays downloading", 100, consts.DLStatusDownloading, 100, consts.DLStatusDownloading},
		{"over clamps", 120, consts.DLStatusDownloading, 100, consts.DLStatusDownloading},
		{"completed forces full", 40, consts.DLStatusCompleted, 100, consts.DLStatusCompleted},
		{"negative clamps", -5, consts.DLStatusFailed, 0, consts.DLStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct, status := tt.pct, tt.status
			normalizeDownloadStatus(&pct, &status, 1)
			if pct != tt.wantPct || status != tt.wantStatus {
				t.Errorf("got (%v, %q), want (%v, %q)", pct, status, tt.wantPct, tt.wantStatus)
			}
		})
	}
}

// ******************************** Saving encoder args *******************************************************************************

// setChannelVideoArgs sets channel "chan"'s encoder args.
func setChannelVideoArgs(cs *ChannelStore, lines ...string) error {
	_, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.TranscodeVideoArgs = lines
		return nil
	})
	return err
}

// TestKnownBadEncoderArgsRefusedOnSave tests each way of saving encoder args refuses those known to be bad.
func TestKnownBadEncoderArgsRefusedOnSave(t *testing.T) {
	cs := newTestChannelStore(t)

	// A new channel, and a new channel's URL.
	c := testChannel(t, "new", "https://example.com/@new")
	c.ChanMetarrArgs.TranscodeVideoArgs = []string{"copy:-crf 20"}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("added a channel with a copy line")
	}
	c = testChannel(t, "new", "https://example.com/@new")
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoArgs: []string{"vp8:-x265-params log-level=error"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("added a channel with a URL's x265 option on VP8")
	}

	// A channel's line every URL replaces, which later URLs would still inherit.
	c = testChannel(t, "new", "https://example.com/@new")
	c.ChanMetarrArgs.TranscodeVideoArgs = []string{"copy:-crf 20"}
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoArgs: []string{"h264:-crf 20"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("added a channel with a copy line its URL replaces")
	}

	// An existing channel.
	cu := addCodecChannel(t, cs, "chan")
	if err := setChannelVideoArgs(cs, "av1:-preset medium"); err == nil {
		t.Error("saved an x264 preset on AV1")
	}
	if err := setChannelVideoArgs(cs, "av1:-preset 8"); err != nil {
		t.Errorf("refused a preset AV1 takes: %v", err)
	}

	// An existing channel's URL.
	cu.ChanURLMetarrArgs.TranscodeVideoArgs = []string{"h264:-x265-params log-level=error"}
	if err := cs.UpdateChannelURLSettings(cu); err == nil {
		t.Error("saved a URL's x265 option on H.264")
	}

	// A new URL on an existing channel.
	existing, found, err := cs.GetChannelModel(consts.QChanName, "chan", false)
	if err != nil || !found {
		t.Fatalf("GetChannelModel = found %t, %v", found, err)
	}
	if _, err := cs.AddChannelURL(existing.ID, &models.ChannelURL{
		URL:               "https://example.com/@other",
		ChanURLMetarrArgs: &models.MetarrArgs{TranscodeVideoArgs: []string{"h264:-x265-params log-level=error"}},
	}, false); err == nil {
		t.Error("added a URL with an x265 option on H.264")
	}

	// A profile a channel already uses, on one without its own args to replace the profile's.
	addCodecChannel(t, cs, "other")
	profile := addTestProfile(t, cs, "used", nil, &models.MetarrArgs{TranscodeVideoArgs: []string{"av1:-preset 8"}})
	if err := cs.SetChannelProfile(consts.QChanName, "other", profile.ID); err != nil {
		t.Fatal(err)
	}
	profile.ProfileMetarrArgs.TranscodeVideoArgs = []string{"av1:-preset p5"}
	if err := cs.UpdateProfile(profile); err == nil {
		t.Error("changed a used profile to an NVENC preset on a channel in software")
	}
}

// TestStoredBadEncoderArgsStillLoad tests that args saved before they were refused still come back from the database, to be
// fixed on the next save.
func TestStoredBadEncoderArgsStillLoad(t *testing.T) {
	cs := newTestChannelStore(t)
	id, err := cs.AddChannel(testChannel(t, "chan", "https://example.com/@a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.db.Exec(`UPDATE channels SET metarr = ? WHERE id = ?`, []byte(`{"metarr_transcode_video_args":["copy:-crf 20"]}`), id); err != nil {
		t.Fatal(err)
	}
	idStr := strconv.FormatInt(id, 10)

	// It loads, with the args there to fix.
	c, found, err := cs.GetChannelModel(consts.QChanID, idStr, true)
	if err != nil || !found {
		t.Fatalf("GetChannelModel = found %t, %v", found, err)
	}
	if c.LoadErr != nil {
		t.Errorf("LoadErr = %v", c.LoadErr)
	}
	if len(c.ChanMetarrArgs.TranscodeVideoArgs) != 1 {
		t.Errorf("args = %v, want the stored line", c.ChanMetarrArgs.TranscodeVideoArgs)
	}
	if chans, _, err := cs.GetAllChannels(true); err != nil || len(chans) != 1 || chans[0].LoadErr != nil {
		t.Errorf("GetAllChannels = %d channels, %v", len(chans), err)
	}

	// Saving Metarr args, until the line goes.
	if err := setChannelOutputExt(cs, "mp4"); err == nil {
		t.Error("saved Metarr args still holding a copy line")
	}
	if err := setChannelVideoArgs(cs); err != nil {
		t.Errorf("refused removing the copy line: %v", err)
	}
	if err := setChannelOutputExt(cs, "mp4"); err != nil {
		t.Errorf("refused Metarr args once fixed: %v", err)
	}
}

// TestProfileEncoderArgsJudgedWithChannelGPU tests a profile alone is only refused for what no GPU could change, with the rest
// judged by the channels using it, as a value bad on software encoders may suit their GPU's.
func TestProfileEncoderArgsJudgedWithChannelGPU(t *testing.T) {
	cs := newTestChannelStore(t)

	// Alone.
	if err := cs.AddProfile(&models.Profile{Name: "copy", ProfileMetarrArgs: &models.MetarrArgs{TranscodeVideoArgs: []string{"copy:-crf 20"}}}); err == nil {
		t.Error("added a profile with a copy line")
	}
	nvenc := addTestProfile(t, cs, "nvenc", nil, &models.MetarrArgs{TranscodeVideoArgs: []string{"av1:-preset p5"}})
	nvenc.ProfileMetarrArgs.TranscodeVideoArgs = []string{"copy:-crf 20"}
	if err := cs.UpdateProfile(nvenc); err == nil {
		t.Error("saved a copy line on a profile")
	}
	nvenc.ProfileMetarrArgs.TranscodeVideoArgs = []string{"av1:-preset p5"}

	// On a channel encoding in software, then one with a GPU.
	addCodecChannel(t, cs, "chan")
	if err := cs.SetChannelProfile(consts.QChanName, "chan", nvenc.ID); err == nil {
		t.Error("gave a channel in software a profile with an NVENC preset")
	}
	if _, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.TranscodeGPU = "cuda"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetChannelProfile(consts.QChanName, "chan", nvenc.ID); err != nil {
		t.Errorf("refused an NVENC preset on a channel with an NVENC GPU: %v", err)
	}

	// The channel's GPU goes while the profile's preset stays.
	if _, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.TranscodeGPU = ""
		return nil
	}); err == nil {
		t.Error("removed the GPU from under a profile's NVENC preset")
	}
}

// ******************************** Merging settings **********************************************************************************

// fullSettings returns Settings with every field set.
func fullSettings() *models.Settings {
	yes := true
	return &models.Settings{
		DownloadConcurrency:       2,
		CookiesFromBrowser:        "firefox",
		CrawlFreq:                 30,
		ExternalDownloader:        "aria2c",
		ExternalDownloaderArgs:    "-x 4",
		MaxFilesize:               "1G",
		MaxResolution:             "1080",
		Retries:                   3,
		UseGlobalCookies:          &yes,
		YtdlpOutputExt:            "mp4",
		YTDLPPreferredVideoCodecs: []string{"av1"},
		YTDLPPreferredAudioCodecs: []string{"opus"},
		ExtraYTDLPVideoArgs:       "--a",
		ExtraYTDLPMetaArgs:        "--b",
		ExtraYTDLPCrawlArgs:       "--c",
		Filters:                   []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}},
		FilterFile:                "/filters",
		MetaFilterMoveOps:         []models.MetaFilterMoveOps{{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}}, OutputDir: "/cats"}},
		MetaFilterMoveOpFile:      "/moves",
		FromDate:                  "20240101",
		ToDate:                    "20250101",
		FiltersReplaceInherited:   true,
		MoveOpsReplaceInherited:   true,
		JSONDir:                   "/json",
		VideoDir:                  "/video",
		Paused:                    true,
		AutoDownload:              &yes,
	}
}

// fullMetarrArgs returns MetarrArgs with every field set.
func fullMetarrArgs() *models.MetarrArgs {
	cat := models.Filters{Field: "title", FilterType: "contains", Value: "cat"}
	yes := true
	return &models.MetarrArgs{
		OutputExt:               "mp4",
		PurgeOriginalVideo:      &yes,
		FilenameOps:             []models.FilenameOps{{OpType: "prefix", OpValue: "[A] "}},
		FilenameOpsFile:         "/filename-ops",
		FilteredFilenameOps:     []models.FilteredFilenameOps{{Filters: []models.Filters{cat}, FilenameOps: []models.FilenameOps{{OpType: "prefix", OpValue: "[C] "}}}},
		FilteredFilenameOpsFile: "/filtered-filename-ops",
		RenameStyle:             "spaces",
		MetaOps:                 []models.MetaOps{{Field: "artist", OpType: "set", OpValue: "A"}},
		MetaOpsFile:             "/meta-ops",
		FilteredMetaOps:         []models.FilteredMetaOps{{Filters: []models.Filters{cat}, MetaOps: []models.MetaOps{{Field: "artist", OpType: "set", OpValue: "C"}}}},
		FilteredMetaOpsFile:     "/filtered-meta-ops",
		WriteNFO:                &yes,
		AutoPostProcess:         &yes,
		OutputDir:               "/out",
		MetaPurge:               "all",
		MetarrConcurrency:       2,
		MaxCPU:                  50,
		MinFreeMem:              "1G",
		TranscodeGPU:            "vaapi",
		TranscodeGPUNode:        "/dev/dri/renderD128",
		TranscodeVideoCodecs:    []string{"h264"},
		TranscodeAudioCodecs:    []string{"aac"},
		TranscodeVideoArgs:      []string{"hevc:-crf 23"},
		ExtraFFmpegArgs:         "-x",

		MetaOpsReplaceInherited:             true,
		FilteredMetaOpsReplaceInherited:     true,
		FilenameOpsReplaceInherited:         true,
		FilteredFilenameOpsReplaceInherited: true,
	}
}

// withoutReplaceTicks returns m with its replace ticks cleared, as merging never passes them on.
func withoutReplaceTicks(m *models.MetarrArgs) models.MetarrArgs {
	m.MetaOpsReplaceInherited = false
	m.FilteredMetaOpsReplaceInherited = false
	m.FilenameOpsReplaceInherited = false
	m.FilteredFilenameOpsReplaceInherited = false
	return *m
}

// withoutOpLists returns m without its op lists, which merging leaves to stackLists.
func withoutOpLists(m models.MetarrArgs) models.MetarrArgs {
	m.MetaOps = nil
	m.FilenameOps = nil
	m.FilteredMetaOps = nil
	m.FilteredFilenameOps = nil
	return m
}

// requireAllSet fails if any field of the struct v points to is unset, so a new settings field must be added here.
func requireAllSet(t *testing.T, v any) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	for i := range rv.NumField() {
		if rv.Field(i).IsZero() {
			t.Fatalf("%s.%s isn't set in the test's full model", rv.Type().Name(), rv.Type().Field(i).Name)
		}
	}
}

// TestMergeSettingsInheritsAll tests a channel URL without settings of its own inherits every channel setting but
// Paused, the replace ticks and the lists, which stackLists fills from every level.
func TestMergeSettingsInheritsAll(t *testing.T) {
	parent := fullSettings()
	requireAllSet(t, parent)

	child := &models.Settings{}
	mergeSettings(child, parent)
	want := *fullSettings()
	want.Paused = false
	want.FiltersReplaceInherited = false
	want.MoveOpsReplaceInherited = false
	want.Filters = nil
	want.MetaFilterMoveOps = nil
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("inherited settings differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeSettingsKeepsOwn tests a channel URL keeps the settings it sets itself, leaving its lists to stackLists.
func TestMergeSettingsKeepsOwn(t *testing.T) {
	no := false
	ownFilter := models.Filters{Field: "title", FilterType: "omits", Value: "dog", MustAny: "any"}
	ownMove := models.MetaFilterMoveOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "dog", MustAny: "must"}}, OutputDir: "/dogs"}
	ownCodecs := []string{"vp9"}
	child := &models.Settings{VideoDir: "/own", Retries: 5, Filters: []models.Filters{ownFilter}, MetaFilterMoveOps: []models.MetaFilterMoveOps{ownMove},
		YTDLPPreferredVideoCodecs: ownCodecs, UseGlobalCookies: &no, Paused: true}
	mergeSettings(child, fullSettings())

	want := *fullSettings()
	want.VideoDir, want.Retries, want.UseGlobalCookies, want.YTDLPPreferredVideoCodecs = "/own", 5, &no, ownCodecs
	want.FiltersReplaceInherited = false
	want.MoveOpsReplaceInherited = false
	want.Filters = []models.Filters{ownFilter}
	want.MetaFilterMoveOps = []models.MetaFilterMoveOps{ownMove}
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("merged settings differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeMetarrArgsInheritsAll tests a channel URL without Metarr args of its own inherits every channel one but the
// replace ticks and the op lists, which stackLists fills from every level.
func TestMergeMetarrArgsInheritsAll(t *testing.T) {
	parent := fullMetarrArgs()
	requireAllSet(t, parent)

	child := &models.MetarrArgs{}
	mergeMetarrArgs(child, parent)
	if want := withoutOpLists(withoutReplaceTicks(fullMetarrArgs())); !reflect.DeepEqual(*child, want) {
		t.Errorf("inherited Metarr args differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeMetarrArgsKeepsOwn tests a channel URL keeps the Metarr args it sets itself, leaving its op lists to
// stackLists.
func TestMergeMetarrArgsKeepsOwn(t *testing.T) {
	ownOp := models.MetaOps{Field: "title", OpType: "prefix", OpValue: "[B] "}
	ownFilenameOp := models.FilenameOps{OpType: "suffix", OpValue: " [B]"}
	ownFiltered := models.FilteredMetaOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "dog"}},
		MetaOps: []models.MetaOps{{Field: "genre", OpType: "set", OpValue: "Dogs"}}}
	ownCodecs := []string{"hevc"}
	child := &models.MetarrArgs{OutputDir: "/own", MaxCPU: 25, MetaOps: []models.MetaOps{ownOp}, FilenameOps: []models.FilenameOps{ownFilenameOp},
		FilteredMetaOps: []models.FilteredMetaOps{ownFiltered}, TranscodeVideoCodecs: ownCodecs}
	mergeMetarrArgs(child, fullMetarrArgs())

	want := withoutReplaceTicks(fullMetarrArgs())
	want.OutputDir, want.MaxCPU, want.TranscodeVideoCodecs = "/own", 25, ownCodecs
	want.MetaOps = []models.MetaOps{ownOp}
	want.FilenameOps = []models.FilenameOps{ownFilenameOp}
	want.FilteredMetaOps = []models.FilteredMetaOps{ownFiltered}
	want.FilteredFilenameOps = nil
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("merged Metarr args differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeWithoutParent tests merging with no channel settings leaves a channel URL's own unchanged.
func TestMergeWithoutParent(t *testing.T) {
	s, m := &models.Settings{VideoDir: "/own"}, &models.MetarrArgs{OutputDir: "/own"}
	mergeSettings(s, nil)
	mergeMetarrArgs(m, nil)
	if !reflect.DeepEqual(*s, models.Settings{VideoDir: "/own"}) || !reflect.DeepEqual(*m, models.MetarrArgs{OutputDir: "/own"}) {
		t.Errorf("merging with no channel settings changed them: %+v, %+v", *s, *m)
	}
}

// ******************************** Notifications *************************************************************************************

// TestDeleteNotifyURLsByURL tests notifications are deleted by URL, for every channel URL, leaving the rest.
func TestDeleteNotifyURLsByURL(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b")
	if err := cs.AddChannelWithDetails(c, nil, []*models.Notification{
		{NotifyURL: "https://one.example.com"},
		{ChannelURL: "https://example.com/@a", NotifyURL: "https://two.example.com"},
		{ChannelURL: "https://example.com/@b", NotifyURL: "https://two.example.com"},
		{NotifyURL: "https://three.example.com"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := cs.DeleteNotifyURLs(c.ID, []string{"https://one.example.com", "https://two.example.com"}); err != nil {
		t.Fatal(err)
	}
	left, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].NotifyURL != "https://three.example.com" {
		t.Errorf("got notifications %+v left, want only three.example.com", left)
	}
}

// TestNotifyURLForSeveralChannelURLs tests one notify URL can be set for several channel URLs, and for all of them.
func TestNotifyURLForSeveralChannelURLs(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b")
	want := []*models.Notification{
		{NotifyURL: "https://hook.example.com"},
		{ChannelURL: "https://example.com/@a", NotifyURL: "https://hook.example.com"},
		{ChannelURL: "https://example.com/@b", NotifyURL: "https://hook.example.com"},
	}
	if err := cs.AddChannelWithDetails(c, nil, want); err != nil {
		t.Fatal(err)
	}
	// Adding one again changes nothing.
	if err := cs.AddNotifyURLs(c.ID, want[1:2]); err != nil {
		t.Fatal(err)
	}

	got, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d notifications, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || (g.ChannelURL == w.ChannelURL && g.NotifyURL == w.NotifyURL)
		}
		if !found {
			t.Errorf("notification %+v is missing from %+v", w, got)
		}
	}
}

// ******************************** Path checks ***************************************************************************************

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
		"move op":   {MetaFilterMoveOps: []models.MetaFilterMoveOps{{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cats", MustAny: "must"}}, OutputDir: missing}}},
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

// ******************************** Profiles ******************************************************************************************

// addTestProfile adds a profile with the given settings.
func addTestProfile(t *testing.T, cs *ChannelStore, name string, s *models.Settings, m *models.MetarrArgs) *models.Profile {
	t.Helper()
	p := &models.Profile{Name: name, ProfileSettings: s, ProfileMetarrArgs: m}
	if err := cs.AddProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestProfileSettingsPrecedence tests that a URL's own settings win, then its profile's, its channel's, and the
// channel's profile's.
func TestProfileSettingsPrecedence(t *testing.T) {
	cs := newTestChannelStore(t)
	profileFilter := models.Filters{Field: "title", FilterType: "omits", Value: "dog", MustAny: "must"}
	channelFilter := models.Filters{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}
	chanProfile := addTestProfile(t, cs, "channel profile",
		&models.Settings{Retries: 3, CrawlFreq: 60, Filters: []models.Filters{profileFilter}},
		&models.MetarrArgs{OutputExt: "mp4", RenameStyle: "spaces"})
	urlProfile := addTestProfile(t, cs, "URL profile", &models.Settings{Retries: 5}, &models.MetarrArgs{OutputExt: "mkv"})

	c := testChannel(t, "chan", "https://example.com/@profiled", "https://example.com/@own")
	c.ProfileID = chanProfile.ID
	c.ChanSettings.Retries = 4
	c.ChanSettings.Filters = []models.Filters{channelFilter}
	c.ChanMetarrArgs.RenameStyle = "underscores"
	c.URLModels[0].ProfileID = urlProfile.ID
	c.URLModels[1].ChanURLSettings = &models.Settings{Retries: 7}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)

	loaded, _, err := cs.GetChannelModel(consts.QChanID, id, true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	type got struct {
		retries, crawlFreq int
		ext, rename        string
	}
	check := func(name string, s *models.Settings, m *models.MetarrArgs, want got) {
		t.Helper()
		if g := (got{s.Retries, s.CrawlFreq, m.OutputExt, m.RenameStyle}); g != want {
			t.Errorf("%s: got %+v, want %+v", name, g, want)
		}
	}
	check("channel", loaded.ChanSettings, loaded.ChanMetarrArgs, got{4, 60, "mp4", "underscores"})
	for _, cu := range loaded.URLModels {
		switch cu.URL {
		case "https://example.com/@profiled":
			check("URL with a profile", cu.ChanURLSettings, cu.ChanURLMetarrArgs, got{5, 60, "mkv", "underscores"})
		case "https://example.com/@own":
			check("URL with its own retries", cu.ChanURLSettings, cu.ChanURLMetarrArgs, got{7, 60, "mp4", "underscores"})
		}
	}
	if want := []models.Filters{profileFilter, channelFilter}; !reflect.DeepEqual(loaded.ChanSettings.Filters, want) {
		t.Errorf("channel filters = %+v, want the profile's then its own", loaded.ChanSettings.Filters)
	}

	// Loaded as saved, the channel has only its own settings.
	saved, _, err := cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ChanSettings.CrawlFreq != 0 || saved.ChanMetarrArgs.OutputExt != "" {
		t.Errorf("saved channel settings include its profile's: crawl frequency %d, output extension %q", saved.ChanSettings.CrawlFreq, saved.ChanMetarrArgs.OutputExt)
	}
	if saved.Profile == nil || saved.Profile.Name != "channel profile" {
		t.Errorf("channel loaded with profile %+v, want %q", saved.Profile, "channel profile")
	}
}

// TestWriteNFOPrecedence tests that the nearest choice wins, whether Yes or No: a URL's own, its profile's, its channel's,
// then the channel's profile's. Unset choices use the next one along.
func TestWriteNFOPrecedence(t *testing.T) {
	cs := newTestChannelStore(t)
	yes, no := true, false
	profileYes := addTestProfile(t, cs, "writes NFO", &models.Settings{}, &models.MetarrArgs{WriteNFO: &yes})
	const none int64 = 0

	for i, tc := range []struct {
		name                    string
		chanProfile, urlProfile int64
		chanOwn, urlOwn         *bool
		wantChan, wantURL       *bool
	}{
		{"all unset", none, none, nil, nil, nil, nil},
		{"channel's profile", profileYes.ID, none, nil, nil, &yes, &yes},
		{"channel's No beats its profile's Yes", profileYes.ID, none, &no, nil, &no, &no},
		{"URL's profile beats the channel's No", none, profileYes.ID, &no, nil, &no, &yes},
		{"URL's No beats its profile's Yes", none, profileYes.ID, nil, &no, nil, &no},
		{"URL's No beats the channel's Yes", none, none, &yes, &no, &yes, &no},
		{"URL's Yes beats the channel's No", none, none, &no, &yes, &no, &yes},
	} {
		c := testChannel(t, "nfo"+strconv.Itoa(i), "https://example.com/@nfo"+strconv.Itoa(i))
		c.ProfileID = tc.chanProfile
		c.ChanMetarrArgs.WriteNFO = tc.chanOwn
		c.URLModels[0].ProfileID = tc.urlProfile
		c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{WriteNFO: tc.urlOwn}
		if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
		if err != nil || loaded.LoadErr != nil {
			t.Fatalf("%s: GetChannelModel = %v, LoadErr %v", tc.name, err, loaded.LoadErr)
		}
		if got := loaded.ChanMetarrArgs.WriteNFO; !reflect.DeepEqual(got, tc.wantChan) {
			t.Errorf("%s: channel got %v, want %v", tc.name, showBool(got), showBool(tc.wantChan))
		}
		if got := loaded.URLModels[0].ChanURLMetarrArgs.WriteNFO; !reflect.DeepEqual(got, tc.wantURL) {
			t.Errorf("%s: URL got %v, want %v", tc.name, showBool(got), showBool(tc.wantURL))
		}
	}
}

// showBool returns an optional choice as text.
func showBool(b *bool) string {
	if b == nil {
		return "unset"
	}
	return strconv.FormatBool(*b)
}

// TestProfileOpsAdded tests that a profile's ops are added to those of the channel or URL using it, as a URL's are to
// its channel's: the nearer op wins where both set the same thing, and the nearer move ops are checked first.
func TestProfileOpsAdded(t *testing.T) {
	cs := newTestChannelStore(t)
	dir := t.TempDir()
	catFilter := models.Filters{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}
	profileTitle := models.MetaOps{Field: "title", OpType: "prefix", OpValue: "[P] "}
	profileArtist := models.MetaOps{Field: "artist", OpType: "set", OpValue: "Profile Band"}
	channelArtist := models.MetaOps{Field: "artist", OpType: "set", OpValue: "Channel Band"}
	urlAlbum := models.MetaOps{Field: "album", OpType: "set", OpValue: "URL Album"}
	profilePrefix := models.FilenameOps{OpType: "prefix", OpValue: "[P] "}
	channelSuffix := models.FilenameOps{OpType: "suffix", OpValue: " [C]"}
	profileFiltered := models.FilteredMetaOps{Filters: []models.Filters{catFilter}, MetaOps: []models.MetaOps{{Field: "genre", OpType: "set", OpValue: "Cats"}}}
	profileMove := models.MetaFilterMoveOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}}, OutputDir: dir}
	channelMove := models.MetaFilterMoveOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "dog", MustAny: "must"}}, OutputDir: dir}

	chanProfile := addTestProfile(t, cs, "ops profile",
		&models.Settings{MetaFilterMoveOps: []models.MetaFilterMoveOps{profileMove}},
		&models.MetarrArgs{MetaOps: []models.MetaOps{profileTitle, profileArtist}, FilenameOps: []models.FilenameOps{profilePrefix}, FilteredMetaOps: []models.FilteredMetaOps{profileFiltered}})
	urlProfile := addTestProfile(t, cs, "URL ops profile", nil, &models.MetarrArgs{MetaOps: []models.MetaOps{urlAlbum}})

	c := testChannel(t, "ops chan", "https://example.com/@ops")
	c.ProfileID = chanProfile.ID
	c.ChanSettings.MetaFilterMoveOps = []models.MetaFilterMoveOps{channelMove}
	c.ChanMetarrArgs.MetaOps = []models.MetaOps{channelArtist}
	c.ChanMetarrArgs.FilenameOps = []models.FilenameOps{channelSuffix}
	c.URLModels[0].ProfileID = urlProfile.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	check := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", what, got, want)
		}
	}
	m := loaded.ChanMetarrArgs
	check("channel meta ops", m.MetaOps, []models.MetaOps{profileTitle, channelArtist})
	check("channel filename ops", m.FilenameOps, []models.FilenameOps{profilePrefix, channelSuffix})
	check("channel filtered meta ops", m.FilteredMetaOps, []models.FilteredMetaOps{profileFiltered})
	check("channel move ops", loaded.ChanSettings.MetaFilterMoveOps, []models.MetaFilterMoveOps{channelMove, profileMove})

	// The URL's profile's ops are added to the channel's, which hold the channel profile's.
	cu := loaded.URLModels[0]
	check("URL meta ops", cu.ChanURLMetarrArgs.MetaOps, []models.MetaOps{profileTitle, channelArtist, urlAlbum})
	check("URL filename ops", cu.ChanURLMetarrArgs.FilenameOps, []models.FilenameOps{profilePrefix, channelSuffix})
}

// TestProfileOutputDirectoryTag tests a profile's output directory tag fills from the video directory on a channel, and the channel's output directory on a URL.
func TestProfileOutputDirectoryTag(t *testing.T) {
	cs := newTestChannelStore(t)
	p := addTestProfile(t, cs, "shorts", nil, &models.MetarrArgs{OutputDir: "{{channel_output_directory}}/Shorts"})

	// On a channel, whose URLs inherit it already filled.
	onChannel := testChannel(t, "on channel", "https://example.com/@a")
	onChannel.ProfileID = p.ID
	if err := cs.AddChannelWithDetails(onChannel, nil, nil); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(onChannel.ID, 10), true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(onChannel.ChanSettings.VideoDir, "Shorts")
	if got := loaded.URLModels[0].ChanURLMetarrArgs.OutputDir; got != want {
		t.Errorf("profile on a channel: got %q, want %q", got, want)
	}

	// On a channel URL.
	onURL := testChannel(t, "on url", "https://example.com/@b")
	onURL.ChanMetarrArgs.OutputDir = t.TempDir()
	onURL.URLModels[0].ProfileID = p.ID
	if err := cs.AddChannelWithDetails(onURL, nil, nil); err != nil {
		t.Fatal(err)
	}
	if loaded, _, err = cs.GetChannelModel(consts.QChanID, strconv.FormatInt(onURL.ID, 10), true); err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(onURL.ChanMetarrArgs.OutputDir, "Shorts")
	if got := loaded.URLModels[0].ChanURLMetarrArgs.OutputDir; got != want {
		t.Errorf("profile on a channel URL: got %q, want %q", got, want)
	}
}

// TestChannelVideoDirFromProfiletests that a channel can use its profile's video directory, but must have one.
func TestChannelVideoDirFromProfile(t *testing.T) {
	cs := newTestChannelStore(t)
	withDir := addTestProfile(t, cs, "with folder", &models.Settings{VideoDir: filepath.Join(t.TempDir(), "{{channel_name}}")}, nil)
	without := addTestProfile(t, cs, "without folder", nil, nil)

	c := testChannel(t, "no folder", "https://example.com/@a")
	c.ChanSettings.VideoDir = ""
	c.ProfileID = without.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Errorf("adding a channel without a video directory, its own or its profile's: %v", err)
	}

	c = testChannel(t, "profile folder", "https://example.com/@b")
	c.ChanSettings.VideoDir = ""
	c.ProfileID = withDir.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatalf("adding a channel using its profile's video directory: %v", err)
	}
	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
	if err != nil {
		t.Fatal(err)
	}
	if s := loaded.ChanSettings; s.VideoDir != withDir.ProfileSettings.VideoDir || s.JSONDir != s.VideoDir {
		t.Errorf("video directory %q, JSON directory %q, want the profile's video directory for both", s.VideoDir, s.JSONDir)
	}

	// It can't lose the profile's video directory.
	if err := cs.SetChannelProfile(consts.QChanID, strconv.FormatInt(c.ID, 10), without.ID); err == nil {
		t.Error("switched a channel without a video directory to a profile without one")
	}
	withDir.ProfileSettings.VideoDir = ""
	if err := cs.UpdateProfile(withDir); err == nil {
		t.Error("cleared the video directory of a profile a channel uses for one")
	}
}

// TestProfileAndVideoDirChangedTogether tests that switching to a profile with a video directory and clearing the
// channel's own in one transaction works in either order.
func TestProfileAndVideoDirChangedTogether(t *testing.T) {
	for _, profileFirst := range []bool{true, false} {
		cs := newTestChannelStore(t)
		p := addTestProfile(t, cs, "with folder", &models.Settings{VideoDir: t.TempDir()}, nil)
		if err := cs.AddChannelWithDetails(testChannel(t, "chan", "https://example.com/@a"), nil, nil); err != nil {
			t.Fatal(err)
		}
		setProfile := func(tcs contracts.ChannelStore) error {
			return tcs.SetChannelProfile(consts.QChanName, "chan", p.ID)
		}
		clearVideoDir := func(tcs contracts.ChannelStore) error {
			_, err := tcs.UpdateChannelSettingsJSON(consts.QChanName, "chan", func(s *models.Settings) error {
				s.VideoDir = ""
				return nil
			})
			return err
		}
		steps := []func(contracts.ChannelStore) error{setProfile, clearVideoDir}
		if !profileFirst {
			slices.Reverse(steps)
		}

		err := cs.WithTx(func(tcs contracts.ChannelStore) error {
			for _, step := range steps {
				if err := step(tcs); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Errorf("profile first %t: %v", profileFirst, err)
		}
	}
}

// TestDeleteProfileInUse tests that a profile can't be deleted while a channel or URL uses it.
func TestDeleteProfileInUse(t *testing.T) {
	cs := newTestChannelStore(t)
	p := addTestProfile(t, cs, "shared", nil, nil)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ProfileID = p.ID
	c.URLModels[0].ProfileID = p.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	err := cs.DeleteProfile(consts.QProfileName, "shared")
	if !errors.Is(err, contracts.ErrInvalidProfile) || !strings.Contains(err.Error(), `channel "chan"`) || !strings.Contains(err.Error(), "@a") {
		t.Errorf("DeleteProfile = %v, want it refused naming the channel and its URL", err)
	}

	// Once neither uses it, it's deleted.
	if err := cs.SetChannelProfile(consts.QChanName, "chan", 0); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ProfileID = 0
	if err := cs.UpdateChannelURLSettings(urls[0]); err != nil {
		t.Fatal(err)
	}
	if err := cs.DeleteProfile(consts.QProfileName, "shared"); err != nil {
		t.Errorf("deleting an unused profile: %v", err)
	}
	if _, found, err := cs.GetProfile(consts.QProfileName, "shared"); found || err != nil {
		t.Errorf("after deleting, GetProfile = found %t, %v", found, err)
	}
}

// TestProfileNamesUnique tests that each profile needs a name no other profile has.
func TestProfileNamesUnique(t *testing.T) {
	cs := newTestChannelStore(t)
	addTestProfile(t, cs, "music", nil, nil)
	other := addTestProfile(t, cs, "podcasts", nil, nil)

	for _, name := range []string{"music", " "} {
		if err := cs.AddProfile(&models.Profile{Name: name}); !errors.Is(err, contracts.ErrInvalidProfile) {
			t.Errorf("adding a profile named %q: %v", name, err)
		}
	}
	other.Name = "music"
	if err := cs.UpdateProfile(other); !errors.Is(err, contracts.ErrInvalidProfile) {
		t.Errorf("renaming a profile to another's name: %v", err)
	}
	other.Name = "talks"
	if err := cs.UpdateProfile(other); err != nil {
		t.Errorf("renaming a profile: %v", err)
	}
}

// TestUnusableProfileMarksChannel tests that a channel whose profile is missing, or whose URL's profile has invalid
// saved settings, loads with an error.
func TestUnusableProfileMarksChannel(t *testing.T) {
	cs := newTestChannelStore(t)
	p := addTestProfile(t, cs, "broken", nil, nil)
	missing := testChannel(t, "missing profile", "https://example.com/@a")
	invalid := testChannel(t, "invalid URL profile", "https://example.com/@b")
	invalid.URLModels[0].ProfileID = p.ID
	for _, c := range []*models.Channel{missing, invalid} {
		if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	// A missing profile, and one with a value a newer version stopped accepting.
	if _, err := cs.db.Exec(`UPDATE channels SET profile_id = 999 WHERE id = ?`, missing.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.db.Exec(`UPDATE profiles SET settings = ? WHERE id = ?`, `{"ytdlp_output_ext":"bogus"}`, p.ID); err != nil {
		t.Fatal(err)
	}

	for _, merged := range []bool{true, false} {
		chans, _, err := cs.GetAllChannels(merged)
		if err != nil || len(chans) != 2 {
			t.Fatalf("GetAllChannels(%t) = %d channels, %v; want both", merged, len(chans), err)
		}
		for _, c := range chans {
			if c.LoadErr == nil {
				t.Errorf("merged %t: channel %q loaded without an error", merged, c.Name)
			}
		}
	}
	if got, _, err := cs.GetProfile(consts.QProfileID, strconv.FormatInt(p.ID, 10)); err != nil || got.LoadErr == nil {
		t.Errorf("GetProfile = %v, LoadErr nil; want the profile loaded with its error so it can be fixed", err)
	}
}

// TestProfileOutputExtCheckedWithUsers tests that a profile's output container is checked against the codecs of the
// channels and URLs using it.
func TestProfileOutputExtCheckedWithUsers(t *testing.T) {
	cs := newTestChannelStore(t)
	cu := addCodecChannel(t, cs, "chan")
	webm := addTestProfile(t, cs, "webm", nil, &models.MetarrArgs{OutputExt: "webm"})
	mp4 := addTestProfile(t, cs, "mp4", nil, &models.MetarrArgs{OutputExt: "mp4"})

	if err := cs.SetChannelProfile(consts.QChanName, "chan", webm.ID); err == nil {
		t.Error("gave a channel with an h264 URL codec a webm profile")
	}
	cu.ProfileID = webm.ID
	if err := cs.UpdateChannelURLSettings(cu); err == nil {
		t.Error("gave a URL with an h264 codec a webm profile")
	}

	if err := cs.SetChannelProfile(consts.QChanName, "chan", mp4.ID); err != nil {
		t.Fatalf("giving the channel an mp4 profile: %v", err)
	}
	mp4.ProfileMetarrArgs.OutputExt = "webm"
	if err := cs.UpdateProfile(mp4); err == nil {
		t.Error("changed a used profile's output to webm under an h264 URL codec")
	}
}

// TestConfigReloadSetsProfiles tests that reloading a channel's config file sets the profiles it names for the channel
// and its URL.
func TestConfigReloadSetsProfiles(t *testing.T) {
	cs := newTestChannelStore(t)
	music := addTestProfile(t, cs, "music", nil, nil)
	podcasts := addTestProfile(t, cs, "podcasts", nil, nil)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	c.ChannelConfigFile = writeChannelConfig(t, strings.Join([]string{
		"profile: music",
		"url-settings:",
		"  'https://example.com/@a':",
		"    profile: podcasts",
	}, "\n")+"\n")
	if err := cs.UpdateChannelFromConfig(c); err != nil {
		t.Fatal(err)
	}
	saved, _, err := cs.GetChannelModel(consts.QChanName, "chan", false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ProfileID != music.ID || saved.URLModels[0].ProfileID != podcasts.ID {
		t.Errorf("channel profile %d and URL profile %d, want %d and %d", saved.ProfileID, saved.URLModels[0].ProfileID, music.ID, podcasts.ID)
	}

	c.ChannelConfigFile = writeChannelConfig(t, "profile: missing\n")
	if err := cs.UpdateChannelFromConfig(c); err == nil {
		t.Error("reloaded a config file naming a profile that doesn't exist")
	}
}

// TestConfigReloadSavesOwnSettings tests that reloading a channel's config file saves only its own settings, and
// leaves the channel filled in from its profile.
func TestConfigReloadSavesOwnSettings(t *testing.T) {
	cs := newTestChannelStore(t)
	p := addTestProfile(t, cs, "music", &models.Settings{Retries: 5}, &models.MetarrArgs{OutputExt: "mp4"})
	added := testChannel(t, "chan", "https://example.com/@a")
	added.ProfileID = p.ID
	if err := cs.AddChannelWithDetails(added, nil, nil); err != nil {
		t.Fatal(err)
	}
	c, _, err := cs.GetChannelModel(consts.QChanName, "chan", true)
	if err != nil {
		t.Fatal(err)
	}

	c.ChannelConfigFile = writeChannelConfig(t, "crawl-freq: 30\n")
	if err := cs.UpdateChannelFromConfig(c); err != nil {
		t.Fatal(err)
	}
	if s, m := c.ChanSettings, c.ChanMetarrArgs; s.CrawlFreq != 30 || s.Retries != 5 || m.OutputExt != "mp4" {
		t.Errorf("reloaded channel: crawl frequency %d, retries %d, output extension %q; want 30, and its profile's 5 and mp4", s.CrawlFreq, s.Retries, m.OutputExt)
	}
	saved, _, err := cs.GetChannelModel(consts.QChanName, "chan", false)
	if err != nil {
		t.Fatal(err)
	}
	if s, m := saved.ChanSettings, saved.ChanMetarrArgs; s.CrawlFreq != 30 || s.Retries != 0 || m.OutputExt != "" {
		t.Errorf("saved channel: crawl frequency %d, retries %d, output extension %q; want 30 and none of its profile's", s.CrawlFreq, s.Retries, m.OutputExt)
	}
}

// ******************************** Redownloading *************************************************************************************

const (
	redownloadChannelURL = "https://example.com/@a"
	redownloadVideoURL   = "https://example.com/v/1"
)

// redownloadStores returns stores on a fresh database with one channel and channel URL.
func redownloadStores(t *testing.T) (*ChannelStore, *VideoStore, *DownloadStore, *models.Channel, int64) {
	t.Helper()
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()
	if res, err = db.Exec("INSERT INTO channel_urls (channel_id, url) VALUES (?, ?)", chanID, redownloadChannelURL); err != nil {
		t.Fatal(err)
	}
	cuID, _ := res.LastInsertId()
	cs, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return cs, GetVideoStore(db), GetDownloadStore(db), &models.Channel{ID: chanID, Name: "Test"}, cuID
}

// TestIgnoredVideoReturnsToAvailable tests an ignored video chosen again shows on the Available page if it then fails or
// is canceled before its metadata is saved, rather than on no page at all.
func TestIgnoredVideoReturnsToAvailable(t *testing.T) {
	for _, status := range []consts.DownloadStatus{consts.DLStatusFailed, consts.DLStatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			_, vs, ds, c, cuID := redownloadStores(t)
			v := &models.Video{ChannelURLID: cuID, ChannelURL: redownloadChannelURL, URL: redownloadVideoURL, Metadata: models.VideoMetadata{Title: "One"}}
			v.MarkVideoAsIgnored()
			if _, err := vs.AddVideos([]*models.Video{v}, c.ID); err != nil {
				t.Fatal(err)
			}

			chosen := &models.Video{URL: redownloadVideoURL, ChannelURLID: cuID, ChannelURL: redownloadChannelURL}
			if claimed, err := vs.QueueVideo(c.ID, chosen, true); err != nil || !claimed {
				t.Fatalf("QueueVideo = %v, %v", claimed, err)
			}
			if err := ds.UpdateDownloadStatus(context.Background(), models.StatusUpdate{VideoID: chosen.ID, VideoURL: chosen.URL, Status: status, Error: errors.New("boom")}); err != nil {
				t.Fatal(err)
			}

			available, err := vs.GetAvailableVideos(app.AvailableStatuses...)
			if err != nil {
				t.Fatal(err)
			}
			if len(available) != 1 || available[0].DownloadStatus.Status != status {
				t.Errorf("Available videos = %+v, want the video with status %q", available, status)
			}
		})
	}
}

// TestRestartRecoversRedownload tests a video re-downloaded when Tubarr was killed can be downloaded again after a restart.
func TestRestartRecoversRedownload(t *testing.T) {
	for _, tc := range []struct {
		name, column string
		status       consts.DownloadStatus
	}{
		{"finished", consts.QVidFinished, consts.DLStatusCompleted},
		{"ignored", consts.QVidIgnored, consts.DLStatusIgnored},
		{"available", "", consts.DLStatusAvailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, vs, _, c, cuID := redownloadStores(t)
			db := cs.GetDB()
			videos := []*models.Video{{ChannelURLID: cuID, ChannelURL: redownloadChannelURL, URL: redownloadVideoURL, Metadata: models.VideoMetadata{Title: "One"}}}
			if _, err := vs.AddAvailableVideos(videos, c.ID); err != nil {
				t.Fatal(err)
			}
			if tc.column != "" {
				if _, err := db.Exec("UPDATE videos SET " + tc.column + " = 1"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("UPDATE downloads SET status = ?", tc.status); err != nil {
					t.Fatal(err)
				}
			}
			if claimed, err := vs.QueueVideo(c.ID, &models.Video{URL: redownloadVideoURL}, true); err != nil || !claimed {
				t.Fatalf("QueueVideo = %v, %v", claimed, err)
			}

			if _, _, err := vs.RecoverInterrupted(); err != nil {
				t.Fatal(err)
			}
			var status consts.DownloadStatus
			if err := db.QueryRow("SELECT status FROM downloads").Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != consts.DLStatusFailed {
				t.Errorf("status after restart = %q, want %q", status, consts.DLStatusFailed)
			}
			if claimed, err := vs.QueueVideo(c.ID, &models.Video{URL: redownloadVideoURL}, true); err != nil || !claimed {
				t.Errorf("QueueVideo after restart = %v, %v, want it claimed", claimed, err)
			}
		})
	}
}

// TestRestartKeepsFinishedDownloads tests finished downloads aren't marked failed by the restart recovery.
func TestRestartKeepsFinishedDownloads(t *testing.T) {
	cs, vs, _, c, cuID := redownloadStores(t)
	db := cs.GetDB()
	videos := []*models.Video{{ChannelURLID: cuID, ChannelURL: redownloadChannelURL, URL: redownloadVideoURL, Metadata: models.VideoMetadata{Title: "One"}}}
	if _, err := vs.AddAvailableVideos(videos, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE videos SET finished = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE downloads SET status = ?", consts.DLStatusCompleted); err != nil {
		t.Fatal(err)
	}

	if _, _, err := vs.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	var status consts.DownloadStatus
	if err := db.QueryRow("SELECT status FROM downloads").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != consts.DLStatusCompleted {
		t.Errorf("status after restart = %q, want %q", status, consts.DLStatusCompleted)
	}
}

// ******************************** Replacing inherited lists *************************************************************************

// TestReplaceInheritedLists tests that a channel's replace tick leaves out its profile's list and file, and a URL's
// leaves out the channel's but keeps its own profile's, while unticked lists are still added together.
func TestReplaceInheritedLists(t *testing.T) {
	cs := newTestChannelStore(t)
	opsFile := filepath.Join(t.TempDir(), "profile-meta-ops.txt")
	if err := os.WriteFile(opsFile, []byte("title:set:From Profile File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profileFilter := models.Filters{Field: "title", FilterType: "omits", Value: "dog", MustAny: "must"}
	channelFilter := models.Filters{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}
	profileOp := models.MetaOps{Field: "title", OpType: "prefix", OpValue: "[P] "}
	channelOp := models.MetaOps{Field: "artist", OpType: "set", OpValue: "Channel Band"}
	urlProfileOp := models.MetaOps{Field: "album", OpType: "set", OpValue: "URL Profile Album"}
	urlOp := models.MetaOps{Field: "genre", OpType: "set", OpValue: "URL Genre"}
	profileSuffix := models.FilenameOps{OpType: "suffix", OpValue: " [P]"}
	channelPrefix := models.FilenameOps{OpType: "prefix", OpValue: "[C] "}

	chanProfile := addTestProfile(t, cs, "replaced profile",
		&models.Settings{Filters: []models.Filters{profileFilter}},
		&models.MetarrArgs{MetaOps: []models.MetaOps{profileOp}, MetaOpsFile: opsFile, FilenameOps: []models.FilenameOps{profileSuffix}})
	urlProfile := addTestProfile(t, cs, "URL profile", nil, &models.MetarrArgs{MetaOps: []models.MetaOps{urlProfileOp}})

	const (
		replacing = "https://example.com/@replacing"
		emptied   = "https://example.com/@emptied"
		adding    = "https://example.com/@adding"
	)
	c := testChannel(t, "replace chan", replacing, emptied, adding)
	c.ProfileID = chanProfile.ID
	c.ChanSettings.Filters = []models.Filters{channelFilter}
	c.ChanSettings.FiltersReplaceInherited = true
	c.ChanMetarrArgs.MetaOps = []models.MetaOps{channelOp}
	c.ChanMetarrArgs.MetaOpsReplaceInherited = true
	c.ChanMetarrArgs.FilenameOps = []models.FilenameOps{channelPrefix}
	c.URLModels[0].ProfileID = urlProfile.ID
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{MetaOps: []models.MetaOps{urlOp}, MetaOpsReplaceInherited: true}
	c.URLModels[1].ChanURLMetarrArgs = &models.MetarrArgs{MetaOpsReplaceInherited: true} // Ticked with nothing else, so it must still be saved.
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)

	loaded, _, err := cs.GetChannelModel(consts.QChanID, id, true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	check := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", what, got, want)
		}
	}

	// The channel replaces its profile's filters and meta ops, file included, but still adds to its filename ops.
	check("channel filters", loaded.ChanSettings.Filters, []models.Filters{channelFilter})
	check("channel meta ops", loaded.ChanMetarrArgs.MetaOps, []models.MetaOps{channelOp})
	check("channel meta ops file", loaded.ChanMetarrArgs.MetaOpsFile, "")
	check("channel filename ops", loaded.ChanMetarrArgs.FilenameOps, []models.FilenameOps{profileSuffix, channelPrefix})

	urls := map[string]*models.ChannelURL{}
	for _, cu := range loaded.URLModels {
		urls[cu.URL] = cu
	}
	check("replacing URL meta ops", urls[replacing].ChanURLMetarrArgs.MetaOps, []models.MetaOps{urlProfileOp, urlOp})
	check("replacing URL filename ops", urls[replacing].ChanURLMetarrArgs.FilenameOps, []models.FilenameOps{profileSuffix, channelPrefix})
	check("emptied URL meta ops", len(urls[emptied].ChanURLMetarrArgs.MetaOps), 0)
	check("adding URL meta ops", urls[adding].ChanURLMetarrArgs.MetaOps, []models.MetaOps{channelOp})

	// A tick alone is saved, as the URL's own.
	saved, _, err := cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, cu := range saved.URLModels {
		if cu.URL == emptied && (cu.ChanURLMetarrArgs == nil || !cu.ChanURLMetarrArgs.MetaOpsReplaceInherited) {
			t.Errorf("a URL ticked with nothing else loads as saved with %+v, want its tick kept", cu.ChanURLMetarrArgs)
		}
	}
}

// ******************************** Saving settings ***********************************************************************************

// addCodecChannel adds a channel whose one URL transcodes video to h264.
func addCodecChannel(t *testing.T, cs *ChannelStore, name string) *models.ChannelURL {
	t.Helper()
	c := testChannel(t, name, "https://example.com/@"+name)
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	return urls[0]
}

// setChannelOutputExt sets channel "chan"'s Metarr output extension.
func setChannelOutputExt(cs contracts.ChannelStore, ext string) error {
	_, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.OutputExt = ext
		return nil
	})
	return err
}

// TestAddChannelKeepsCodecOnlyURLSettings tests that a URL override setting only preferred codecs is saved with the channel.
func TestAddChannelKeepsCodecOnlyURLSettings(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.URLModels[0].ChanURLSettings = &models.Settings{
		YTDLPPreferredVideoCodecs: []string{"av1"},
		YTDLPPreferredAudioCodecs: []string{"opus"},
	}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 {
		t.Fatalf("got %d URLs, want 1", len(urls))
	}
	s := urls[0].ChanURLSettings
	if !slices.Equal(s.YTDLPPreferredVideoCodecs, []string{"av1"}) || !slices.Equal(s.YTDLPPreferredAudioCodecs, []string{"opus"}) {
		t.Errorf("URL's preferred codecs not saved, got video %v, audio %v", s.YTDLPPreferredVideoCodecs, s.YTDLPPreferredAudioCodecs)
	}
}

// TestURLSettingsSavedOnlyIfLoadable tests that URL settings loading would reject are refused when saved.
func TestURLSettingsSavedOnlyIfLoadable(t *testing.T) {
	bad := &models.MetarrArgs{OutputExt: "xyz"}

	cs := newTestChannelStore(t)
	c := testChannel(t, "added", "https://example.com/@a")
	c.URLModels[0].ChanURLMetarrArgs = bad
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("AddChannelWithDetails saved a URL Metarr output extension the channel can't load")
	}

	c = testChannel(t, "updated", "https://example.com/@b")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ChanURLMetarrArgs = bad
	if err := cs.UpdateChannelURLSettings(urls[0]); err == nil {
		t.Error("UpdateChannelURLSettings saved a URL Metarr output extension the channel can't load")
	}
}

// TestURLSettingsSaveLowercasesExtensions tests that a URL's output extensions are saved lowercase.
func TestURLSettingsSaveLowercasesExtensions(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ChanURLSettings = &models.Settings{YtdlpOutputExt: "MKV"}
	urls[0].ChanURLMetarrArgs = &models.MetarrArgs{OutputExt: "MP4"}
	if err := cs.UpdateChannelURLSettings(urls[0]); err != nil {
		t.Fatal(err)
	}
	var settingsJSON, metarrJSON string
	query := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s = ?", consts.QChanURLSettings, consts.QChanURLMetarr, consts.DBChannelURLs, consts.QChanURLID)
	if err := cs.db.QueryRow(query, urls[0].ID).Scan(&settingsJSON, &metarrJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(settingsJSON, `"mkv"`) || !strings.Contains(metarrJSON, `"mp4"`) {
		t.Errorf("extensions not saved lowercase, got settings %s, Metarr args %s", settingsJSON, metarrJSON)
	}
}

// TestAddChannelRefusesCodecsContainerCantHold tests that a URL codec its channel's output container can't hold is refused.
func TestAddChannelRefusesCodecsContainerCantHold(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ChanMetarrArgs.OutputExt = "webm"
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("AddChannelWithDetails saved an h264 URL codec under a webm channel")
	}
}

// TestChannelOutputExtRefusedOverURLCodecs tests that a channel output container its URLs' codecs can't go in is refused.
func TestChannelOutputExtRefusedOverURLCodecs(t *testing.T) {
	cs := newTestChannelStore(t)
	addCodecChannel(t, cs, "chan")
	if err := setChannelOutputExt(cs, "webm"); err == nil {
		t.Error("saved a webm channel output with an h264 URL codec")
	}
	if err := setChannelOutputExt(cs, "mp4"); err != nil {
		t.Errorf("refused an mp4 channel output with an h264 URL codec: %v", err)
	}
}

// TestURLCodecRefusedOverChannelOutputExt tests that a URL codec its channel's output container can't hold is refused when updated.
func TestURLCodecRefusedOverChannelOutputExt(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ChanMetarrArgs.OutputExt = "webm"
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}
	if err := cs.UpdateChannelURLSettings(urls[0]); err == nil {
		t.Error("UpdateChannelURLSettings saved an h264 URL codec under a webm channel")
	}
}

// TestChannelAndURLChangesJudgedTogether tests that channel and URL changes saved in one transaction are checked
// once both are written, in either order.
func TestChannelAndURLChangesJudgedTogether(t *testing.T) {
	for _, channelFirst := range []bool{true, false} {
		cs := newTestChannelStore(t)
		cu := addCodecChannel(t, cs, "chan")
		err := cs.WithTx(func(tcs contracts.ChannelStore) error {
			cu.ChanURLMetarrArgs = nil
			if channelFirst {
				if err := setChannelOutputExt(tcs, "webm"); err != nil {
					return err
				}
				return tcs.UpdateChannelURLSettings(cu)
			}
			if err := tcs.UpdateChannelURLSettings(cu); err != nil {
				return err
			}
			return setChannelOutputExt(tcs, "webm")
		})
		if err != nil {
			t.Errorf("channel first %t: refused a webm channel output saved with its URL's h264 codec removed: %v", channelFirst, err)
		}
	}
}

// ******************************** Transactions **************************************************************************************

// newTestChannelStore returns a channel store on a fresh database.
func newTestChannelStore(t *testing.T) *ChannelStore {
	t.Helper()
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cs, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// testChannel returns a valid channel with the given URLs.
func testChannel(t *testing.T, name string, urls ...string) *models.Channel {
	t.Helper()
	c := &models.Channel{
		Name:           name,
		ChanSettings:   &models.Settings{VideoDir: t.TempDir()},
		ChanMetarrArgs: &models.MetarrArgs{},
	}
	for _, u := range urls {
		c.URLModels = append(c.URLModels, &models.ChannelURL{URL: u})
	}
	return c
}

// TestWithTxRollsBack tests that WithTx undoes every write, including nested transactions, when fn fails.
func TestWithTxRollsBack(t *testing.T) {
	cs := newTestChannelStore(t)
	errStop := errors.New("stop")

	err := cs.WithTx(func(tcs contracts.ChannelStore) error {
		id, err := tcs.AddChannel(testChannel(t, "rolled back", "https://example.com/@a"))
		if err != nil {
			return err
		}
		if err := tcs.AddNotifyURLs(id, []*models.Notification{{NotifyURL: "https://notify.example.com"}}); err != nil {
			return err
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("expected fn's error, got %v", err)
	}
	if cs.channelExists(consts.QChanName, "rolled back") {
		t.Error("channel added in a failed transaction was kept")
	}
	var notifications int
	if err := cs.db.QueryRow("SELECT COUNT(*) FROM " + consts.DBNotifications).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications != 0 {
		t.Errorf("nested transaction's notification was kept, got %d", notifications)
	}

	if err := cs.WithTx(func(tcs contracts.ChannelStore) error {
		_, err := tcs.AddChannel(testChannel(t, "committed", "https://example.com/@b"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !cs.channelExists(consts.QChanName, "committed") {
		t.Error("channel added in a successful transaction is missing")
	}
}

// TestAddChannelWithDetailsRollsBack tests that a channel isn't added when its logins can't be.
func TestAddChannelWithDetailsRollsBack(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	err := cs.AddChannelWithDetails(c, map[string]*models.ChannelURLAccessDetails{"https://example.com/@other": {Username: "bob"}}, nil)
	if !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Fatalf("expected ErrInvalidChannel for a login on a URL not in the channel, got %v", err)
	}
	if cs.channelExists(consts.QChanName, "chan") {
		t.Error("channel was added although its logins failed")
	}
}

// TestDeleteChannelCascades tests that deleting a channel removes its URLs and notifications, on any pooled connection.
func TestDeleteChannelCascades(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, []*models.Notification{{NotifyURL: "https://notify.example.com"}}); err != nil {
		t.Fatal(err)
	}

	// Hold the connection the database was set up on, so the delete runs on another one.
	conn, err := cs.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if err := cs.DeleteChannel(consts.QChanID, strconv.FormatInt(c.ID, 10)); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{consts.DBChannelURLs, consts.DBNotifications} {
		var n int
		if err := conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table+" WHERE channel_id = ?", c.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: %d row(s) left behind by the deleted channel", table, n)
		}
	}
}

// TestDeleteVideoLinksCascade tests that deleting a URL unlinks its videos without deleting them, and deleting a video
// removes its download row, on any pooled connection.
func TestDeleteVideoLinksCascade(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	v := &models.Video{URL: "https://example.com/v/1", ChannelURL: "https://example.com/@a"}
	videoID, err := GetVideoStore(cs.db).AddVideo(v, c.ID, c.URLModels[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	// Hold the connection the database was set up on, so the deletes run on another one.
	conn, err := cs.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	count := func(query string) int {
		t.Helper()
		var n int
		if err := conn.QueryRowContext(t.Context(), query, videoID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := cs.DeleteChannelURL(c.URLModels[0].ID); err != nil {
		t.Fatal(err)
	}
	if n := count("SELECT COUNT(*) FROM " + consts.DBVideos + " WHERE id = ? AND channel_url_id IS NULL"); n != 1 {
		t.Errorf("deleting the URL left its video linked to it, or deleted the video (%d unlinked rows, want 1)", n)
	}

	if err := cs.DeleteVideosByURLs(c.ID, []string{v.URL}, false); err != nil {
		t.Fatal(err)
	}
	if n := count("SELECT COUNT(*) FROM " + consts.DBDownloads + " WHERE video_id = ?"); n != 0 {
		t.Errorf("deleting the video left %d download row(s) behind", n)
	}
}

// TestAddNotifyURLsFailureReleasesTx tests that a failed AddNotifyURLs rolls back, leaving the database writable.
func TestAddNotifyURLsFailureReleasesTx(t *testing.T) {
	cs := newTestChannelStore(t)
	id, err := cs.AddChannel(testChannel(t, "chan", "https://example.com/@a"))
	if err != nil {
		t.Fatal(err)
	}

	if err := cs.AddNotifyURLs(id, []*models.Notification{{NotifyURL: "https://notify.example.com"}, {}}); err == nil {
		t.Fatal("expected an error for a notification without a URL")
	}
	notifications, err := cs.GetNotifyURLs(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 0 {
		t.Errorf("failed AddNotifyURLs kept %d notifications", len(notifications))
	}
	if err := cs.UpdateLastScan(id); err != nil {
		t.Fatalf("database not writable after a failed AddNotifyURLs: %v", err)
	}
}

// TestAddAuthPerURL tests that AddAuth sets each URL's login on that URL only.
func TestAddAuthPerURL(t *testing.T) {
	cs := newTestChannelStore(t)
	id, err := cs.AddChannel(testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b"))
	if err != nil {
		t.Fatal(err)
	}

	if err := cs.AddAuth(id, map[string]*models.ChannelURLAccessDetails{
		"https://example.com/@a": {Username: "alice", Password: "secret", LoginURL: "https://example.com/login"},
	}); err != nil {
		t.Fatal(err)
	}
	if user, _, _, err := cs.GetAuth(id, "https://example.com/@a"); err != nil || user != "alice" {
		t.Errorf("URL a: got user %q, %v, want alice", user, err)
	}
	if user, _, _, err := cs.GetAuth(id, "https://example.com/@b"); err != nil || user != "" {
		t.Errorf("URL b: got user %q, %v, want none", user, err)
	}

	err = cs.AddAuth(id, map[string]*models.ChannelURLAccessDetails{"https://example.com/@other": {Username: "bob"}})
	if !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Errorf("expected ErrInvalidChannel for a URL not in the channel, got %v", err)
	}
}

// ******************************** URL names *****************************************************************************************

// TestChannelURLNameSaved tests a channel URL's name is saved with its channel, changed, and saved with a URL added later.
func TestChannelURLNameSaved(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.URLModels[0].Name = "Main"
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil || len(urls) != 1 || urls[0].Name != "Main" {
		t.Fatalf("after adding the channel: got %v, %v; want one URL named %q", urls, err, "Main")
	}

	// Renamed.
	urls[0].Name = "Shorts"
	if err := cs.UpdateChannelURLSettings(urls[0]); err != nil {
		t.Fatal(err)
	}

	// Added later.
	if _, err := cs.AddChannelURL(c.ID, &models.ChannelURL{URL: "https://example.com/@b", Name: "Second"}, false); err != nil {
		t.Fatal(err)
	}

	urls, err = cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, cu := range urls {
		got[cu.URL] = cu.Name
	}
	if want := map[string]string{"https://example.com/@a": "Shorts", "https://example.com/@b": "Second"}; !maps.Equal(got, want) {
		t.Errorf("got names %v, want %v", got, want)
	}
}

// TestAddChannelURLChannelName tests a channel URL gets its channel's name when added, so its log lines are tagged, and
// that adding one to a channel that doesn't exist fails.
func TestAddChannelURLChannelName(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "Cool Channel", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := c.URLModels[0].ChannelName; got != c.Name {
		t.Errorf("URL added with its channel has channel name %q, want %q", got, c.Name)
	}

	added := &models.ChannelURL{URL: "https://example.com/@b"}
	if _, err := cs.AddChannelURL(c.ID, added, false); err != nil {
		t.Fatal(err)
	}
	if added.ChannelName != c.Name {
		t.Errorf("URL added later has channel name %q, want %q", added.ChannelName, c.Name)
	}

	if _, err := cs.AddChannelURL(c.ID+1000, &models.ChannelURL{URL: "https://example.com/@c"}, false); err == nil {
		t.Error("adding a URL to a channel that doesn't exist succeeded")
	}
}

// ******************************** Videos ********************************************************************************************

// TestUpdateVideoPaths tests that paths are only stored while the row still holds the paths a run started from.
func TestUpdateVideoPaths(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	v := &models.Video{URL: "https://example.com/v/1", ChannelURL: "https://example.com/@a"}
	id, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	v.Files.VideoFilePath, v.Files.JSONFilePath = "/in/v.webm", "/in/v.json"
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		t.Fatal(err)
	}

	paths := func() (string, string) {
		t.Helper()
		var video, json string
		if err := cs.db.QueryRow("SELECT "+consts.QVidVideoPath+", "+consts.QVidJSONPath+" FROM "+consts.DBVideos+" WHERE id = ?", id).Scan(&video, &json); err != nil {
			t.Fatal(err)
		}
		return video, json
	}

	// A newer download changed the row, so the stale run's paths are not stored.
	if updated, err := vs.UpdateVideoPaths(id, "/old/v.webm", "/old/v.json", "/out/stale.mp4", "/out/stale.json"); err != nil || updated {
		t.Errorf("stale update: updated=%v err=%v, want not updated", updated, err)
	}
	if video, json := paths(); video != "/in/v.webm" || json != "/in/v.json" {
		t.Errorf("stale update changed paths to %q, %q", video, json)
	}

	if updated, err := vs.UpdateVideoPaths(id, "/in/v.webm", "/in/v.json", "/out/v.mp4", ""); err != nil || !updated {
		t.Fatalf("matching update: updated=%v err=%v, want updated", updated, err)
	}
	if video, json := paths(); video != "/out/v.mp4" || json != "" {
		t.Errorf("paths = %q, %q; want /out/v.mp4 and empty", video, json)
	}
}

// TestAddVideosDuplicateURL tests that a URL listed twice in one batch is stored once without an error.
func TestAddVideosDuplicateURL(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	videos := []*models.Video{
		{URL: "https://example.com/v/1", ChannelURLID: c.URLModels[0].ID, ChannelURL: c.URLModels[0].URL},
		{URL: "https://example.com/v/1", ChannelURLID: c.URLModels[1].ID, ChannelURL: c.URLModels[1].URL},
	}
	for _, v := range videos {
		v.MarkVideoAsIgnored()
	}
	if _, err := vs.AddVideos(videos, c.ID); err != nil {
		t.Fatalf("AddVideos: %v", err)
	}

	var n int
	if err := cs.db.QueryRow("SELECT COUNT(*) FROM "+consts.DBVideos+" WHERE "+consts.QVidURL+" = ?", "https://example.com/v/1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("stored %d rows, want 1", n)
	}
	if videos[0].ID == 0 || videos[1].ID != videos[0].ID {
		t.Errorf("IDs %d and %d, want both the stored row's", videos[0].ID, videos[1].ID)
	}
}

// TestFinishedVideoLists tests the latest finished and the downloaded video lists, newest first, and the failed Metarr count.
func TestFinishedVideoLists(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	cu := c.URLModels[0]
	for day, v := range []*models.Video{
		{URL: "https://example.com/v/old", Finished: true},
		{URL: "https://example.com/v/mid", Finished: true},
		{URL: "https://example.com/v/ignored", Ignored: true},
		{URL: "https://example.com/v/new", Finished: true, Metadata: models.VideoMetadata{Title: "New"}},
		{URL: "https://example.com/v/unfinished"},
	} {
		v.ChannelURL = cu.URL
		id, err := vs.AddVideo(v, c.ID, cu.ID)
		if err != nil {
			t.Fatal(err)
		}
		updated := time.Date(2024, 1, day+1, 0, 0, 0, 0, time.UTC)
		if _, err := cs.db.Exec("UPDATE "+consts.DBVideos+" SET "+consts.QVidUpdatedAt+" = ? WHERE id = ?", updated, id); err != nil {
			t.Fatal(err)
		}
	}
	urls := func(videos []*models.Video) []string {
		got := make([]string, 0, len(videos))
		for _, v := range videos {
			got = append(got, v.URL)
		}
		return got
	}

	latest, err := vs.GetLatestFinishedVideos(t.Context(), c.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/v/new", "https://example.com/v/mid"}; !slices.Equal(urls(latest), want) {
		t.Fatalf("latest finished: got %q, want %q", urls(latest), want)
	}
	if v := latest[0]; v.Metadata.Title != "New" || v.ChannelURLID != cu.ID || v.ChannelURL != cu.URL || !v.Finished {
		t.Errorf("latest finished: fields not read: %+v", v)
	}
	if n, err := vs.CountFinishedVideos(t.Context(), c.ID); err != nil || n != 3 {
		t.Errorf("finished count: got %d, %v, want 3", n, err)
	}

	// Ignored videos are listed on the Available page instead.
	all, _, err := cs.GetDownloadedVideos(c)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com/v/new", "https://example.com/v/mid", "https://example.com/v/old"}
	if !slices.Equal(urls(all), want) {
		t.Fatalf("downloaded: got %q, want %q", urls(all), want)
	}

	// Only downloaded, not ignored, videos Metarr failed on are counted.
	for u, status := range map[string]consts.MetarrStatus{
		"https://example.com/v/old":     consts.MetarrStatusFailed,
		"https://example.com/v/mid":     consts.MetarrStatusFailed,
		"https://example.com/v/new":     consts.MetarrStatusDone,
		"https://example.com/v/ignored": consts.MetarrStatusFailed,
	} {
		if _, err := cs.db.Exec("UPDATE "+consts.DBVideos+" SET "+consts.QVidMetarrStatus+" = ? WHERE "+consts.QVidURL+" = ?", status, u); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := vs.CountFailedMetarrVideos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[c.ID] != 2 {
		t.Errorf("failed Metarr counts: got %v, want 2 for channel %d", counts, c.ID)
	}
}

// TestRecoverInterrupted tests startup repairs videos left mid-Metarr or mid-download, and leaves every other video alone.
func TestRecoverInterrupted(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()

	type row struct {
		finished, ignored    int
		videoPath            any
		metarrStatus, status string
		errMsg               string
	}
	const stopped = "Tubarr stopped before this video finished"
	cases := map[string]struct{ before, want row }{
		"mid-Metarr":              {row{0, 0, "/v/1.mp4", "done", "Finished", ""}, row{1, 0, "/v/1.mp4", "pending", "Finished", ""}},
		"mid-Metarr, no path":     {row{0, 0, nil, "done", "Finished", ""}, row{0, 0, nil, "done", "Failed", stopped}},
		"mid-Metarr, empty path":  {row{0, 0, "", "done", "Finished", ""}, row{0, 0, "", "done", "Failed", stopped}},
		"queued":                  {row{0, 0, nil, "done", "Queued", ""}, row{0, 0, nil, "done", "Failed", stopped}},
		"downloading":             {row{0, 0, nil, "done", "Downloading", ""}, row{0, 0, nil, "done", "Failed", stopped}},
		"available":               {row{0, 0, nil, "done", "Available", ""}, row{0, 0, nil, "done", "Available", ""}},
		"failed":                  {row{0, 0, nil, "done", "Failed", "yt-dlp error"}, row{0, 0, nil, "done", "Failed", "yt-dlp error"}},
		"canceled":                {row{0, 0, nil, "done", "Canceled", ""}, row{0, 0, nil, "done", "Canceled", ""}},
		"ignored":                 {row{0, 1, nil, "done", "Ignored", ""}, row{0, 1, nil, "done", "Ignored", ""}},
		"finished":                {row{1, 0, "/v/2.mp4", "done", "Finished", ""}, row{1, 0, "/v/2.mp4", "done", "Finished", ""}},
		"finished, Metarr failed": {row{1, 0, "/v/3.mp4", "failed", "Finished", ""}, row{1, 0, "/v/3.mp4", "failed", "Finished", ""}},
	}

	ids := map[string]int64{}
	for name, tc := range cases {
		b := tc.before
		res, err := db.Exec("INSERT INTO videos (channel_id, url, finished, ignored, video_path, metarr_status) VALUES (?, ?, ?, ?, ?, ?)",
			chanID, "https://example.com/"+name, b.finished, b.ignored, b.videoPath, b.metarrStatus)
		if err != nil {
			t.Fatal(err)
		}
		ids[name], _ = res.LastInsertId()
		if _, err := db.Exec("INSERT INTO downloads (video_id, status, error) VALUES (?, ?, ?)", ids[name], b.status, b.errMsg); err != nil {
			t.Fatal(err)
		}
	}

	vs := GetVideoStore(db)
	pending, failed, err := vs.RecoverInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if pending != 1 || failed != 4 {
		t.Errorf("got %d pending, %d failed, want 1 and 4", pending, failed)
	}

	for name, tc := range cases {
		var (
			got       row
			videoPath sql.NullString
			errMsg    sql.NullString
		)
		if err := db.QueryRow("SELECT v.finished, v.ignored, v.video_path, v.metarr_status, d.status, d.error FROM videos v JOIN downloads d ON d.video_id = v.id WHERE v.id = ?",
			ids[name]).Scan(&got.finished, &got.ignored, &videoPath, &got.metarrStatus, &got.status, &errMsg); err != nil {
			t.Fatal(err)
		}
		if videoPath.Valid {
			got.videoPath = videoPath.String
		}
		got.errMsg = errMsg.String
		if got != tc.want {
			t.Errorf("%s: got %+v, want %+v", name, got, tc.want)
		}
	}

	// A second startup finds nothing left to repair.
	if pending, failed, err = vs.RecoverInterrupted(); err != nil || pending != 0 || failed != 0 {
		t.Errorf("second run: got %d pending, %d failed, %v, want nothing", pending, failed, err)
	}

	// Recovered videos are the ones Run Metarr picks up.
	videos, err := vs.GetVideosAwaitingMetarr(chanID, false)
	if err != nil {
		t.Fatal(err)
	}
	var awaiting []int64
	for _, v := range videos {
		awaiting = append(awaiting, v.ID)
	}
	if len(awaiting) != 2 || !slices.Contains(awaiting, ids["mid-Metarr"]) || !slices.Contains(awaiting, ids["finished, Metarr failed"]) {
		t.Errorf("awaiting Metarr: got %v, want the mid-Metarr and failed Metarr videos", awaiting)
	}
}
