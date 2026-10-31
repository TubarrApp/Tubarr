package repo

import (
	"errors"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"tubarr/internal/app"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

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
			{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: "https://example.com/v/1", Title: "One", Duration: 60},
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
	if one == nil || one.Title != "One" || one.Duration != 60 || one.ChannelName != "Test" || one.DownloadStatus.Status != consts.DLStatusAvailable {
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
		if v.URL == "https://example.com/v/1" && v.Title != "One" {
			t.Errorf("queued video 1's title = %q, want \"One\"", v.Title)
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

	v := &models.Video{ChannelURLID: cuID, ChannelURL: "https://example.com/@a", URL: "https://example.com/v/1", Title: "One"}
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
