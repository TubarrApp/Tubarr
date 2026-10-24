package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/limits"
	"tubarr/internal/metadata"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// PostProcessingQueue holds the downloaded videos waiting for Metarr, and those running in it.
type PostProcessingQueue struct {
	store contracts.Store
	lanes lanes

	mu      sync.Mutex
	claims  map[videoKey]*claim // Videos held by a download, or in the queue.
	stops   map[int64]int       // How many times each channel has been stopped.
	changed chan struct{}       // Closed and replaced whenever a video is released.
}

// videoKey identifies a video before it has a database ID.
type videoKey struct {
	channelID int64
	url       string
}

// claim is a video held by a download, or in the queue.
type claim struct {
	download bool               // Held by a download, which other downloads can't take over.
	stops    int                // How many times the channel had been stopped when its download took the video.
	entry    *Entry             // Set while the video is in the queue.
	cancel   context.CancelFunc // Cancels the video's Metarr run, set with entry.
	done     chan struct{}      // Closed when the video is released.
}

// videoHold is a video reserved for its download.
type videoHold struct {
	q     *PostProcessingQueue
	key   videoKey
	claim *claim
}

// NewPostProcessingQueue returns an empty post-processing queue.
func NewPostProcessingQueue(s contracts.Store) *PostProcessingQueue {
	return &PostProcessingQueue{
		store:   s,
		claims:  make(map[videoKey]*claim),
		stops:   make(map[int64]int),
		changed: make(chan struct{}),
	}
}

// hold reserves a video for its download, first canceling and waiting for any Metarr run on it.
//
// Returns false if another download already holds it.
func (q *PostProcessingQueue) hold(ctx context.Context, c *models.Channel, videoURL string) (*videoHold, bool, error) {
	key := videoKey{c.ID, videoURL}
	for {
		q.mu.Lock()
		held, ok := q.claims[key]
		if !ok {
			cl := &claim{download: true, stops: q.stops[c.ID], done: make(chan struct{})}
			q.claims[key] = cl
			q.mu.Unlock()
			return &videoHold{q: q, key: key, claim: cl}, true, nil
		}
		if held.download {
			q.mu.Unlock()
			return nil, false, nil
		}
		logger.Pl.I("Cancelling Metarr run for video %q in favor of new download", videoURL)
		held.cancel()
		q.mu.Unlock()

		select {
		case <-held.done:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
}

// postProcess puts the held video in the queue and runs Metarr on it once its turn comes, returning its Metarr status.
//
// The video stays pending if its channel was stopped since its download took it, or it's stopped while it waits.
func (h *videoHold) postProcess(ctx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dp *parsing.DirectoryParser) consts.MetarrStatus {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	q := h.q
	q.mu.Lock()
	stopped := q.stops[c.ID] != h.claim.stops
	if !stopped {
		h.claim.entry, h.claim.cancel = newEntry(v, cu, c), cancel
	}
	q.mu.Unlock()
	if stopped {
		logger.Pl.W("Metarr not started for video %s as its channel was stopped, marking as pending", v.URL)
		return consts.MetarrStatusPending
	}

	release, err := q.acquire(ctx, c, cu)
	if err != nil {
		logger.Pl.W("Metarr not started for video %s, marking as pending", v.URL)
		return consts.MetarrStatusPending
	}
	defer release()
	q.started(h.claim)
	return execMetarr(ctx, v, cu, c, dp)
}

// release ends the hold, once the video's download no longer needs it.
func (h *videoHold) release() {
	h.q.release(h.key, h.claim)
}

// RunMetarr runs Metarr on finished videos in a channel which Metarr has not yet processed, and with rerun on given
// videos it already has, using their finished files. It returns once they're done.
//
// If videoIDs is empty, all videos awaiting Metarr in the channel are processed.
func (q *PostProcessingQueue) RunMetarr(ctx context.Context, c *models.Channel, videoIDs []int64, rerun bool) error {
	if c == nil {
		return errors.New("invalid nil channel")
	}
	if c.LoadErr != nil {
		return c.LoadErr
	}

	if err := metarr.CheckInstalled(ctx); err != nil {
		return fmt.Errorf("cannot run 'metarr': %w", err)
	}

	if rerun && len(videoIDs) == 0 {
		return errors.New("a Metarr re-run needs the videos to run on")
	}
	videos, err := q.store.VideoStore().GetVideosAwaitingMetarr(c.ID, rerun)
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
		wg    sync.WaitGroup
		mu    sync.Mutex
		errs  []error
		skips = map[int64]error{} // Channel URL ID -> why its videos can't run, checked once per URL.
	)

	for _, v := range videos {
		cu, err := ManualChannelURL(q.store.ChannelStore(), c, v.ChannelURLID)
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("video %q: %w", v.URL, err))
			mu.Unlock()
			continue
		}

		// An unusable ops file leaves the URL's videos pending rather than run without its ops. Download As videos don't use them.
		skipErr, checked := skips[cu.ID]
		if !checked {
			var skip models.SkipMetarr
			if skip, skipErr = metarr.SkipReason(nil, cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)); skipErr != nil {
				skipErr = fmt.Errorf("%s: %w", skip.Reason, skipErr)
				logger.Pl.E("Not running Metarr for %q, its videos stay pending: %v", cu.URL, skipErr)
			}
			skips[cu.ID] = skipErr
		}
		if skipErr != nil && v.Overrides == nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("video %q: %w", v.URL, skipErr))
			mu.Unlock()
			continue
		}

		key := videoKey{c.ID, v.URL}
		vCtx, vCancel := context.WithCancel(ctx)
		cl, added := q.add(key, newEntry(v, cu, c), vCancel)
		if !added {
			vCancel()
			logger.Pl.W("Video %q is already being processed, skipping", v.URL)
			continue
		}

		wg.Go(func() {
			defer q.release(key, cl)
			defer vCancel()

			release, err := q.acquire(vCtx, c, cu)
			if err != nil {
				return
			}
			defer release()
			q.started(cl)

			if err := runVideoMetarr(vCtx, q.store.VideoStore(), v, cu, c); err != nil {
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

// Cancel cancels the channel's given videos in the queue, waiting or running, or all of its videos if none are given.
// Returns how many were canceled.
func (q *PostProcessingQueue) Cancel(channelID int64, videoIDs []int64) (n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for key, cl := range q.claims {
		if key.channelID == channelID && cl.entry != nil && (len(videoIDs) == 0 || slices.Contains(videoIDs, cl.entry.VideoID)) {
			cl.cancel()
			n++
		}
	}
	return n
}

// Stop cancels the channel's videos waiting in the queue, and keeps the videos its downloads hold from being
// post-processed. Returns how many were canceled.
func (q *PostProcessingQueue) Stop(channelID int64) (n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stops[channelID]++
	for key, cl := range q.claims {
		if key.channelID == channelID && cl.entry != nil && cl.entry.StartedAt == nil {
			cl.cancel()
			n++
		}
	}
	return n
}

// Abort cancels all of the channel's videos in the queue, and keeps the videos its downloads hold from being
// post-processed. Returns how many were canceled.
func (q *PostProcessingQueue) Abort(channelID int64) (n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stops[channelID]++
	for key, cl := range q.claims {
		if key.channelID == channelID && cl.entry != nil {
			cl.cancel()
			n++
		}
	}
	return n
}

// Entries returns the videos in the queue, oldest first.
func (q *PostProcessingQueue) Entries() []Entry {
	q.mu.Lock()
	entries := make([]Entry, 0, len(q.claims))
	for _, cl := range q.claims {
		if cl.entry != nil {
			entries = append(entries, *cl.entry)
		}
	}
	q.mu.Unlock()

	for i := range entries {
		if entries[i].StartedAt != nil {
			entries[i].Progress = metarr.VideoProgress(entries[i].VideoID)
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int { return a.QueuedAt.Compare(b.QueuedAt) })
	return entries
}

// Busy returns how many of the channel's given videos are in the queue.
func (q *PostProcessingQueue) Busy(channelID int64, videoIDs []int64) (n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for key, cl := range q.claims {
		if key.channelID == channelID && cl.entry != nil && slices.Contains(videoIDs, cl.entry.VideoID) {
			n++
		}
	}
	return n
}

// Len returns how many videos are held by a download or in the queue.
func (q *PostProcessingQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.claims)
}

// Wait waits until no video is held by a download or in the queue, returning false if ctx ends first.
func (q *PostProcessingQueue) Wait(ctx context.Context) bool {
	for {
		q.mu.Lock()
		n, changed := len(q.claims), q.changed
		q.mu.Unlock()
		if n == 0 {
			return true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

// add puts a video in the queue for a Metarr run, returning false if a download holds it or it's already in the queue.
func (q *PostProcessingQueue) add(key videoKey, e *Entry, cancel context.CancelFunc) (*claim, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, held := q.claims[key]; held {
		return nil, false
	}
	cl := &claim{entry: e, cancel: cancel, done: make(chan struct{})}
	q.claims[key] = cl
	return cl, true
}

// started marks a video in the queue as running in Metarr.
func (q *PostProcessingQueue) started(cl *claim) {
	now := time.Now()
	q.mu.Lock()
	defer q.mu.Unlock()
	cl.entry.StartedAt = &now
}

// release takes a video out of the queue, or ends its download's hold on it.
func (q *PostProcessingQueue) release(key videoKey, cl *claim) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.claims[key] == cl {
		delete(q.claims, key)
	}
	close(cl.done)
	close(q.changed)
	q.changed = make(chan struct{})
}

// acquire waits for a slot in the video's lane, then a global one, returning a function releasing both.
//
// The global slot is taken last, so a video waiting for its lane doesn't hold one.
func (q *PostProcessingQueue) acquire(ctx context.Context, c *models.Channel, cu *models.ChannelURL) (release func(), err error) {
	var chanLimit, urlLimit int
	if c.ChanMetarrArgs != nil {
		chanLimit = c.ChanMetarrArgs.MetarrConcurrency
	}
	if cu.ChanURLMetarrArgs != nil {
		urlLimit = cu.ChanURLMetarrArgs.MetarrConcurrency // Merged, so equal to the channel's when inherited.
	}

	lane := q.lanes.lane(c, cu, chanLimit, urlLimit)
	if lane != nil {
		if err := lane.Acquire(ctx); err != nil {
			return nil, err
		}
	}
	if err := limits.GlobalPostProcessing.Acquire(ctx); err != nil {
		if lane != nil {
			lane.Release()
		}
		return nil, err
	}
	return func() {
		limits.GlobalPostProcessing.Release()
		if lane != nil {
			lane.Release()
		}
	}, nil
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

	oldVideo, oldJSON := v.VideoFilePath, v.JSONFilePath
	dirParser := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	// Video already passed filters on download, only the filtered ops are needed.
	_, v.FilteredMetaOps, v.FilteredFilenameOps, _ = metadata.ValidateAndFilter(v, cu, c, dirParser)
	v.MoveOpOutputDir = metadata.HandleMoveOps(v, cu, dirParser)

	v.MetarrStatus = execMetarr(ctx, v, cu, c, dirParser)

	// Canceled (stopped, new download of this video, or shutdown), leave the database entry as it was.
	if ctx.Err() != nil {
		logger.Pl.I("Metarr run for video %q was canceled, not storing result", v.URL)
		// Metarr may have moved files before stopping, so keep where they are unless a newer download changed the row.
		if v.VideoFilePath != oldVideo || v.JSONFilePath != oldJSON {
			if updated, err := vs.UpdateVideoPaths(v.ID, oldVideo, oldJSON, v.VideoFilePath, v.JSONFilePath); err != nil {
				logger.Pl.E("Failed to store file paths for canceled Metarr run of video %q: %v", v.URL, err)
			} else if updated {
				logger.Pl.I("Stored file paths Metarr reported before it was canceled for video %q", v.URL)
			}
		}
		return nil //nolint:nilerr // Cancellation is not a failure.
	}

	v.MarkVideoAsComplete()

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
			logger.Pl.W("Metarr canceled for video %q: %v", v.URL, err)
			return consts.MetarrStatusPending
		}
		logger.Pl.E("Metarr processing error for video %q: %v", v.URL, err)
		v.MetarrError = err.Error()
		return consts.MetarrStatusFailed
	}
	v.MetarrError = ""
	return consts.MetarrStatusDone
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
