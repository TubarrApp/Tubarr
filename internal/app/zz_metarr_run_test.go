package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
)

// TestMetarrBusy tests that only the channel's given videos queued for or running in Metarr are counted.
func TestMetarrBusy(t *testing.T) {
	inMetarr := func(videoID int64) *metarrClaim {
		claim := &metarrClaim{videoID: videoID, done: make(chan struct{})}
		claim.inMetarr.Store(true)
		return claim
	}
	claims := map[metarrKey]*metarrClaim{
		{1, "https://a.example/1"}: inMetarr(10),
		{1, "https://a.example/2"}: inMetarr(11),
		{1, "https://a.example/3"}: {videoID: 12, pipeline: true, done: make(chan struct{})}, // Still downloading.
		{2, "https://b.example/1"}: inMetarr(20),
	}
	for k, c := range claims {
		metarrClaims.Store(k, c)
	}
	t.Cleanup(func() {
		for k := range claims {
			metarrClaims.Delete(k)
		}
	})

	tests := []struct {
		name     string
		channel  int64
		videoIDs []int64
		want     int
	}{
		{"one running", 1, []int64{10}, 1},
		{"two running", 1, []int64{10, 11}, 2},
		{"not in Metarr yet", 1, []int64{12}, 0},
		{"other channel's video", 1, []int64{20}, 0},
		{"not held", 1, []int64{99}, 0},
		{"no videos given", 1, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MetarrBusy(tt.channel, tt.videoIDs); got != tt.want {
				t.Errorf("MetarrBusy(%d, %v) = %d, want %d", tt.channel, tt.videoIDs, got, tt.want)
			}
		})
	}
}

// TestMetarrSkip tests each reason Metarr is held back for a channel URL, and that it isn't otherwise.
func TestMetarrSkip(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	c := &models.Channel{ID: 1, Name: "Cats"}

	for name, tc := range map[string]struct {
		metarrErr error
		args      models.MetarrArgs
		want      models.SkipMetarrReasons
	}{
		"runs":                       {},
		"no metarr":                  {metarrErr: errors.New("not found"), want: models.SkipMetarrNoPath},
		"meta ops file":              {args: models.MetarrArgs{MetaOpsFile: missing}, want: models.SkipMetarrMissingMetaOpsFile},
		"filename ops file":          {args: models.MetarrArgs{FilenameOpsFile: missing}, want: models.SkipMetarrMissingFilenameOpsFile},
		"filtered meta ops file":     {args: models.MetarrArgs{FilteredMetaOpsFile: missing}, want: models.SkipMetarrMissingFilteredMetaOpsFile},
		"filtered filename ops file": {args: models.MetarrArgs{FilteredFilenameOpsFile: missing}, want: models.SkipMetarrMissingFilteredFilenameOpsFile},
	} {
		cu := &models.ChannelURL{URL: "https://example.com/@cats", ChanURLMetarrArgs: &tc.args}
		skip, err := metarrSkip(tc.metarrErr, cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
		if skip.Skip != (tc.want != "") || skip.Reason != tc.want || (err != nil) != skip.Skip {
			t.Errorf("%s: metarrSkip = %+v, %v; want reason %q", name, skip, err, tc.want)
		}
	}
}

// TestRunMetarrLeavesPendingForMissingOpsFile tests Run Metarr doesn't run Metarr without a channel URL's ops file,
// leaving the video pending to try again once it's fixed.
func TestRunMetarrLeavesPendingForMissingOpsFile(t *testing.T) {
	// A metarr that answers --help, and records any real run.
	bin, dir := t.TempDir(), t.TempDir()
	ran := filepath.Join(dir, "metarr-ran")
	script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = --help ] && exit 0\ntouch %q\n", ran)
	if err := os.WriteFile(filepath.Join(bin, "metarr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := repo.InitStores(db)
	if err != nil {
		t.Fatal(err)
	}

	opsFile := filepath.Join(dir, "meta-ops.txt")
	if err := os.WriteFile(opsFile, []byte("title:set:Cats\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := &models.Channel{
		Name:           "Cats",
		ChanSettings:   &models.Settings{VideoDir: dir},
		ChanMetarrArgs: &models.MetarrArgs{MetaOpsFile: opsFile},
		URLModels:      []*models.ChannelURL{{URL: "https://example.com/@cats"}},
	}
	id, err := store.ChannelStore().AddChannel(add)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := store.ChannelStore().GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
	if err != nil {
		t.Fatal(err)
	}

	video, metaFile := filepath.Join(dir, "v.mp4"), filepath.Join(dir, "v.json")
	for _, f := range []string{video, metaFile} {
		if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, video_path, json_path, metarr_status)
		VALUES (?, ?, ?, 1, 0, 'https://example.com/watch/1', ?, ?, ?)`,
		c.ID, c.URLModels[0].ID, c.URLModels[0].URL, video, metaFile, consts.MetarrStatusPending); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(opsFile); err != nil {
		t.Fatal(err)
	}
	if err := RunMetarr(context.Background(), store, c, nil, false); err == nil {
		t.Error("RunMetarr with a missing meta ops file succeeded, want an error")
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("Metarr ran without the channel URL's meta ops file")
	}
	waiting, err := store.VideoStore().GetVideosAwaitingMetarr(c.ID, false)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("videos awaiting Metarr = %d, %v; want the video still pending", len(waiting), err)
	}

	// A Download As video doesn't use the ops file, so Metarr runs for it.
	if err := store.VideoStore().SetVideoOverrides([]int64{waiting[0].ID}, &models.VideoOverrides{}); err != nil {
		t.Fatal(err)
	}
	_ = RunMetarr(context.Background(), store, c, nil, false) // The stand-in Metarr gives no final paths, so this fails after running.
	if _, err := os.Stat(ran); err != nil {
		t.Error("Metarr didn't run for a Download As video, which doesn't use the ops file")
	}
}

// TestWaitMetarr tests shutdown waits for videos in Metarr to be released, up to the timeout, and not for ones still downloading.
func TestWaitMetarr(t *testing.T) {
	hold := func(t *testing.T, url string, inMetarr bool) (metarrKey, *metarrClaim) {
		t.Helper()
		key := metarrKey{channelID: 1, url: url}
		claim := &metarrClaim{done: make(chan struct{})}
		claim.inMetarr.Store(inMetarr)
		metarrClaims.Store(key, claim)
		t.Cleanup(func() { metarrClaims.Delete(key) })
		return key, claim
	}

	t.Run("waits for release", func(t *testing.T) {
		key, claim := hold(t, "https://example.com/v/1", true)
		go func() {
			time.Sleep(50 * time.Millisecond)
			releaseMetarr(key, claim)
		}()
		start := time.Now()
		WaitMetarr(5 * time.Second)
		select {
		case <-claim.done:
		default:
			t.Fatal("returned before the run was released")
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("waited %v after the run was released at 50ms", d)
		}
	})

	t.Run("gives up at timeout", func(t *testing.T) {
		hold(t, "https://example.com/v/2", true)
		start := time.Now()
		WaitMetarr(100 * time.Millisecond)
		if d := time.Since(start); d < 100*time.Millisecond || d > 2*time.Second {
			t.Fatalf("waited %v, want the 100ms timeout", d)
		}
	})

	t.Run("skips downloads", func(t *testing.T) {
		hold(t, "https://example.com/v/3", false)
		start := time.Now()
		WaitMetarr(5 * time.Second)
		if d := time.Since(start); d > time.Second {
			t.Fatalf("waited %v on a video not yet in Metarr", d)
		}
	})
}
