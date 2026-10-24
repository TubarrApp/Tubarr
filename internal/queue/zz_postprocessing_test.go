package queue

import (
	"context"
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
	"tubarr/internal/repo"
)

// queueVideo puts a video in the queue as a Metarr run would, returning a function reporting whether it was canceled.
func queueVideo(t *testing.T, q *PostProcessingQueue, channelID, videoID int64, started bool) (canceled func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := &Entry{VideoID: videoID, ChannelID: channelID, QueuedAt: time.Now()}
	if started {
		now := time.Now()
		e.StartedAt = &now
	}
	if _, added := q.add(videoKey{channelID, fmt.Sprintf("https://example.com/v/%d", videoID)}, e, cancel); !added {
		t.Fatalf("video %d is already in the queue", videoID)
	}
	return func() bool { return ctx.Err() != nil }
}

// TestBusy tests that only the channel's given videos in the queue are counted, not ones still downloading.
func TestBusy(t *testing.T) {
	q := NewPostProcessingQueue(nil)
	queueVideo(t, q, 1, 10, true)
	queueVideo(t, q, 1, 11, false)
	queueVideo(t, q, 2, 20, true)
	if _, held, err := q.hold(context.Background(), &models.Channel{ID: 1}, "https://example.com/v/12"); err != nil || !held {
		t.Fatalf("Hold = %t, %v; want the video held", held, err)
	}

	tests := []struct {
		name     string
		channel  int64
		videoIDs []int64
		want     int
	}{
		{"one running", 1, []int64{10}, 1},
		{"running and waiting", 1, []int64{10, 11}, 2},
		{"still downloading", 1, []int64{12}, 0},
		{"other channel's video", 1, []int64{20}, 0},
		{"not in the queue", 1, []int64{99}, 0},
		{"no videos given", 1, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := q.Busy(tt.channel, tt.videoIDs); got != tt.want {
				t.Errorf("Busy(%d, %v) = %d, want %d", tt.channel, tt.videoIDs, got, tt.want)
			}
		})
	}
}

// TestStopAbortCancel tests Stop cancels only the channel's waiting videos, Abort its running ones too, and Cancel the
// given videos.
func TestStopAbortCancel(t *testing.T) {
	q := NewPostProcessingQueue(nil)
	running := queueVideo(t, q, 1, 10, true)
	waiting := queueVideo(t, q, 1, 11, false)
	otherChannel := queueVideo(t, q, 2, 20, false)

	if n := q.Stop(1); n != 1 || !waiting() || running() || otherChannel() {
		t.Fatalf("Stop canceled %d (waiting %t, running %t, other channel %t), want only the waiting video", n, waiting(), running(), otherChannel())
	}
	if n := q.Abort(1); n != 2 || !running() || otherChannel() {
		t.Fatalf("Abort canceled %d (running %t, other channel %t), want both of the channel's videos", n, running(), otherChannel())
	}
	if n := q.Cancel(2, []int64{99}); n != 0 || otherChannel() {
		t.Fatalf("Cancel of a video not in the queue canceled %d", n)
	}
	if n := q.Cancel(2, nil); n != 1 || !otherChannel() {
		t.Fatalf("Cancel with no videos given canceled %d, want the channel's one video", n)
	}
}

// TestHold tests a download's hold keeps other downloads off the video, and cancels and waits out a Metarr run on it.
func TestHold(t *testing.T) {
	q := NewPostProcessingQueue(nil)
	c := &models.Channel{ID: 1}
	videoURL := "https://example.com/v/1"

	// A Metarr run on the video ends once canceled, after which the download takes it.
	key := videoKey{c.ID, videoURL}
	runCtx, cancelRun := context.WithCancel(context.Background())
	run, _ := q.add(key, &Entry{VideoID: 1}, cancelRun)
	go func() {
		<-runCtx.Done()
		q.release(key, run)
	}()
	hold, held, err := q.hold(context.Background(), c, videoURL)
	if err != nil || !held || runCtx.Err() == nil {
		t.Fatalf("Hold = %t, %v (Metarr run canceled %t), want the run canceled and the video held", held, err, runCtx.Err() != nil)
	}

	if _, held, err := q.hold(context.Background(), c, videoURL); err != nil || held {
		t.Fatalf("second download's Hold = %t, %v; want it refused", held, err)
	}
	hold.release()
	if _, held, err := q.hold(context.Background(), c, videoURL); err != nil || !held {
		t.Fatalf("Hold after release = %t, %v; want the video held", held, err)
	}
}

// TestStopKeepsHeldVideoPending tests a video its download held before the channel was stopped isn't post-processed.
func TestStopKeepsHeldVideoPending(t *testing.T) {
	q := NewPostProcessingQueue(nil)
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{ID: 1, URL: "https://example.com/@cats"}
	hold, held, err := q.hold(context.Background(), c, "https://example.com/v/1")
	if err != nil || !held {
		t.Fatalf("Hold = %t, %v; want the video held", held, err)
	}

	q.Stop(c.ID)
	v := &models.Video{ID: 1, URL: "https://example.com/v/1"}
	if status := hold.postProcess(context.Background(), v, cu, c, nil); status != consts.MetarrStatusPending {
		t.Errorf("PostProcess after the channel was stopped = %q, want pending", status)
	}
	if entries := q.Entries(); len(entries) != 0 {
		t.Errorf("stopped video was put in the queue: %+v", entries)
	}
	hold.release()
}

// TestStopWhileWaitingForLane tests a held video waits in the queue while its channel's lane is full, and stopping the
// channel leaves it pending.
func TestStopWhileWaitingForLane(t *testing.T) {
	q := NewPostProcessingQueue(nil)
	c := &models.Channel{ID: 1, Name: "Cats", ChanMetarrArgs: &models.MetarrArgs{MetarrConcurrency: 1}}
	cu := &models.ChannelURL{ID: 1, URL: "https://example.com/@cats", ChanURLMetarrArgs: &models.MetarrArgs{MetarrConcurrency: 1}}
	lane := q.lanes.lane(c, cu, 1, 1)
	if err := lane.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer lane.Release()

	hold, held, err := q.hold(context.Background(), c, "https://example.com/v/1")
	if err != nil || !held {
		t.Fatalf("Hold = %t, %v; want the video held", held, err)
	}
	defer hold.release()
	status := make(chan consts.MetarrStatus, 1)
	go func() {
		status <- hold.postProcess(context.Background(), &models.Video{ID: 1, URL: "https://example.com/v/1"}, cu, c, nil)
	}()

	waitFor(t, func() bool { return len(q.Entries()) == 1 })
	if e := q.Entries()[0]; e.VideoID != 1 || e.ChannelName != "Cats" || e.StartedAt != nil {
		t.Fatalf("entry = %+v, want video 1 waiting", e)
	}
	if n := q.Stop(c.ID); n != 1 {
		t.Errorf("Stop canceled %d, want the waiting video", n)
	}
	select {
	case s := <-status:
		if s != consts.MetarrStatusPending {
			t.Errorf("PostProcess = %q, want pending", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stopped video still waiting for its lane")
	}
}

// TestWait tests Wait returns once every held or queued video is released, and gives up when its context ends.
func TestWait(t *testing.T) {
	q := NewPostProcessingQueue(nil)
	c := &models.Channel{ID: 1}
	hold, _, _ := q.hold(context.Background(), c, "https://example.com/v/1")
	go func() {
		time.Sleep(50 * time.Millisecond)
		hold.release()
	}()
	start := time.Now()
	if !q.Wait(context.Background()) || q.Len() != 0 {
		t.Fatal("Wait returned with a video still held")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("waited %v after the video was released at 50ms", d)
	}

	if _, held, _ := q.hold(context.Background(), c, "https://example.com/v/2"); !held {
		t.Fatal("video not held")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if q.Wait(ctx) {
		t.Fatal("Wait reported the queue empty with a video still held")
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
	q := NewPostProcessingQueue(store)

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
	if err := q.RunMetarr(context.Background(), c, nil, false); err == nil {
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
	_ = q.RunMetarr(context.Background(), c, nil, false) // The stand-in Metarr gives no final paths, so this fails after running.
	if _, err := os.Stat(ran); err != nil {
		t.Error("Metarr didn't run for a Download As video, which doesn't use the ops file")
	}
	if q.Len() != 0 {
		t.Errorf("%d video(s) left in the queue after the run", q.Len())
	}
}
