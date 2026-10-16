package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"tubarr/internal/abstractions"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/metadata"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// metarrClaims holds videos currently owned by a Metarr run or the download pipeline, keyed by metarrKey.
var metarrClaims sync.Map

// metarrKey identifies a video before it has a database ID.
type metarrKey struct {
	channelID int64
	url       string
}

// metarrClaim is a hold on a video.
type metarrClaim struct {
	channelName string
	cancel      context.CancelFunc // Cancels the video's Metarr run.
	pipeline    bool               // Held by the download pipeline, which other downloads cannot preempt.
	inMetarr    atomic.Bool        // Queued for or running in Metarr, set once the fields below are.
	done        chan struct{}

	videoID      int64
	channelURLID int64
	channelURL   string
	title        string
	queuedAt     time.Time
	startedAt    atomic.Int64 // Unix milliseconds the Metarr process started, 0 while queued.
}

// enqueue records the video a claim holds and marks it as queued for Metarr.
func (claim *metarrClaim) enqueue(v *models.Video, cu *models.ChannelURL) {
	claim.videoID, claim.channelURLID, claim.channelURL, claim.title = v.ID, cu.ID, cu.URL, v.Title
	claim.queuedAt = time.Now()
	claim.inMetarr.Store(true)
}

// MetarrJob is a video queued for or running in Metarr.
type MetarrJob struct {
	VideoID      int64      `json:"video_id"`
	ChannelID    int64      `json:"channel_id"`
	ChannelName  string     `json:"channel_name"`
	ChannelURLID int64      `json:"channel_url_id"`
	ChannelURL   string     `json:"channel_url"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	QueuedAt     time.Time  `json:"queued_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
}

// metarrSems holds the Metarr semaphores shared by all runs, keyed by metarrSemKey.
var metarrSems sync.Map

// metarrSemKey identifies a channel URL's Metarr semaphore, a new one being used if its concurrency limit changes.
type metarrSemKey struct {
	channelURLID int64
	limit        int
}

// metarrSem returns the Metarr semaphore shared by all runs for a channel URL.
func metarrSem(cu *models.ChannelURL) chan struct{} {
	n := metarrConcurrency(cu)
	v, _ := metarrSems.LoadOrStore(metarrSemKey{cu.ID, n}, make(chan struct{}, n))
	sem, _ := v.(chan struct{})
	return sem
}

// RunMetarr runs Metarr on finished videos in a channel which Metarr has not yet processed.
//
// If videoIDs is empty, all videos awaiting Metarr in the channel are processed.
func RunMetarr(ctx context.Context, s contracts.Store, c *models.Channel, videoIDs []int64) error {
	if c == nil {
		return errors.New("invalid nil channel")
	}

	AddRunningProcess()
	defer SubtractRunningProcess()

	if err := metarrExecutable(ctx); err != nil {
		return fmt.Errorf("cannot run 'metarr': %w", err)
	}

	videos, err := s.VideoStore().GetVideosAwaitingMetarr(c.ID)
	if err != nil {
		return err
	}
	if len(videoIDs) > 0 {
		videos = filterVideosByID(videos, videoIDs)
	}
	if len(videos) == 0 {
		logger.Pl.I("No videos awaiting Metarr in channel %q", c.Name)
		return nil
	}

	logger.Pl.I("Running Metarr on %d video(s) in channel %q", len(videos), c.Name)

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for _, v := range videos {
		cu := findChannelURL(c, v.ChannelURLID)
		if cu == nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("video %q: channel URL no longer exists", v.URL))
			mu.Unlock()
			continue
		}

		key := metarrKey{c.ID, v.URL}
		vCtx, vCancel := context.WithCancel(ctx)
		claim := &metarrClaim{channelName: c.Name, cancel: vCancel, done: make(chan struct{})}
		claim.enqueue(v, cu)
		if _, loaded := metarrClaims.LoadOrStore(key, claim); loaded {
			vCancel()
			logger.Pl.W("Video %q is already being processed, skipping", v.URL)
			continue
		}

		sem := metarrSem(cu)
		wg.Go(func() {
			defer releaseMetarr(key, claim)
			defer vCancel()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-vCtx.Done():
				return
			}
			claim.startedAt.Store(time.Now().UnixMilli())

			if err := runVideoMetarr(vCtx, s.VideoStore(), v, cu, c); err != nil {
				logger.Pl.E("Metarr run failed for video %q: %v", v.URL, err)
				mu.Lock()
				errs = append(errs, fmt.Errorf("video %q: %w", v.URL, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	return errors.Join(errs...)
}

// runVideoMetarr rebuilds a video's Metarr state from its metadata file and runs Metarr on it.
func runVideoMetarr(ctx context.Context, vs contracts.VideoStore, v *models.Video, cu *models.ChannelURL, c *models.Channel) error {
	for _, f := range []string{v.VideoFilePath, v.JSONFilePath} {
		if f == "" {
			return errors.New("source file path not stored in database")
		}
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("source file no longer present: %w", err)
		}
	}

	dirParser := parsing.NewDirectoryParser(c, parsing.TubarrTags)

	// Video already passed filters on download, only the filtered ops are needed.
	_, v.FilteredMetaOps, v.FilteredFilenameOps, _ = metadata.ValidateAndFilter(v, cu, c, dirParser)
	v.MoveOpOutputDir = metadata.HandleMoveOps(v, cu, dirParser)

	v.MetarrStatus = execMetarr(ctx, v, cu, c, dirParser)

	// Cancelled (stopped, new download of this video, or shutdown), leave the database entry as it was.
	if ctx.Err() != nil {
		logger.Pl.I("Metarr run for video %q was cancelled, not storing result", v.URL)
		return nil //nolint:nilerr // Cancellation is not a failure.
	}

	v.MarkVideoAsComplete()
	purgeMetaFileIfSet(v)

	if err := vs.UpdateVideo(v, c.ID); err != nil {
		return fmt.Errorf("failed to update video DB entry: %w", err)
	}
	if v.MetarrStatus != consts.MetarrStatusDone {
		return fmt.Errorf("metarr status %q", v.MetarrStatus)
	}
	return nil
}

// execMetarr runs Metarr on a video and returns the resulting status.
func execMetarr(ctx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) consts.MetarrStatus {
	if err := metarr.InitMetarr(ctx, v, cu, c, dirParser); err != nil {
		if ctx.Err() != nil {
			logger.Pl.W("Metarr cancelled for video %q: %v", v.URL, err)
			return consts.MetarrStatusPending
		}
		logger.Pl.E("Metarr processing error for video %q: %v", v.URL, err)
		return consts.MetarrStatusFailed
	}
	return consts.MetarrStatusDone
}

// purgeMetaFileIfSet deletes the metadata file if configured, once Metarr no longer needs it.
func purgeMetaFileIfSet(v *models.Video) {
	if v.MetarrStatus != consts.MetarrStatusDone || !abstractions.IsSet(keys.PurgeMetaFile) || !abstractions.GetBool(keys.PurgeMetaFile) {
		return
	}

	logger.Pl.I("Purging metadata file for video with URL %q as per configuration", v.URL)
	if err := os.Remove(v.JSONFilePath); err != nil && !os.IsNotExist(err) {
		logger.Pl.W("Failed to delete metadata file %q: %v", v.JSONFilePath, err)
		return
	}
	logger.Pl.D(1, "Deleted metadata file %q successfully", v.JSONFilePath)
	v.JSONFilePath = ""
}

// metarrExecutable returns an error if Metarr is not in $PATH or cannot be executed.
func metarrExecutable(ctx context.Context) error {
	if _, err := exec.LookPath("metarr"); err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, "metarr", "--help").Run(); err != nil {
		return fmt.Errorf("found 'metarr' in $PATH but failed to execute: %w", err)
	}
	return nil
}

// metarrConcurrency returns the Metarr concurrency limit for a channel URL.
func metarrConcurrency(cu *models.ChannelURL) int {
	if cu.ChanURLMetarrArgs.Concurrency > 0 {
		return cu.ChanURLMetarrArgs.Concurrency
	}
	if abstractions.IsSet(keys.MConcurrency) {
		return sharedvalidation.ValidateConcurrencyLimit(abstractions.GetInt(keys.MConcurrency))
	}
	return 1
}

// StopMetarr cancels a channel's queued and running Metarr processes, returning how many were stopped.
//
// If videoIDs is empty, all of the channel's Metarr processes are stopped.
func StopMetarr(channelID int64, videoIDs []int64) (n int) {
	metarrClaims.Range(func(k, v any) bool {
		key, _ := k.(metarrKey)
		claim, ok := v.(*metarrClaim)
		if !ok || key.channelID != channelID || !claim.inMetarr.Load() {
			return true
		}
		if len(videoIDs) == 0 || slices.Contains(videoIDs, claim.videoID) {
			claim.cancel()
			n++
		}
		return true
	})
	return n
}

// MetarrJobs returns the videos queued for or running in Metarr, oldest first.
func MetarrJobs() []MetarrJob {
	jobs := []MetarrJob{}
	metarrClaims.Range(func(k, v any) bool {
		key, _ := k.(metarrKey)
		claim, ok := v.(*metarrClaim)
		if !ok || !claim.inMetarr.Load() {
			return true
		}
		job := MetarrJob{
			VideoID:      claim.videoID,
			ChannelID:    key.channelID,
			ChannelName:  claim.channelName,
			ChannelURLID: claim.channelURLID,
			ChannelURL:   claim.channelURL,
			URL:          key.url,
			Title:        claim.title,
			QueuedAt:     claim.queuedAt,
		}
		if ms := claim.startedAt.Load(); ms != 0 {
			started := time.UnixMilli(ms)
			job.StartedAt = &started
		}
		jobs = append(jobs, job)
		return true
	})
	slices.SortFunc(jobs, func(a, b MetarrJob) int { return a.QueuedAt.Compare(b.QueuedAt) })
	return jobs
}

// MetarrActivity returns the names of channels with queued or running Metarr processes.
func MetarrActivity() []string {
	seen := map[string]struct{}{}
	names := []string{}
	metarrClaims.Range(func(_, v any) bool {
		if claim, ok := v.(*metarrClaim); ok && claim.inMetarr.Load() {
			if _, dup := seen[claim.channelName]; !dup {
				seen[claim.channelName] = struct{}{}
				names = append(names, claim.channelName)
			}
		}
		return true
	})
	return names
}

// preemptMetarr takes a hold on a video for the download pipeline, cancelling and awaiting any separate Metarr run on it.
//
// Returns false if another download already holds the video.
func preemptMetarr(ctx context.Context, key metarrKey, claim *metarrClaim) (bool, error) {
	for {
		existing, loaded := metarrClaims.LoadOrStore(key, claim)
		if !loaded {
			return true, nil
		}

		held, ok := existing.(*metarrClaim)
		if !ok || held.pipeline {
			return false, nil
		}

		logger.Pl.I("Cancelling Metarr run for video %q in favour of new download", key.url)
		held.cancel()
		select {
		case <-held.done:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// releaseMetarr releases a hold on a video.
func releaseMetarr(key metarrKey, claim *metarrClaim) {
	if claim == nil {
		return
	}
	metarrClaims.CompareAndDelete(key, claim)
	close(claim.done)
}

// findChannelURL returns the channel URL model with the given ID, or nil.
func findChannelURL(c *models.Channel, id int64) *models.ChannelURL {
	for _, cu := range c.URLModels {
		if cu.ID == id {
			return cu
		}
	}
	return nil
}

// filterVideosByID returns the videos whose IDs are in ids.
func filterVideosByID(videos []*models.Video, ids []int64) []*models.Video {
	want := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make([]*models.Video, 0, len(ids))
	for _, v := range videos {
		if _, ok := want[v.ID]; ok {
			out = append(out, v)
		}
	}
	return out
}
