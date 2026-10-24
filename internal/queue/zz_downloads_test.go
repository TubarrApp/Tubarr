package queue

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/paths"
	"tubarr/internal/downloads"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
)

// newTestChannel returns a store on a new database, and a channel in it downloading one video at a time.
func newTestChannel(t *testing.T) (contracts.Store, *sql.DB, *models.Channel) {
	t.Helper()
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

	add := &models.Channel{
		Name:           "Cats",
		ChanSettings:   &models.Settings{VideoDir: t.TempDir(), DownloadConcurrency: 1},
		ChanMetarrArgs: &models.MetarrArgs{},
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
	return store, db, c
}

// storedStatus returns a video's stored download status and error.
func storedStatus(t *testing.T, db *sql.DB, videoID int64) (consts.DownloadStatus, string) {
	t.Helper()
	var status consts.DownloadStatus
	var errMsg sql.NullString
	query := "SELECT " + consts.QDLStatus + ", " + consts.QDLError + " FROM " + consts.DBDownloads + " WHERE " + consts.QDLVidID + " = ?"
	if err := db.QueryRow(query, videoID).Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	return status, errMsg.String
}

// TestAddThenStop tests videos waiting for their channel's lane are stopped: one the user chose returns as canceled, a
// crawled one as failed, and a video already queued isn't queued again.
func TestAddThenStop(t *testing.T) {
	store, db, c := newTestChannel(t)
	cu := c.URLModels[0]
	pp := NewPostProcessingQueue(store)
	dq := NewDownloadQueue(t.Context(), store, pp)

	// The channel's one slot is taken, so the videos wait.
	lane := dq.lanes.lane(c, cu, 1, 1)
	if err := lane.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer lane.Release()

	chosen := &models.Video{URL: "https://example.com/v/chosen", ChannelURLID: cu.ID}
	crawled := &models.Video{URL: "https://example.com/v/crawled", ChannelURLID: cu.ID}
	queued, err := dq.Add(c, Request{Video: chosen, ByUser: true}, Request{Video: crawled})
	if err != nil || len(queued) != 2 {
		t.Fatalf("Add = %d queued, %v; want both", len(queued), err)
	}
	if again, err := dq.Add(c, Request{Video: &models.Video{URL: chosen.URL, ChannelURLID: cu.ID}, ByUser: true}); err != nil || len(again) != 0 {
		t.Fatalf("Add of a queued video = %d queued, %v; want none", len(again), err)
	}
	if entries := dq.Entries(); len(entries) != 2 || entries[0].DownloadStatus.Stage != "Waiting to start" {
		t.Fatalf("entries = %+v, want both waiting to start", entries)
	}

	if !dq.Stop(c.ID) {
		t.Fatal("Stop found no videos for the channel")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("stopped videos still in the queue")
	}
	if status, _ := storedStatus(t, db, chosen.ID); status != consts.DLStatusCanceled {
		t.Errorf("chosen video stored as %q, want Canceled", status)
	}
	if status, errMsg := storedStatus(t, db, crawled.ID); status != consts.DLStatusFailed || errMsg != errStopped.Error() {
		t.Errorf("crawled video stored as %q (%q), want Failed as stopped", status, errMsg)
	}
	if pp.Len() != 0 {
		t.Errorf("%d video(s) still held for post-processing", pp.Len())
	}
}

// TestCancel tests a video canceled while it waits returns as canceled, and can't be canceled again.
func TestCancel(t *testing.T) {
	store, db, c := newTestChannel(t)
	cu := c.URLModels[0]
	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))
	lane := dq.lanes.lane(c, cu, 1, 1)
	if err := lane.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer lane.Release()

	v := &models.Video{URL: "https://example.com/v/1", ChannelURLID: cu.ID}
	if queued, err := dq.Add(c, Request{Video: v}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want 1", len(queued), err)
	}
	if !dq.Cancel(v.ID) {
		t.Fatal("Cancel didn't find the waiting video")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("canceled video still in the queue")
	}
	if status, _ := storedStatus(t, db, v.ID); status != consts.DLStatusCanceled {
		t.Errorf("canceled video stored as %q, want Canceled", status)
	}
	if dq.Cancel(v.ID) {
		t.Error("Cancel found a video no longer in the queue")
	}
}

// TestFinalStatus tests the status each way a video can leave the queue gives it.
func TestFinalStatus(t *testing.T) {
	errFailed := errors.New("yt-dlp failed")
	tests := []struct {
		name        string
		status      consts.DownloadStatus // Status the download left the video in.
		byUser      bool
		downloading bool
		cancel      error // Cause the video's context was canceled with, if any.
		stop        bool
		shutdown    bool
		err         error
		want        consts.DownloadStatus
		wantErr     error
	}{
		{name: "downloaded", status: consts.DLStatusCompleted, want: consts.DLStatusCompleted},
		{name: "filtered out", status: consts.DLStatusIgnored, want: consts.DLStatusIgnored},
		{name: "canceled by user", status: consts.DLStatusDownloading, downloading: true, cancel: downloads.ErrCanceledByUser, err: errFailed, want: consts.DLStatusCanceled},
		{name: "chosen, stopped while waiting", status: consts.DLStatusQueued, byUser: true, stop: true, err: context.Canceled, want: consts.DLStatusCanceled},
		{name: "crawled, stopped while waiting", status: consts.DLStatusQueued, stop: true, err: context.Canceled, want: consts.DLStatusFailed, wantErr: errStopped},
		{name: "crawled, aborted while waiting", status: consts.DLStatusQueued, cancel: errAborted, err: context.Canceled, want: consts.DLStatusFailed, wantErr: errStopped},
		{name: "aborted while downloading", status: consts.DLStatusCanceled, downloading: true, cancel: errAborted, err: errFailed, want: consts.DLStatusCanceled},
		{name: "failed", status: consts.DLStatusFailed, downloading: true, err: errFailed, want: consts.DLStatusFailed, wantErr: errFailed},
		{name: "shutdown", status: consts.DLStatusQueued, byUser: true, shutdown: true, err: errFailed, want: consts.DLStatusFailed, wantErr: errFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qCtx, shutdown := context.WithCancel(t.Context())
			defer shutdown()
			q := NewDownloadQueue(qCtx, nil, nil)
			ctx, cancel := context.WithCancelCause(qCtx)
			startCtx, stop := context.WithCancelCause(ctx)
			defer cancel(nil)
			defer stop(nil)
			d := &download{
				video:       &models.Video{DownloadStatus: models.DLStatus{Status: tt.status}},
				byUser:      tt.byUser,
				downloading: tt.downloading,
				ctx:         ctx,
				startCtx:    startCtx,
			}
			if tt.cancel != nil {
				cancel(tt.cancel)
			}
			if tt.stop {
				stop(errStopped)
			}
			if tt.shutdown {
				shutdown()
			}

			status, err := q.finalStatus(d, tt.err)
			if status != tt.want || !errors.Is(err, tt.wantErr) {
				t.Errorf("finalStatus = %q, %v; want %q, %v", status, err, tt.want, tt.wantErr)
			}
		})
	}
}

// TestStopLetsDownloadsFinish tests Stop only cancels the channel's videos not yet downloading, and Abort all of them.
func TestStopLetsDownloadsFinish(t *testing.T) {
	q := NewDownloadQueue(t.Context(), nil, nil)
	add := func(id, channelID int64, downloading bool) *download {
		ctx, cancel := context.WithCancelCause(t.Context())
		startCtx, stop := context.WithCancelCause(ctx)
		d := &download{video: &models.Video{ID: id}, c: &models.Channel{ID: channelID}, ctx: ctx, cancel: cancel, startCtx: startCtx, stop: stop, downloading: downloading}
		q.downloads[id] = d
		return d
	}
	waiting := add(1, 1, false)
	downloading := add(2, 1, true)
	otherChannel := add(3, 2, false)

	if !q.Stop(1) || waiting.startCtx.Err() == nil || downloading.startCtx.Err() != nil || otherChannel.startCtx.Err() != nil {
		t.Fatal("Stop should cancel only the channel's waiting video")
	}
	if active, stopping := q.Channels(); len(active) != 1 || active[0] != 2 || len(stopping) != 1 || stopping[0] != 1 {
		t.Errorf("Channels = %v active, %v stopping; want channel 2 active and 1 stopping", active, stopping)
	}
	if !q.Abort(1) || downloading.ctx.Err() == nil || otherChannel.ctx.Err() != nil {
		t.Fatal("Abort should cancel the channel's downloading video too")
	}
	if q.Stop(3) {
		t.Error("Stop found videos for a channel with none")
	}
}

// TestSendUpdate tests a video's status is stored when it changes but not its progress, and a new attempt clears the
// last one's error.
func TestSendUpdate(t *testing.T) {
	store, db, c := newTestChannel(t)
	v := &models.Video{URL: "https://example.com/v/1", ChannelURLID: c.URLModels[0].ID}
	if _, err := store.VideoStore().QueueVideo(c.ID, v, true); err != nil {
		t.Fatal(err)
	}
	d := &download{q: NewDownloadQueue(t.Context(), store, nil), video: v}

	d.SetStatus(v, consts.DLStatusDownloading, 10, "", nil)
	d.SetStatus(v, consts.DLStatusDownloading, 50, "", nil)
	var pct float64
	if err := db.QueryRow("SELECT "+consts.QDLPct+" FROM "+consts.DBDownloads+" WHERE "+consts.QDLVidID+" = ?", v.ID).Scan(&pct); err != nil {
		t.Fatal(err)
	}
	if pct != 10 {
		t.Errorf("stored percentage %.0f, want 10 (progress isn't stored)", pct)
	}

	d.SetStatus(v, consts.DLStatusFailed, 50, "", errors.New("attempt 1 failed"))
	if status, errMsg := storedStatus(t, db, v.ID); status != consts.DLStatusFailed || errMsg != "attempt 1 failed" {
		t.Errorf("stored %q (%q), want the failed attempt", status, errMsg)
	}
	d.SetStatus(v, consts.DLStatusQueued, 0, "Retrying in 5s (attempt 2/3)", errors.New("attempt 1 failed"))
	if d.status.Error != nil {
		t.Errorf("retry kept the last attempt's error: %v", d.status.Error)
	}
}

// TestOverrideDir tests a Download As directory is filled in, a blank one keeps the channel URL's, and one whose
// fixed part doesn't exist (e.g. an unmounted drive) is refused.
func TestOverrideDir(t *testing.T) {
	dir := t.TempDir()
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@cats"}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	if got, err := overrideDir(dp, "", "/channel", "test"); err != nil || got != "/channel" {
		t.Errorf("blank: got %q, %v, want /channel", got, err)
	}
	if got, err := overrideDir(dp, filepath.Join(dir, "{{channel_name}}"), "/channel", "test"); err != nil || got != filepath.Join(dir, "Cats") {
		t.Errorf("templated: got %q, %v, want %q", got, err, filepath.Join(dir, "Cats"))
	}
	if _, err := overrideDir(dp, filepath.Join(dir, "unmounted", "videos"), "/channel", "test"); err == nil {
		t.Error("missing destination: got no error")
	}
}
