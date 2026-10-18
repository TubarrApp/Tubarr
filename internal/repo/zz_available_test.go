package repo

import (
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

	// Claiming twice only succeeds once.
	ids := []int64{available[0].ID, available[1].ID}
	claimed, err := vs.ClaimVideos(chanID, ids, app.AvailableStatuses, consts.DLStatusQueued)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("first claim: got %d, %v, want 2", len(claimed), err)
	}
	if again, err := vs.ClaimVideos(chanID, ids, app.AvailableStatuses, consts.DLStatusQueued); err != nil || len(again) != 0 {
		t.Fatalf("second claim: got %d, %v, want 0", len(again), err)
	}

	// Claimed videos no longer count as seen, and a crawl re-finding them leaves them alone.
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
	if seen, failed, _ := cs.GetDownloadedOrIgnoredVideoURLs(c); len(seen) != 1 || !slices.Equal(failed, []string{"https://example.com/v/2"}) {
		t.Errorf("expected the dismissed video to be seen and the other failed, got %v and %v", seen, failed)
	}

	// Videos can't be claimed through another channel.
	if claimed, err := vs.ClaimVideos(chanID+1, ids[1:], app.AvailableStatuses, consts.DLStatusQueued); err != nil || len(claimed) != 0 {
		t.Errorf("other channel claim: got %d, %v, want 0", len(claimed), err)
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
	awaiting, err := vs.GetVideosAwaitingMetarr(chanID)
	if err != nil || len(awaiting) != 1 {
		t.Fatalf("got %d videos awaiting Metarr, %v, want 1", len(awaiting), err)
	}
	if o := awaiting[0].Overrides; o == nil || o.Filename != "Chosen" || len(o.MetaOps) != 1 || o.MetaOps[0].OpValue != "Cats" {
		t.Errorf("video awaiting Metarr: got overrides %+v", o)
	}

	if err := vs.SetVideoOverrides([]int64{v.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if o, err := vs.GetVideoOverrides(v.ID); err != nil || o != nil {
		t.Errorf("cleared: got overrides %+v, %v, want none", o, err)
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
