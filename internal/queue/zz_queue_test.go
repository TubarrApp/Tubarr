package queue

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/downloads"
	"tubarr/internal/limits"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// ******************************** Queue *********************************************************************************************

// TestMain sets up the test environment for the queue package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// TestManualChannelURL tests a channel URL is found by its ID, and an ID the channel doesn't have is refused.
func TestManualChannelURL(t *testing.T) {
	c := &models.Channel{Name: "Cats", URLModels: []*models.ChannelURL{{ID: 4, URL: "https://example.com/@cats"}}}
	if cu, err := ManualChannelURL(nil, c, 4); err != nil || cu != c.URLModels[0] {
		t.Errorf("ManualChannelURL(4) = %v, %v; want the channel's URL", cu, err)
	}
	if _, err := ManualChannelURL(nil, c, 5); err == nil {
		t.Error("ManualChannelURL(5) found a URL the channel doesn't have")
	}
}

// TestWithYtdlpOverrides tests a video's Download As yt-dlp options replace its channel URL's, blank ones keeping the
// channel URL's, without changing the channel URL itself.
func TestWithYtdlpOverrides(t *testing.T) {
	cu := &models.ChannelURL{URL: "https://example.com/@cats", ChanURLSettings: &models.Settings{
		YtdlpOutputExt:            "mp4",
		MaxFilesize:               "1g",
		MaxResolution:             "1080",
		YTDLPPreferredVideoCodecs: []string{"h264"},
		YTDLPPreferredAudioCodecs: []string{"aac"},
	}}
	got := withYtdlpOverrides(cu, &models.VideoOverrides{YtdlpOutputExt: "mkv", MaxResolution: "720", YTDLPPreferredVideoCodecs: []string{"av1", "vp9"}})

	s := got.ChanURLSettings
	if s.YtdlpOutputExt != "mkv" || s.MaxFilesize != "1g" || s.MaxResolution != "720" || len(s.YTDLPPreferredVideoCodecs) != 2 || s.YTDLPPreferredAudioCodecs[0] != "aac" {
		t.Errorf("got settings %+v", s)
	}
	if got.URL != cu.URL {
		t.Errorf("got URL %q, want the channel URL's", got.URL)
	}
	if cu.ChanURLSettings.YtdlpOutputExt != "mp4" || len(cu.ChanURLSettings.YTDLPPreferredVideoCodecs) != 1 {
		t.Errorf("the channel URL's own settings changed: %+v", cu.ChanURLSettings)
	}
}

// waitFor waits until done reports true.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(time.Millisecond)
	}
}

// ******************************** Download attempts *********************************************************************************

// TestDownloadAttempts tests that downloads run once plus their Download Retries setting, else the default.
func TestDownloadAttempts(t *testing.T) {
	tests := []struct {
		name string
		cu   *models.ChannelURL
		want int
	}{
		{"no settings", &models.ChannelURL{}, 1 + consts.DefaultDownloadRetries},
		{"unset", &models.ChannelURL{ChanURLSettings: &models.Settings{}}, 1 + consts.DefaultDownloadRetries},
		{"negative", &models.ChannelURL{ChanURLSettings: &models.Settings{Retries: -1}}, 1 + consts.DefaultDownloadRetries},
		{"set", &models.ChannelURL{ChanURLSettings: &models.Settings{Retries: 5}}, 6},
	}
	for _, tt := range tests {
		if got := downloadAttempts(tt.cu); got != tt.want {
			t.Errorf("%s: got %d attempts, want %d", tt.name, got, tt.want)
		}
	}
}

// ******************************** Download slots ************************************************************************************

// firstStage returns the stage the Downloads page shows for the queue's first video.
func firstStage(dq *DownloadQueue) string {
	entries := dq.Entries()
	if len(entries) == 0 {
		return ""
	}
	return entries[0].DownloadStatus.Stage
}

// TestGlobalDownloadLimitHoldsMetadataFetch tests a video's metadata isn't fetched until it has a global download slot.
func TestGlobalDownloadLimitHoldsMetadataFetch(t *testing.T) {
	store, _, c := newTestChannel(t)
	cu := c.URLModels[0]
	cu.ChanURLSettings.Retries = 1

	// A yt-dlp that leaves a file showing it ran, then fails.
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	script := "#!/bin/sh\ntouch '" + ran + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, sharedconsts.ProgramYTDLP), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	abstractions.Set(keys.SkipAllWaits, true)
	t.Cleanup(func() { abstractions.Set(keys.SkipAllWaits, nil) })

	// The one global download slot is taken.
	limits.GlobalDownloads.SetLimit(1)
	t.Cleanup(func() { limits.GlobalDownloads.SetLimit(0) })
	if err := limits.GlobalDownloads.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(limits.GlobalDownloads.Release) }
	t.Cleanup(release)

	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))
	v := &models.Video{URL: "https://example.com/v/1", ChannelURLID: cu.ID}
	if queued, err := dq.Add(c, Request{Video: v, ByUser: true}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want it queued", len(queued), err)
	}

	// It waits for the slot without fetching its metadata.
	deadline := time.Now().Add(10 * time.Second)
	for firstStage(dq) != "Waiting for download slot" {
		if time.Now().After(deadline) {
			t.Fatalf("stage = %q, want it waiting for a download slot", firstStage(dq))
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("metadata was fetched without a global download slot")
	}

	// Once the slot is free, its metadata is fetched.
	release()
	deadline = time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ran); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("metadata wasn't fetched once the slot was free (stage %q)", firstStage(dq))
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel its retry rather than wait for it.
	dq.Cancel(v.ID)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("canceled video still in the queue")
	}
}

// ******************************** Downloads *****************************************************************************************

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

// TestAddRefusesNil tests Add returns an error rather than panicking for a nil channel or a request without a video.
func TestAddRefusesNil(t *testing.T) {
	store, _, c := newTestChannel(t)
	q := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))
	if _, err := q.Add(nil, Request{Video: &models.Video{URL: "https://example.com/v/1"}}); err == nil {
		t.Error("Add with a nil channel succeeded")
	}
	if queued, err := q.Add(c, Request{}); err == nil || len(queued) != 0 {
		t.Errorf("Add with a request without a video = %d queued, %v; want an error", len(queued), err)
	}
}

// TestAddThenStop tests videos waiting for their channel's lane are stopped: one the user chose returns as canceled, a
// crawled one as failed, and a video already queued isn't queued again.
func TestAddThenStop(t *testing.T) {
	store, db, c := newTestChannel(t)
	cu := c.URLModels[0]
	pp := NewPostProcessingQueue(store)
	dq := NewDownloadQueue(t.Context(), store, pp)

	// The channel's one slot is taken, so the videos wait.
	dq.SetLimits(c)
	takeSlot(t, &dq.lanes, c.ID, cu.ID)

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
	if !pp.Wait(ctx) {
		t.Errorf("%d video(s) still held for post-processing", pp.Len())
	}
}

// TestCancel tests a video canceled while it waits returns as canceled, and can't be canceled again.
func TestCancel(t *testing.T) {
	store, db, c := newTestChannel(t)
	cu := c.URLModels[0]
	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))
	dq.SetLimits(c)
	takeSlot(t, &dq.lanes, c.ID, cu.ID)

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
			preDownloadCtx, stop := context.WithCancelCause(ctx)
			defer cancel(nil)
			defer stop(nil)
			d := &download{
				video:          &models.Video{DownloadStatus: models.DLStatus{Status: tt.status}},
				byUser:         tt.byUser,
				downloading:    tt.downloading,
				ctx:            ctx,
				preDownloadCtx: preDownloadCtx,
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
		preDownloadCtx, stop := context.WithCancelCause(ctx)
		d := &download{video: &models.Video{ID: id}, c: &models.Channel{ID: channelID}, ctx: ctx, cancel: cancel, preDownloadCtx: preDownloadCtx, stop: stop, downloading: downloading}
		q.downloads[id] = d
		return d
	}
	waiting := add(1, 1, false)
	downloading := add(2, 1, true)
	otherChannel := add(3, 2, false)

	if !q.Stop(1) || waiting.preDownloadCtx.Err() == nil || downloading.preDownloadCtx.Err() != nil || otherChannel.preDownloadCtx.Err() != nil {
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
	d := &download{q: NewDownloadQueue(t.Context(), store, nil), video: v, c: c}

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

// TestCheckDestinationsOutputDirectoryTags tests an output folder inside a video or output directory needn't exist yet, but that directory must.
func TestCheckDestinationsOutputDirectoryTags(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "unmounted")

	tests := []struct {
		name          string
		channelOutput string
		urlOutput     string
		moveOp        string
		wantErr       bool
	}{
		{"new folder inside the channel's output directory", dir, filepath.Join(dir, "Shorts"), "", false},
		{"new folder inside a missing output directory", missing, filepath.Join(missing, "Shorts"), "", true},
		{"move op inside the channel URL's output directory", "", dir, "{{channel_url_output_directory}}/Shorts", false},
		{"move op inside the channel's output directory", dir, "", "{{channel_output_directory}}/Shorts", false},
		{"move op inside a missing channel output directory", missing, dir, "{{channel_output_directory}}/Shorts", true},
		{"channel URL elsewhere from a missing channel output directory", missing, dir, "", false},
		{"missing folder outside every directory", "", filepath.Join(missing, "Shorts"), "", true},
	}
	for _, tt := range tests {
		c := &models.Channel{ChanSettings: &models.Settings{VideoDir: dir}, ChanMetarrArgs: &models.MetarrArgs{OutputDir: tt.channelOutput}}
		s := &models.Settings{VideoDir: dir}
		if tt.moveOp != "" {
			s.MetaFilterMoveOps = []models.MetaFilterMoveOps{{OutputDir: tt.moveOp}}
		}
		cu := &models.ChannelURL{ChanURLSettings: s, ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: tt.urlOutput}}
		if err := CheckDestinations(c, cu, nil); (err != nil) != tt.wantErr {
			t.Errorf("%s: got %v, want an error: %t", tt.name, err, tt.wantErr)
		}
	}

	// A video directory must exist, even inside another directory, as nothing creates it for Tubarr.
	inside := &models.ChannelURL{ChanURLSettings: &models.Settings{VideoDir: filepath.Join(dir, "missing")}, ChanURLMetarrArgs: &models.MetarrArgs{}}
	if err := CheckDestinations(&models.Channel{ChanSettings: &models.Settings{VideoDir: dir}, ChanMetarrArgs: &models.MetarrArgs{}}, inside, nil); err == nil {
		t.Error("missing video directory inside the channel's: got no error")
	}

	// A Download As directory using a tag is checked the same way.
	c := &models.Channel{ChanSettings: &models.Settings{VideoDir: dir}, ChanMetarrArgs: &models.MetarrArgs{OutputDir: missing}}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{VideoDir: dir}, ChanURLMetarrArgs: &models.MetarrArgs{}}
	o := &models.VideoOverrides{}
	o.OutputDir = "{{channel_output_directory}}/Once"
	if err := CheckDestinations(c, cu, o); err == nil {
		t.Error("Download As directory inside a missing channel output directory: got no error")
	}
}

// ******************************** Ignored redownload ********************************************************************************

const filteredVideoURL = "https://example.com/v/filtered-out"

// storeFilteredVideo stores a video an earlier run filtered out.
func storeFilteredVideo(t *testing.T, store contracts.Store, c *models.Channel) {
	t.Helper()
	v := &models.Video{URL: filteredVideoURL, ChannelURLID: c.URLModels[0].ID, ChannelURL: c.URLModels[0].URL, Metadata: models.VideoMetadata{Title: "Filtered out"}}
	v.MarkVideoAsIgnored()
	if _, err := store.VideoStore().AddVideos([]*models.Video{v}, c.ID); err != nil {
		t.Fatal(err)
	}
}

// checkOnAvailablePage fails the test unless the filtered out video is on the Available page.
func checkOnAvailablePage(t *testing.T, store contracts.Store, status consts.DownloadStatus) {
	t.Helper()
	available, err := store.VideoStore().GetAvailableVideos(consts.DLStatusAvailable, consts.DLStatusFailed, consts.DLStatusCanceled)
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || available[0].DownloadStatus.Status != status {
		t.Errorf("Available videos = %+v, want the video with status %q", available, status)
	}
}

// TestFilteredVideoCanceledWhileWaiting tests a filtered out video entered by its URL returns to Available if canceled
// before its metadata is saved.
func TestFilteredVideoCanceledWhileWaiting(t *testing.T) {
	store, _, c := newTestChannel(t)
	storeFilteredVideo(t, store, c)
	cu := c.URLModels[0]
	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))

	// The channel's one slot is taken, so the video waits.
	dq.SetLimits(c)
	takeSlot(t, &dq.lanes, c.ID, cu.ID)

	v := &models.Video{URL: filteredVideoURL, ChannelURLID: cu.ID}
	if queued, err := dq.Add(c, Request{Video: v, ByUser: true}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want it queued", len(queued), err)
	}
	if !dq.Cancel(v.ID) {
		t.Fatal("Cancel found no video")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("canceled video still in the queue")
	}
	checkOnAvailablePage(t, store, consts.DLStatusCanceled)
}

// TestFilteredVideoMetadataFails tests a filtered out video entered by its URL returns to Available if its metadata can't
// be fetched.
func TestFilteredVideoMetadataFails(t *testing.T) {
	store, _, c := newTestChannel(t)
	storeFilteredVideo(t, store, c)
	cu := c.URLModels[0]
	cu.ChanURLSettings.Retries = 1

	// yt-dlp fails.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sharedconsts.ProgramYTDLP), []byte("#!/bin/sh\necho 'ERROR: the site is down' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	abstractions.Set(keys.SkipAllWaits, true)
	t.Cleanup(func() { abstractions.Set(keys.SkipAllWaits, nil) })

	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))
	v := &models.Video{URL: filteredVideoURL, ChannelURLID: cu.ID}
	if queued, err := dq.Add(c, Request{Video: v, ByUser: true}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want it queued", len(queued), err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("failed video still in the queue")
	}
	checkOnAvailablePage(t, store, consts.DLStatusFailed)
}

// ******************************** Lanes *********************************************************************************************

// limitsChannel returns a loaded channel with the given download and Metarr concurrency, and URLs merged with it.
func limitsChannel(id int64, loadedAt time.Time, limit int, urlLimits map[int64]int) *models.Channel {
	c := &models.Channel{
		ID:             id,
		LoadedAt:       loadedAt,
		ChanSettings:   &models.Settings{DownloadConcurrency: limit},
		ChanMetarrArgs: &models.MetarrArgs{MetarrConcurrency: limit},
	}
	for urlID, own := range urlLimits {
		merged := cmp.Or(own, limit)
		c.URLModels = append(c.URLModels, &models.ChannelURL{
			ID:                urlID,
			ChanURLSettings:   &models.Settings{DownloadConcurrency: merged},
			ChanURLMetarrArgs: &models.MetarrArgs{MetarrConcurrency: merged},
		})
	}
	return c
}

// mustAcquire takes a slot for a video of the channel URL, failing if none is free.
func mustAcquire(t *testing.T, l *lanes, channelID, channelURLID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := l.acquire(ctx, channelID, channelURLID); err != nil {
		t.Fatalf("no slot free for channel URL %d: %v", channelURLID, err)
	}
}

// takeSlot takes a slot for a video of the channel URL, held until the test ends.
func takeSlot(t *testing.T, l *lanes, channelID, channelURLID int64) {
	t.Helper()
	mustAcquire(t, l, channelID, channelURLID)
	t.Cleanup(func() { l.release(channelID, channelURLID) })
}

// assertNoSlot fails if a video of the channel URL is given a slot.
func assertNoSlot(t *testing.T, l *lanes, channelID, channelURLID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := l.acquire(ctx, channelID, channelURLID); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			l.release(channelID, channelURLID)
		}
		t.Fatalf("channel URL %d got a slot (error %v), want its lane full", channelURLID, err)
	}
}

// waitingLen returns how many videos are waiting for a slot.
func waitingLen(l *lanes) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.waiting)
}

// startWaiter starts a video of the channel URL waiting for a slot, returning a channel told when it gets one.
func startWaiter(t *testing.T, l *lanes, channelID, channelURLID int64) <-chan error {
	t.Helper()
	before := waitingLen(l)
	got := make(chan error, 1)
	go func() { got <- l.acquire(t.Context(), channelID, channelURLID) }()
	waitFor(t, func() bool { return waitingLen(l) > before || len(got) > 0 })
	if len(got) > 0 {
		t.Fatalf("video of channel URL %d got a slot straight away, want it waiting", channelURLID)
	}
	return got
}

// gotSlot fails unless the waiting video gets a slot.
func gotSlot(t *testing.T, got <-chan error) {
	t.Helper()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("waiting video got error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting video never got a slot")
	}
}

// stillWaiting fails if any of the waiting videos got a slot.
func stillWaiting(t *testing.T, waiters ...<-chan error) {
	t.Helper()
	for i, got := range waiters {
		select {
		case err := <-got:
			t.Fatalf("waiting video %d got a slot (error %v), want it still waiting", i, err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestLaneSharing tests channel URLs share their channel's lane unless they have a limit of their own.
func TestLaneSharing(t *testing.T) {
	var l lanes
	l.setLimits(1, downloadLimits(limitsChannel(1, time.Now(), 1, map[int64]int{10: 0, 11: 2})))

	takeSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 12) // A URL the channel was loaded without shares its lane too.
	takeSlot(t, &l, 1, 11)
	takeSlot(t, &l, 1, 11)
	assertNoSlot(t, &l, 1, 11)
}

// TestLaneLimitsFromSettings tests the lane limits read from a channel's download and Metarr concurrency settings.
func TestLaneLimitsFromSettings(t *testing.T) {
	c := limitsChannel(1, time.Now(), 0, map[int64]int{10: 0, 11: 3})
	if got := downloadLimits(c); got.channel != 1 || got.urls[10] != 0 || got.urls[11] != 3 {
		t.Errorf("downloadLimits = %+v, want the channel at 1, URL 10 sharing its lane and URL 11 at 3", got)
	}
	if got := metarrLimits(c); got.channel != 0 || got.urls[10] != 0 || got.urls[11] != 3 {
		t.Errorf("metarrLimits = %+v, want the channel unlimited, URL 10 sharing its lane and URL 11 at 3", got)
	}
	if got := downloadLimits(limitsChannel(1, time.Now(), 2, map[int64]int{12: 2})); got.urls[12] != 0 {
		t.Errorf("downloadLimits = %+v, want URL 12 with the channel's limit sharing its lane", got)
	}
}

// TestLaneChangedLimitStartsWaitingVideos tests a raised or removed limit gives a waiting video a slot straight away.
func TestLaneChangedLimitStartsWaitingVideos(t *testing.T) {
	loaded := time.Now()
	for name, newLimit := range map[string]int{"raised": 2, "removed": 0} {
		t.Run(name, func(t *testing.T) {
			var l lanes
			l.setLimits(1, metarrLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0})))
			takeSlot(t, &l, 1, 10)
			waiter := startWaiter(t, &l, 1, 10)

			l.setLimits(1, metarrLimits(limitsChannel(1, loaded.Add(time.Second), newLimit, map[int64]int{10: 0})))
			gotSlot(t, waiter)
		})
	}
}

// TestLaneLoweredLimitCountsRunningVideos tests a lowered limit starts no video until fewer run than it allows.
func TestLaneLoweredLimitCountsRunningVideos(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 3, nil)))
	for range 3 {
		mustAcquire(t, &l, 1, 10)
	}
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 1, nil)))

	l.release(1, 10)
	l.release(1, 10)
	assertNoSlot(t, &l, 1, 10)
	l.release(1, 10)
	mustAcquire(t, &l, 1, 10)
}

// TestLaneURLGainsOwnLimit tests a URL given its own limit takes its running video to its own lane.
func TestLaneURLGainsOwnLimit(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0, 11: 0})))
	takeSlot(t, &l, 1, 11)
	other := startWaiter(t, &l, 1, 10)
	same := startWaiter(t, &l, 1, 11)

	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 1, map[int64]int{10: 0, 11: 2})))
	gotSlot(t, other)
	gotSlot(t, same)
	assertNoSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 11)
}

// TestLaneURLLosesOwnLimit tests a URL whose own limit is removed counts its running videos in the channel's lane.
func TestLaneURLLosesOwnLimit(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0, 11: 2})))
	mustAcquire(t, &l, 1, 11)
	mustAcquire(t, &l, 1, 11)
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 1, map[int64]int{10: 0, 11: 0})))

	// Both now run in the channel's lane of 1, so its next video waits until neither does.
	assertNoSlot(t, &l, 1, 10)
	l.release(1, 11)
	assertNoSlot(t, &l, 1, 10)
	l.release(1, 11)
	mustAcquire(t, &l, 1, 10)
}

// TestLaneKeepsNewestLimits tests limits from an older model of a channel never replace newer ones.
func TestLaneKeepsNewestLimits(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 2, nil)))
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(-time.Second), 1, nil)))

	takeSlot(t, &l, 1, 10)
	takeSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 10)
}

// TestLaneLimitsLeavingOutURLs tests limits from a channel loaded with only some URLs, as a crawl's is, keep the rest's.
func TestLaneLimitsLeavingOutURLs(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0, 11: 0, 12: 2})))
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 3, map[int64]int{10: 0})))

	// URL 12 keeps its own limit of 2.
	takeSlot(t, &l, 1, 12)
	takeSlot(t, &l, 1, 12)
	assertNoSlot(t, &l, 1, 12)

	// URLs 10 and 11 share the channel's new limit of 3.
	takeSlot(t, &l, 1, 10)
	takeSlot(t, &l, 1, 11)
	takeSlot(t, &l, 1, 11)
	assertNoSlot(t, &l, 1, 10)
}

// TestLaneOrder tests waiting videos get slots in the order they asked, also when a raised limit frees them.
func TestLaneOrder(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, nil)))
	mustAcquire(t, &l, 1, 10)
	first := startWaiter(t, &l, 1, 10)
	second := startWaiter(t, &l, 1, 10)
	third := startWaiter(t, &l, 1, 10)

	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 2, nil)))
	gotSlot(t, first)
	stillWaiting(t, second, third)

	l.release(1, 10)
	gotSlot(t, second)
	stillWaiting(t, third)
}

// TestLaneCanceledWait tests a video whose wait ends leaves the line without taking a slot.
func TestLaneCanceledWait(t *testing.T) {
	var l lanes
	l.setLimits(1, downloadLimits(limitsChannel(1, time.Now(), 1, nil)))
	mustAcquire(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 10)
	if n := waitingLen(&l); n != 0 {
		t.Fatalf("%d videos still waiting after their wait ended", n)
	}

	l.release(1, 10)
	mustAcquire(t, &l, 1, 10)
	l.release(1, 10)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.held) != 0 {
		t.Errorf("slots still held after every release: %v", l.held)
	}
}

// channelLimit returns the channel's limit the lanes hold, or -1 if they hold none.
func channelLimit(l *lanes, channelID int64) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim := l.limits[channelID]; lim != nil {
		return lim.channel
	}
	return -1
}

// abortAndWait aborts the channel's videos in the download queue and waits for them to leave it.
func abortAndWait(t *testing.T, dq *DownloadQueue, channelID int64) {
	t.Helper()
	dq.Abort(channelID)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("aborted videos still in the queue")
	}
}

// TestAddAppliesChannelLimits tests Add gives both queues its channel's limits before its videos ask for slots.
func TestAddAppliesChannelLimits(t *testing.T) {
	store, _, c := newTestChannel(t)
	cu := c.URLModels[0]
	c.ChanMetarrArgs.MetarrConcurrency = 3
	pp := NewPostProcessingQueue(store)
	dq := NewDownloadQueue(t.Context(), store, pp)
	takeSlot(t, &dq.lanes, c.ID, cu.ID)

	if queued, err := dq.Add(c, Request{Video: &models.Video{URL: "https://example.com/v/1", ChannelURLID: cu.ID}}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want 1", len(queued), err)
	}
	if got := channelLimit(&dq.lanes, c.ID); got != 1 {
		t.Errorf("download limit after Add = %d, want the channel's 1", got)
	}
	if got := channelLimit(&pp.lanes, c.ID); got != 3 {
		t.Errorf("Metarr limit after Add = %d, want the channel's 3", got)
	}
	if entries := dq.Entries(); len(entries) != 1 || entries[0].DownloadStatus.Stage != "Waiting to start" {
		t.Errorf("entries = %+v, want the video waiting for the channel's one slot", entries)
	}
	abortAndWait(t, dq, c.ID)
}

// TestAddKeepsNewerLimits tests a channel loaded before its last save, as a long crawl's is, can't bring old limits back.
func TestAddKeepsNewerLimits(t *testing.T) {
	store, _, crawled := newTestChannel(t)
	cu := crawled.URLModels[0]
	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))

	// Saved and applied after the crawl loaded the channel.
	id := strconv.FormatInt(crawled.ID, 10)
	if _, err := store.ChannelStore().UpdateChannelSettingsJSON(consts.QChanID, id, func(s *models.Settings) error {
		s.DownloadConcurrency = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, _, err := store.ChannelStore().GetChannelModel(consts.QChanID, id, true)
	if err != nil {
		t.Fatal(err)
	}
	dq.SetLimits(saved)
	takeSlot(t, &dq.lanes, saved.ID, cu.ID)
	takeSlot(t, &dq.lanes, saved.ID, cu.ID)

	// The crawl adds a video with its older copy.
	if queued, err := dq.Add(crawled, Request{Video: &models.Video{URL: "https://example.com/v/1", ChannelURLID: cu.ID}}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want 1", len(queued), err)
	}
	if got := channelLimit(&dq.lanes, saved.ID); got != 2 {
		t.Errorf("download limit after adding with the older copy = %d, want the saved 2", got)
	}
	abortAndWait(t, dq, saved.ID)
}

// TestLanesConcurrentStress tests limits hold and every slot is released under heavy use while limits change.
func TestLanesConcurrentStress(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Channel 1's limits never change: URLs 10 and 11 share 2 slots, and URL 12 has 3 of its own.
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 2, map[int64]int{10: 0, 11: 0, 12: 3})))
	var shared, own atomic.Int32
	running := map[int64]*atomic.Int32{10: &shared, 11: &shared, 12: &own}
	limit := map[int64]int32{10: 2, 11: 2, 12: 3}

	// Channel 2's limits keep changing.
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 300 {
			l.setLimits(2, metarrLimits(limitsChannel(2, loaded.Add(time.Duration(i+1)), i%4, map[int64]int{20: 0, 21: i % 3})))
		}
	})

	for i := range 32 {
		wg.Go(func() {
			for j := range 50 {
				channelID := int64(1)
				channelURLID := int64(10 + (i+j)%3)
				if (i+j)%2 == 0 {
					channelID = 2
					channelURLID = int64(20 + (i+j)%2)
				}
				if err := l.acquire(ctx, channelID, channelURLID); err != nil {
					t.Errorf("acquire failed (deadlock?): %v", err)
					return
				}
				if channelID == 1 {
					if n := running[channelURLID].Add(1); n > limit[channelURLID] {
						t.Errorf("%d videos running in channel URL %d's lane, over its limit of %d", n, channelURLID, limit[channelURLID])
					}
					time.Sleep(time.Microsecond)
					running[channelURLID].Add(-1)
				}
				l.release(channelID, channelURLID)
			}
		})
	}
	wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.held) != 0 || len(l.waiting) != 0 {
		t.Errorf("after every release, %v slots held and %d videos waiting", l.held, len(l.waiting))
	}
}

// ******************************** Post-processing ***********************************************************************************

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
	cu := &models.ChannelURL{ID: 1, ChannelID: c.ID, ChannelName: c.Name, URL: "https://example.com/@cats"}
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
	cu := &models.ChannelURL{ID: 1, ChannelID: c.ID, ChannelName: c.Name, URL: "https://example.com/@cats", ChanURLMetarrArgs: &models.MetarrArgs{MetarrConcurrency: 1}}
	c.URLModels = []*models.ChannelURL{cu}
	q.SetLimits(c)
	takeSlot(t, &q.lanes, c.ID, cu.ID)

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
	if err := os.WriteFile(filepath.Join(bin, sharedconsts.ProgramMetarr), []byte(script), 0o755); err != nil {
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

// TestAutoPostProcessOff tests turning off Post-Process After Download holds back Metarr after downloads only, while
// the user's own Post-Process still runs it.
func TestAutoPostProcessOff(t *testing.T) {
	// A metarr that answers --help, and records any real run.
	bin, dir := t.TempDir(), t.TempDir()
	ran := filepath.Join(dir, "metarr-ran")
	script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = --help ] && exit 0\ntouch %q\n", ran)
	if err := os.WriteFile(filepath.Join(bin, sharedconsts.ProgramMetarr), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// After a download: off holds Metarr back, unset and on don't.
	yes, no := true, false
	c := &models.Channel{Name: "Cats", ChanSettings: &models.Settings{VideoDir: dir}}
	for _, tc := range []struct {
		name string
		auto *bool
		want models.SkipMetarr
	}{
		{"unset", nil, models.SkipMetarr{}},
		{"on", &yes, models.SkipMetarr{}},
		{"off", &no, models.SkipMetarr{Skip: true, Reason: models.SkipMetarrAutoPostProcessOff}},
	} {
		cu := &models.ChannelURL{
			URL:               "https://example.com/@cats",
			ChanURLMetarrArgs: &models.MetarrArgs{AutoPostProcess: tc.auto},
			Session:           models.ChannelURLSession{CookiePath: "fetched"}, // Already fetched, so no cookie store is needed.
		}
		s := &session{cu: cu}
		if err := s.ready(context.Background(), nil, c); err != nil {
			t.Fatalf("%s: ready = %v", tc.name, err)
		}
		if s.skipMetarr != tc.want {
			t.Errorf("%s: skip = %+v, want %+v", tc.name, s.skipMetarr, tc.want)
		}
	}

	// The user's Post-Process still runs Metarr.
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
	id, err := store.ChannelStore().AddChannel(&models.Channel{
		Name:           "Cats",
		ChanSettings:   &models.Settings{VideoDir: dir},
		ChanMetarrArgs: &models.MetarrArgs{AutoPostProcess: &no},
		URLModels:      []*models.ChannelURL{{URL: "https://example.com/@cats"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.ChannelStore().GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
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
		loaded.ID, loaded.URLModels[0].ID, loaded.URLModels[0].URL, video, metaFile, consts.MetarrStatusPending); err != nil {
		t.Fatal(err)
	}
	_ = q.RunMetarr(context.Background(), loaded, nil, false) // The stand-in Metarr gives no final paths, so this fails after running.
	if _, err := os.Stat(ran); err != nil {
		t.Error("Post-Process didn't run Metarr with Post-Process After Download turned off")
	}
}
