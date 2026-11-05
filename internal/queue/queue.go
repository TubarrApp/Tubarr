package queue

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"tubarr/internal/blocking"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/limits"
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

// newEntry returns a video's entry, queued now.
func newEntry(v *models.Video, cu *models.ChannelURL, c *models.Channel) *Entry {
	return &Entry{
		VideoID:      v.ID,
		ChannelID:    c.ID,
		ChannelName:  c.Name,
		ChannelURLID: cu.ID,
		ChannelURL:   cu.URL,
		URL:          v.URL,
		Title:        v.Title,
		ThumbnailURL: v.ThumbnailURL,
		QueuedAt:     time.Now(),
	}
}

// lanes holds the slots of each channel, and of each channel URL with a limit of its own.
type lanes struct {
	mu   sync.Mutex
	sems map[laneKey]*limits.Semaphore
}

// laneKey identifies a channel's lane, or a channel URL's if it has a limit of its own.
type laneKey struct {
	channelURL bool
	id         int64
}

// lane returns the lane of a channel URL whose channel and URL have the given limits, or nil if neither has one.
//
// A URL whose limit is the channel's shares the channel's lane.
func (l *lanes) lane(c *models.Channel, cu *models.ChannelURL, chanLimit, urlLimit int) *limits.Semaphore {
	// Default: Set Channel limit from Channel settings.
	key := laneKey{false, c.ID}
	limit := chanLimit

	// ChannelURL concurrency limit overrides Channel's setting.
	if urlLimit > 0 && urlLimit != chanLimit {
		key = laneKey{true, cu.ID}
		limit = urlLimit
	}

	// No limit.
	if limit <= 0 {
		return nil
	}

	// Lock lanes from modification.
	l.mu.Lock()
	defer l.mu.Unlock()

	// Create semaphore map if not yet present.
	if l.sems == nil {
		l.sems = make(map[laneKey]*limits.Semaphore)
	}

	// Add semaphore to map for this key (or retrieve existing from map).
	sem, exists := l.sems[key]
	if !exists {
		sem = limits.NewSemaphore(limit)
		l.sems[key] = sem
	}

	// Set semaphore concurrency limit.
	sem.SetLimit(limit)

	return sem
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
