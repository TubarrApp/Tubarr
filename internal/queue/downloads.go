package queue

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads"
	"tubarr/internal/file"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/network"
	"tubarr/internal/notify"
	"tubarr/internal/parsing"
	"tubarr/internal/scraper"
)

// filteredDisplayTime is how long a filtered out video stays on the Downloads page.
const filteredDisplayTime = 5 * time.Second

// errStopped is why a crawled video stopped before its download began failed.
var errStopped = errors.New("the queue was stopped before this video started")

// errAborted is the cause of a download canceled by Abort All.
var errAborted = errors.New("the channel's downloads were aborted")

// Request asks for one video to be downloaded.
type Request struct {
	Video            *models.Video          // Its URL and channel URL, or its ID if it's stored, with anything a crawl found.
	ByUser           bool                   // Chosen from Available or entered as a URL, rather than found by a crawl.
	Auth             *models.ManualAuth     // Login details for this download only, never stored.
	ReplaceOverrides bool                   // Store Overrides as the video's Download As options, nil clearing them.
	Overrides        *models.VideoOverrides // Download As options.
}

// DownloadQueue holds the videos waiting to be downloaded, and those downloading.
type DownloadQueue struct {
	ctx            context.Context // Canceled on shutdown.
	store          contracts.Store
	postProcessing *PostProcessingQueue
	lanes          lanes

	mu        sync.Mutex
	downloads map[int64]*download // By video ID.
	urls      map[string]int64    // Video IDs by URL, so two channels never download the same video at once.
	stopping  map[int64]bool      // Channels stopped while they still have videos in the queue.
	running   map[int64]int       // Videos each channel has downloading or post-processing.
	newVideos map[int64][]string  // Channel URLs that got new videos, by channel, notified once none of its videos are running.
	changed   chan struct{}       // Closed and replaced whenever a video leaves the queue.
}

// download is a video in the download queue.
type download struct {
	q         *DownloadQueue
	entry     Entry           // As the Downloads page shows it.
	status    models.DLStatus // Its download status as last reported.
	video     *models.Video
	c         *models.Channel
	cu        *models.ChannelURL
	session   *session
	byUser    bool
	dirParser *parsing.DirectoryParser

	ctx            context.Context         // Canceled by Cancel, Abort and shutdown.
	cancel         context.CancelCauseFunc // Cancels ctx.
	preDownloadCtx context.Context         // Also canceled by Stop, which only reaches videos not yet downloading.
	stop           context.CancelCauseFunc // Cancels preDownloadCtx.

	downloading bool // Its download has begun, so Stop lets it finish.
	done        bool // Finished, and only still shown on the Downloads page.
}

// sessionKey identifies the channel URL, any login, and the host that videos in one request download through.
type sessionKey struct {
	channelURLID int64
	auth         models.ManualAuth
	host         string
}

// session is what the videos of one request share when they download through the same channel URL and login, from one host.
type session struct {
	once       sync.Once
	cu         *models.ChannelURL
	videoURL   string // Its first video's URL, whose host its cookies are for.
	scraper    *scraper.Scraper
	skipMetarr models.SkipMetarr
	err        error
}

// NewDownloadQueue returns an empty download queue, handing its downloaded videos to postProcessing.
func NewDownloadQueue(ctx context.Context, s contracts.Store, postProcessing *PostProcessingQueue) *DownloadQueue {
	return &DownloadQueue{
		ctx:            ctx,
		store:          s,
		postProcessing: postProcessing,
		downloads:      make(map[int64]*download),
		urls:           make(map[string]int64),
		stopping:       make(map[int64]bool),
		running:        make(map[int64]int),
		newVideos:      make(map[int64][]string),
		changed:        make(chan struct{}),
	}
}

// Add claims each requested video in the database and starts its download, returning the videos it queued.
//
// Videos already queued or downloading, for this channel or another, are left out.
func (q *DownloadQueue) Add(c *models.Channel, reqs ...Request) (queued []*models.Video, err error) {
	if c == nil {
		return nil, errors.New("channel cannot be nil")
	}
	vs := q.store.VideoStore()

	// Load the channel's config file settings, holding back its videos if they can't be used.
	file.UpdateFromConfigFile(q.store.ChannelStore(), c)
	if c.LoadErr != nil {
		return nil, c.LoadErr
	}

	// Its videos wait for slots under its current limits, in both queues.
	q.SetLimits(c)
	q.postProcessing.SetLimits(c)

	// Videos sharing a channel URL, login and host share their cookies.
	sessions := make(map[sessionKey]*session)

	var errs []error
	for _, r := range reqs {
		v := r.Video
		if v == nil {
			errs = append(errs, errors.New("a download request has no video"))
			continue
		}
		v.ChannelName = c.Name // So the video's log lines are tagged with the channel it downloads for.
		name := cmp.Or(v.URL, "ID "+strconv.FormatInt(v.ID, 10))

		// A URL the channel may not have yet is stored with the channel URL it downloads through.
		if v.ID == 0 {
			cu, err := q.channelURL(c, v.ChannelURLID)
			if err != nil {
				errs = append(errs, fmt.Errorf("video %s: %w", name, err))
				continue
			}
			v.ChannelURLID = cu.ID
			v.ChannelURL = cu.URL
		}

		// Claim the video, so nothing else downloads it.
		claimed, err := vs.QueueVideo(c.ID, v, r.ByUser)
		if err != nil {
			errs = append(errs, fmt.Errorf("video %s: %w", name, err))
			continue
		}
		if !claimed {
			c.Log().I("Video %s is already queued or downloading, or no longer available, skipping", name)
			continue
		}

		// A claimed video which can't be queued fails, so it returns to the Available list.
		d, err := q.newDownload(c, r, sessions)
		switch {
		case err != nil:
			// Exit switch (will log error)
		case d == nil:
			// Nil guard.
			err = fmt.Errorf("video %q: newDownload returned nil without an error", v.URL)
		default:
			takenBy, inserted := q.insert(d)
			if inserted {
				// Begin download.
				queued = append(queued, v)
				go q.run(d)
				continue
			}
			d.cancel(nil)

			// Claimed again while its own download is between attempts, which carries on.
			if takenBy == v.ID {
				c.Log().I("Video %s is already queued or downloading, skipping", name)
				continue
			}
			err = fmt.Errorf("video %q is already being downloaded for another channel", v.URL)
		}
		c.Log().E("Not downloading video %q: %v", v.URL, err)
		q.storeStatus(v, consts.DLStatusFailed, 0, err)
		errs = append(errs, err)
	}
	return queued, errors.Join(errs...)
}

// SetLimits applies a channel's download concurrency to its videos in the queue, waiting ones included.
func (q *DownloadQueue) SetLimits(c *models.Channel) {
	q.lanes.setLimits(c.ID, downloadLimits(c))
}

// Cancel cancels a video's download, waiting or running, returning false if it isn't in the queue.
func (q *DownloadQueue) Cancel(videoID int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.downloads[videoID]
	if !ok || d.done {
		return false
	}
	d.c.Log().I("Canceling download of video %q", d.video.URL)
	d.cancel(downloads.ErrCanceledByUser)
	return true
}

// Stop cancels the channel's videos that haven't started downloading, letting those downloading finish.
//
// Returns false if the channel has no videos in the queue.
func (q *DownloadQueue) Stop(channelID int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	found := false
	for _, d := range q.downloads {
		if d.c.ID != channelID || d.done {
			continue
		}
		found = true
		if !d.downloading {
			d.stop(errStopped)
		}
	}
	if found {
		q.stopping[channelID] = true
	}
	return found
}

// Abort cancels all of the channel's videos in the queue, downloading ones too.
//
// Returns false if the channel has no videos in the queue.
func (q *DownloadQueue) Abort(channelID int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	found := false
	for _, d := range q.downloads {
		if d.c.ID == channelID && !d.done {
			found = true
			d.cancel(errAborted)
		}
	}
	if found {
		q.stopping[channelID] = true
	}
	return found
}

// Entries returns the videos in the queue, oldest first, with their download status.
func (q *DownloadQueue) Entries() []Entry {
	q.mu.Lock()
	entries := make([]Entry, 0, len(q.downloads))
	for _, d := range q.downloads {
		e := d.entry
		status := d.status
		if status.Error != nil {
			status.ErrorMsg = status.Error.Error()
		}
		e.DownloadStatus = &status
		entries = append(entries, e)
	}
	q.mu.Unlock()

	slices.SortFunc(entries, func(a, b Entry) int { return a.QueuedAt.Compare(b.QueuedAt) })
	return entries
}

// Channels returns the IDs of channels with videos in the queue, and of those being stopped.
func (q *DownloadQueue) Channels() (active, stopping []int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	active = []int64{}
	stopping = make([]int64, 0, len(q.stopping))
	for _, d := range q.downloads {
		id := d.c.ID
		if d.done || q.stopping[id] || slices.Contains(active, id) {
			continue
		}
		active = append(active, id)
	}
	for id := range q.stopping {
		stopping = append(stopping, id)
	}
	return active, stopping
}

// Len returns how many videos are in the queue.
func (q *DownloadQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, d := range q.downloads {
		if !d.done {
			n++
		}
	}
	return n
}

// Wait waits until the queue is empty, returning false if ctx ends first.
func (q *DownloadQueue) Wait(ctx context.Context) bool {
	for {
		q.mu.Lock()
		changed := q.changed
		q.mu.Unlock()
		if q.Len() == 0 {
			return true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

// newDownload returns the queue's download of a claimed video, through the session for its channel URL and login.
func (q *DownloadQueue) newDownload(c *models.Channel, r Request, sessions map[sessionKey]*session) (*download, error) {
	v := r.Video

	// Download As options replace the video's stored ones.
	if r.ReplaceOverrides {
		if err := q.store.VideoStore().SetVideoOverrides([]int64{v.ID}, r.Overrides); err != nil {
			return nil, err
		}
	}

	cu, err := q.channelURL(c, v.ChannelURLID)
	if err != nil {
		return nil, err
	}

	// Videos sharing a channel URL, login and host share a session.
	host := network.Hostname(v.URL)
	key := sessionKey{channelURLID: cu.ID, host: host}
	if r.Auth != nil {
		key.auth = *r.Auth
	}
	s := sessions[key]
	if s == nil {
		sessionCU := *cu // A copy, so the session's login and cookies aren't kept on the channel's model.
		if r.Auth != nil {
			sessionCU.Login.Username = r.Auth.Username
			sessionCU.Login.Password = r.Auth.Password
			sessionCU.Login.LoginURL = r.Auth.LoginURL
		}

		// A crawl's cookies are for the channel URL's own host, so videos on another host fetch their own.
		if host != network.Hostname(cu.URL) {
			sessionCU.Session = models.ChannelURLSession{}
		}
		s = &session{cu: &sessionCU, videoURL: v.URL, scraper: scraper.New()}
		sessions[key] = s
	}

	ctx, cancel := context.WithCancelCause(q.ctx)
	preDownloadCtx, stop := context.WithCancelCause(ctx)
	d := &download{
		q:              q,
		entry:          *newQueueEntry(v, cu),
		status:         models.DLStatus{Status: consts.DLStatusQueued, Stage: "Waiting to start"},
		video:          v,
		c:              c,
		cu:             s.cu,
		session:        s,
		byUser:         r.ByUser,
		ctx:            ctx,
		cancel:         cancel,
		preDownloadCtx: preDownloadCtx,
		stop:           stop,
	}
	return d, nil
}

// channelURL returns the channel URL a video downloads through: its own, or the channel's manual downloads URL if it has
// none or it was since removed.
func (q *DownloadQueue) channelURL(c *models.Channel, channelURLID int64) (*models.ChannelURL, error) {
	if c.URLByID(channelURLID) == nil {
		channelURLID = 0
	}
	return ManualChannelURL(q.store.ChannelStore(), c, channelURLID)
}

// insert puts a download in the queue, or returns the ID of the video already downloading its URL.
func (q *DownloadQueue) insert(d *download) (takenBy int64, inserted bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if id, taken := q.urls[d.video.URL]; taken {
		return id, false
	}
	q.downloads[d.video.ID] = d
	q.urls[d.video.URL] = d.video.ID
	q.running[d.c.ID]++
	return 0, true
}

// finish stores the video's final download status and takes it out of the queue.
func (q *DownloadQueue) finish(d *download, err error) {
	status, err := q.finalStatus(d, err)
	if err != nil {
		d.c.Log().E("Video %q not downloaded: %v", d.video.URL, err)
	}
	d.SetStatus(d.video, status, d.video.DownloadStatus.Percent, "", err)

	q.mu.Lock()
	if q.urls[d.video.URL] == d.video.ID {
		delete(q.urls, d.video.URL)
	}
	d.done = true

	// Filtered out videos are kept briefly, so the Downloads page shows they were filtered out.
	if status == consts.DLStatusIgnored {
		time.AfterFunc(filteredDisplayTime, func() { q.remove(d) })
	} else if q.downloads[d.video.ID] == d {
		delete(q.downloads, d.video.ID)
	}

	if !q.hasChannel(d.c.ID) {
		delete(q.stopping, d.c.ID)
	}

	close(q.changed)
	q.changed = make(chan struct{})
	q.mu.Unlock()

	d.cancel(nil)
}

// addNewVideo records that the download's channel URL got a new video, for its channel's notification services.
func (q *DownloadQueue) addNewVideo(d *download) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !slices.Contains(q.newVideos[d.c.ID], d.cu.URL) {
		q.newVideos[d.c.ID] = append(q.newVideos[d.c.ID], d.cu.URL)
	}
}

// notifyIfLast ends one of the channel's running videos, telling its notification services about its new videos once none are left.
func (q *DownloadQueue) notifyIfLast(c *models.Channel) {
	q.mu.Lock()
	q.running[c.ID]--
	if q.running[c.ID] > 0 {
		q.mu.Unlock()
		return
	}
	delete(q.running, c.ID)
	urls := q.newVideos[c.ID]
	delete(q.newVideos, c.ID)
	q.mu.Unlock()

	// Tell the services.
	if len(urls) == 0 {
		return
	}
	if err := notify.Services(q.store.ChannelStore(), c, urls); err != nil {
		c.Log().E("Failed to notify services for channel %q: %v", c.Name, err)
	}
}

// finalStatus returns the status a video leaves the queue with, and the error stored with it.
func (q *DownloadQueue) finalStatus(d *download, err error) (consts.DownloadStatus, error) {
	status := d.video.DownloadStatus.Status
	cause := context.Cause(d.ctx)
	switch {
	case status == consts.DLStatusCompleted || status == consts.DLStatusIgnored:
		return status, nil
	case errors.Is(cause, downloads.ErrCanceledByUser):
		return consts.DLStatusCanceled, nil
	case q.ctx.Err() != nil:
		return consts.DLStatusFailed, cmp.Or(err, q.ctx.Err())
	case !d.downloading && d.preDownloadCtx.Err() != nil:
		// Stopped before its download began, so it returns to the Available list as if never reached.
		if d.byUser {
			return consts.DLStatusCanceled, nil
		}
		return consts.DLStatusFailed, errStopped
	case errors.Is(cause, errAborted):
		return consts.DLStatusCanceled, nil
	case err == nil:
		return consts.DLStatusFailed, errors.New("skipped before downloading, see the Tubarr log for why")
	default:
		return consts.DLStatusFailed, err
	}
}

// startDownloading marks a video as downloading unless it was stopped first. Stop lets it finish after this.
func (q *DownloadQueue) startDownloading(d *download) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if d.preDownloadCtx.Err() != nil {
		return false
	}
	d.downloading = true
	return true
}

// remove takes a finished video off the Downloads page.
func (q *DownloadQueue) remove(d *download) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.downloads[d.video.ID] == d {
		delete(q.downloads, d.video.ID)
	}
}

// hasChannel reports whether the channel has videos in the queue. Must hold q.mu.
func (q *DownloadQueue) hasChannel(channelID int64) bool {
	for _, d := range q.downloads {
		if d.c.ID == channelID && !d.done {
			return true
		}
	}
	return false
}

// storeStatus stores a video's download status.
//
//nolint:contextcheck // Downloads outlive the request that queued them, so they use the queue's own context.
func (q *DownloadQueue) storeStatus(v *models.Video, status consts.DownloadStatus, pct float64, err error) {
	update := models.StatusUpdate{VideoID: v.ID, VideoURL: v.URL, Status: status, Percent: pct, Error: err}

	// Stored even while shutting down, so the video's final status isn't lost.
	if err := q.store.DownloadStore().UpdateDownloadStatus(context.WithoutCancel(q.ctx), update); err != nil {
		v.Log().E("Failed to store download status for video %q: %v", v.URL, err)
	}
}

// SetStatus sets the video's download status, then shows and stores it.
func (d *download) SetStatus(v *models.Video, status consts.DownloadStatus, pct float64, stage string, err error) {
	v.DownloadStatus = models.DLStatus{Status: status, Percent: pct, Stage: stage, Error: err}
	d.SendUpdate(v)
}

// SendUpdate shows the video's download status on the Downloads page, storing it when the status changes.
func (d *download) SendUpdate(v *models.Video) {
	s := v.DownloadStatus
	s.Percent = min(s.Percent, 100)
	downloads.TrackETA(v.ID, s.Status, s.Percent, time.Now())

	d.q.mu.Lock()
	prev := d.status

	// A new attempt, or progress since a failed one, clears its error.
	if s.Status == consts.DLStatusQueued || (s.Status == consts.DLStatusDownloading && prev.Status != consts.DLStatusDownloading) {
		s.Error = nil
	}
	if s.Percent > prev.Percent && prev.Error != nil {
		s.Error = nil
	}
	d.status = s
	if v.Metadata.Title != "" {
		d.entry.Title = v.Metadata.Title
	}
	d.q.mu.Unlock()

	// Status changes are logged at info level, progress only when debugging.
	const statusFmt = "Status update for video with URL %q:\n\nStatus: %s\nPercentage: %.1f\nError: %v\n"
	if s.Status != prev.Status {
		d.c.Log().I(statusFmt, v.URL, s.Status, s.Percent, s.Error)
	} else if s.Percent != prev.Percent || s.Stage != prev.Stage {
		d.c.Log().D(2, statusFmt, v.URL, s.Status, s.Percent, s.Error)
	}

	// Status changes are stored, as is reaching 100%.
	if s.Status != prev.Status || (s.Percent == 100 && prev.Percent != 100) {
		d.q.storeStatus(v, s.Status, s.Percent, s.Error)
	}
}

// ready fetches the channel URL's cookies and checks whether Metarr can run for its videos, once for all of them.
func (s *session) ready(ctx context.Context, cs contracts.ChannelStore, c *models.Channel) error {
	s.once.Do(func() {
		// Cookies a crawl already fetched are reused.
		if s.cu.Session.CookiePath == "" {
			s.cu.Session.Cookies, s.cu.Session.CookiePath, s.err = s.scraper.ScraperURLCookies(ctx, cs, c, s.cu, s.videoURL)
		}

		// Metarr is held back if it can't run or an ops file can't be used, leaving videos pending for Run Metarr.
		var skipErr error
		s.skipMetarr, skipErr = metarr.SkipReason(metarr.CheckInstalled(ctx), s.cu, parsing.NewDirectoryParser(c, s.cu, parsing.TubarrTags))
		if skipErr != nil {
			c.Log().E("Not running Metarr for %q (videos stay pending for Run Metarr): %s: %v", s.cu.URL, s.skipMetarr.Reason, skipErr)
		}

		// Turned off, videos stay pending until the user post-processes them.
		if m := s.cu.ChanURLMetarrArgs; !s.skipMetarr.Skip && m != nil && m.AutoPostProcess != nil && !*m.AutoPostProcess {
			s.skipMetarr = models.SkipMetarr{Skip: true, Reason: models.SkipMetarrAutoPostProcessOff}
		}
	})
	return s.err
}
