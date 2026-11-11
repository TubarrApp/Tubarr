package queue

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"tubarr/internal/blocking"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"
)

// Entry is one video in a queue, as its page shows it.
type Entry struct {
	VideoID        int64            `json:"video_id"`
	ChannelID      int64            `json:"channel_id"`
	ChannelName    string           `json:"channel_name"`
	ChannelURLID   int64            `json:"channel_url_id"`
	ChannelURL     string           `json:"channel_url"`
	URL            string           `json:"url"`
	Title          string           `json:"title"`
	ThumbnailURL   string           `json:"thumbnail_url,omitempty"`
	QueuedAt       time.Time        `json:"queued_at"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	Progress       *metarr.Progress `json:"progress,omitempty"`        // While FFmpeg transcodes.
	DownloadStatus *models.DLStatus `json:"download_status,omitempty"` // While in the download queue.
}

// newQueueEntry returns a video's entry, queued now.
func newQueueEntry(v *models.Video, cu *models.ChannelURL) *Entry {
	return &Entry{
		VideoID:      v.ID,
		ChannelID:    cu.ChannelID,
		ChannelName:  cu.ChannelName,
		ChannelURLID: cu.ID,
		ChannelURL:   cu.URL,
		URL:          v.URL,
		Title:        v.Metadata.Title,
		ThumbnailURL: v.Metadata.ThumbnailURL,
		QueuedAt:     time.Now(),
	}
}

// lanes holds the slots of each channel, and of each channel URL with a limit of its own.
//
// Videos are given slots in the order they asked. Each video counts in the lane its channel's latest limits put it in,
// so a changed limit applies to videos already running or waiting.
type lanes struct {
	mu      sync.Mutex
	limits  map[int64]*laneLimits // By channel ID.
	held    map[laneURL]int       // Slots held by each channel URL's videos.
	waiting []*laneWaiter
}

// laneKey identifies a channel's lane, or a channel URL's if it has a limit of its own.
type laneKey struct {
	channelURL bool
	id         int64
}

// laneURL identifies a channel URL whose videos take slots.
type laneURL struct {
	channelID    int64
	channelURLID int64
}

// laneLimits is a channel's limit (0 or less for none) and its URLs' limits, from a model of it loaded at loadedAt.
type laneLimits struct {
	loadedAt time.Time
	channel  int
	urls     map[int64]int // By channel URL ID, 0 for a URL whose limit is the channel's.
}

// laneWaiter is a video waiting for a slot.
type laneWaiter struct {
	url   laneURL
	ready chan struct{} // Closed once it has a slot.
}

// newLaneLimits returns a channel's lane limits holding only its own limit, for setURL to add its URLs'.
func newLaneLimits(c *models.Channel, channelLimit int) laneLimits {
	return laneLimits{loadedAt: c.LoadedAt, channel: channelLimit, urls: make(map[int64]int, len(c.URLModels))}
}

// setURL sets a channel URL's limit, stored as 0 when it's the channel's so the URL shares the channel's lane.
func (lim *laneLimits) setURL(id int64, limit int) {
	if limit == lim.channel {
		limit = 0
	}
	lim.urls[id] = limit
}

// setLimits applies a channel's limits unless newer ones are already applied, giving waiting videos any slots they free.
func (l *lanes) setLimits(channelID int64, lim laneLimits) {
	// Lock lanes from modification.
	l.mu.Lock()
	defer l.mu.Unlock()

	// Keep the limits of the newest model.
	stored := l.limits[channelID]
	if stored != nil && lim.loadedAt.Before(stored.loadedAt) {
		return
	}

	// First limits for this channel.
	if stored == nil {
		if l.limits == nil {
			l.limits = make(map[int64]*laneLimits)
		}
		stored = &laneLimits{urls: make(map[int64]int, len(lim.urls))}
		l.limits[channelID] = stored
	}

	// Store the new limits, URLs they leave out (as a crawl's channel does) keeping theirs, then hand out freed slots.
	stored.loadedAt = lim.loadedAt
	stored.channel = lim.channel
	maps.Copy(stored.urls, lim.urls)
	l.grant()
}

// acquire waits for a slot in the lane of a channel URL's videos, or for ctx to end.
func (l *lanes) acquire(ctx context.Context, channelID, channelURLID int64) error {
	// Join the end of the line, taking a slot straight away if one is free.
	w := &laneWaiter{url: laneURL{channelID, channelURLID}, ready: make(chan struct{})}
	l.mu.Lock()
	l.waiting = append(l.waiting, w)
	l.grant()
	l.mu.Unlock()

	// Wait for a slot.
	select {
	case <-w.ready:
		return nil
	case <-ctx.Done():
		l.mu.Lock()
		defer l.mu.Unlock()
		select {
		case <-w.ready: // Given a slot as ctx ended, so it goes to the next video.
			l.free(w.url)
		default:
			if i := slices.Index(l.waiting, w); i >= 0 {
				l.waiting = slices.Delete(l.waiting, i, i+1)
			}
		}
		return ctx.Err()
	}
}

// release frees a slot acquire gave to one of a channel URL's videos.
func (l *lanes) release(channelID, channelURLID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.free(laneURL{channelID, channelURLID})
}

// free frees a slot held by one of u's videos for the next waiting video. Must hold l.mu.
func (l *lanes) free(u laneURL) {
	l.held[u]--
	if l.held[u] <= 0 {
		delete(l.held, u)
	}
	l.grant()
}

// grant gives free slots to the longest waiting videos. Must hold l.mu.
func (l *lanes) grant() {
	if l.held == nil {
		l.held = make(map[laneURL]int)
	}
	if len(l.waiting) == 0 {
		return
	}

	// Count the slots in use in each lane, putting each video in the lane the limits put it in now.
	inUse := make(map[laneKey]int, len(l.held))
	for u, n := range l.held {
		key, _ := l.lane(u)
		inUse[key] += n
	}

	// Give each waiting video a slot if its lane has one free, oldest first.
	waiting := l.waiting[:0]
	for _, w := range l.waiting {
		key, limit := l.lane(w.url)
		if limit > 0 && inUse[key] >= limit {
			waiting = append(waiting, w)
			continue
		}
		inUse[key]++
		l.held[w.url]++
		close(w.ready)
	}
	clear(l.waiting[len(waiting):])
	l.waiting = waiting
}

// lane returns the lane of a channel URL's videos and its limit (0 or less for none). Must hold l.mu.
//
// A URL whose limit is the channel's shares the channel's lane.
func (l *lanes) lane(u laneURL) (key laneKey, concurrencyLimit int) {
	lim := l.limits[u.channelID]
	if lim == nil {
		return laneKey{false, u.channelID}, 0
	}
	if limit := lim.urls[u.channelURLID]; limit > 0 {
		return laneKey{true, u.channelURLID}, limit
	}
	return laneKey{false, u.channelID}, lim.channel
}

// ManualChannelURL returns the channel's URL with the given ID, or its manual downloads URL for 0. A video whose channel
// URL was removed has ID 0, so it's treated as a manual download.
func ManualChannelURL(cs contracts.ChannelStore, c *models.Channel, id int64) (*models.ChannelURL, error) {
	if id == 0 {
		return ensureManualDownloadsChannelURL(cs, c)
	}
	if cu := c.URLByID(id); cu != nil {
		return cu, nil
	}

	// Sent invalid URL ID for given channel.
	return nil, fmt.Errorf("channel URL ID %d doesn't exist for channel %q", id, c.Name)
}

// ensureManualDownloadsChannelURL ensures a special 'manual-downloads' ChannelURL exists for the channel.
func ensureManualDownloadsChannelURL(cs contracts.ChannelStore, c *models.Channel) (*models.ChannelURL, error) {
	// First check in-memory models.
	for _, cu := range c.URLModels {
		if cu.IsManual {
			return cu, nil // Return existing model.
		}
	}

	// If not found in memory, check database directly.
	existingCU, hasRows, err := cs.GetChannelURLModel(c.ID, consts.ManualDownloadsCol, true)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing manual downloads URL: %w", err)
	}
	if hasRows && existingCU != nil {
		c.URLModels = append(c.URLModels, existingCU)
		return existingCU, nil
	}

	// Get channel to inherit settings.
	manualChanURL := &models.ChannelURL{
		URL: consts.ManualDownloadsCol,
	}

	// Create it if it doesn't exist.
	if _, err = cs.AddChannelURL(c.ID, manualChanURL, true); err != nil {
		return nil, fmt.Errorf("failed to create manual downloads channel URL: %w", err)
	}

	// Fetch the newly created ChannelURL with merged parent settings.
	fetchedCU, hasRows, err := cs.GetChannelURLModel(c.ID, consts.ManualDownloadsCol, true)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch newly created manual downloads URL: %w", err)
	}
	if !hasRows || fetchedCU == nil {
		return nil, errors.New("newly created manual downloads URL not found in database")
	}

	// Add to in-memory models.
	c.URLModels = append(c.URLModels, fetchedCU)

	return fetchedCU, nil
}

// DomainBlocked returns why the channel URL's domain is blocked for its authentication context, or nil if it isn't.
func DomainBlocked(cu *models.ChannelURL) error {
	parsed, err := url.Parse(cu.URL)
	if err != nil {
		return fmt.Errorf("unable to parse %q: %w", cu.URL, err)
	}
	hostname := parsed.Hostname()
	blockCtx := blocking.GetBlockContext(cu)
	if isBlocked, _, remaining := blocking.IsBlocked(hostname, blockCtx); isBlocked {
		return fmt.Errorf("%s is blocked for %s requests until %s", hostname, blockCtx, time.Now().Add(remaining).Format("2006-01-02 15:04 MST"))
	}
	return nil
}

// CheckDestinations returns an error if any of a channel URL's download destinations (or a video's overrides) isn't available.
func CheckDestinations(c *models.Channel, cu *models.ChannelURL, o *models.VideoOverrides) error {
	s := cmp.Or(cu.ChanURLSettings, c.ChanSettings)
	var outputDirs []string
	if s != nil {
		videoDir, jsonDir := s.VideoDir, s.JSONDir
		if o != nil {
			videoDir, jsonDir = cmp.Or(o.VideoDir, videoDir), cmp.Or(o.JSONDir, jsonDir)
		}
		for _, dir := range []string{videoDir, jsonDir} {
			if err := validation.CheckDestination(dir); err != nil {
				return err
			}
		}
	}
	// Metarr's output directory, in the order metarr.parseMetarrOutputDir picks it.
	switch {
	case o != nil && o.OutputDir != "":
		outputDirs = append(outputDirs, o.OutputDir)
	default:
		if s != nil {
			for _, op := range s.MetaFilterMoveOps {
				outputDirs = append(outputDirs, op.OutputDir)
			}
		}
		if m := cmp.Or(cu.ChanURLMetarrArgs, c.ChanMetarrArgs); m != nil {
			outputDirs = append(outputDirs, m.OutputDir)
		}
	}

	// Metarr creates a folder inside a video or output directory when needed, so only that directory has to be available.
	parents := videoAndOutputDirs(c, cu)
	for _, dir := range outputDirs {
		filled, _ := parsing.FillOutputDirectoryTags(dir, cu, c)
		if err := validation.CheckDestination(outermostParent(filled, parents)); err != nil {
			return err
		}
	}
	return nil
}

// videoAndOutputDirs returns the channel's and channel URL's video and Metarr output directories.
func videoAndOutputDirs(c *models.Channel, cu *models.ChannelURL) []string {
	var dirs []string
	for _, s := range []*models.Settings{c.ChanSettings, cu.ChanURLSettings} {
		if s != nil {
			dirs = append(dirs, s.VideoDir)
		}
	}
	for _, m := range []*models.MetarrArgs{c.ChanMetarrArgs, cu.ChanURLMetarrArgs} {
		if m != nil {
			dirs = append(dirs, m.OutputDir)
		}
	}
	return dirs
}

// outermostParent returns the shortest of parents that contains dir, or dir if none do.
func outermostParent(dir string, parents []string) string {
	for _, parent := range parents {
		if parent != "" && strings.HasPrefix(filepath.Clean(dir), filepath.Clean(parent)+string(filepath.Separator)) {
			dir = parent
		}
	}
	return dir
}
