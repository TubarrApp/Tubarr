package downloads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"tubarr/internal/blocking"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedfiles"
)

// verifyJSONDownload verifies the JSON file downloaded and contains valid JSON data.
func verifyJSONDownload(jsonPath string) error {
	j, err := sharedfiles.OpenRegularInRoot(jsonPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := j.Close(); closeErr != nil {
			logger.Pl.E("Failed to close JSON file %q: %v", jsonPath, closeErr)
		}
	}()

	jsonData, err := io.ReadAll(j)
	if err != nil {
		return fmt.Errorf("failed to read JSON file: %w", err)
	}

	if !json.Valid(jsonData) {
		return fmt.Errorf("invalid JSON content in file: %s", jsonPath)
	}
	return nil
}

// verifyVideoDownload checks if the specified video file exists and is not empty.
func verifyVideoDownload(videoPath string) error {
	// Check video file.
	videoInfo, err := os.Stat(videoPath)
	if err != nil {
		return fmt.Errorf("video file verification failed: %w", err)
	}
	if videoInfo.Size() == 0 {
		return fmt.Errorf("video file is empty: %s", videoPath)
	}
	if videoInfo.IsDir() {
		return errors.New("video path created is a directory")
	}

	return nil
}

// waitForFile waits until the file is ready in the file system.
func (d *VideoDownload) waitForFile(filepath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {

		if _, err := os.Stat(filepath); err == nil { // err IS nil
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("unexpected error while checking file: %w", err)
		}

		select {
		case <-d.Context.Done():
			return d.Context.Err()
		case <-time.After(consts.Interval100ms):
		}
	}
	return fmt.Errorf("file not ready or empty after %v: %s", timeout, filepath)
}

// cancelVideoDownload cancels the download, typically due to user input.
func (d *VideoDownload) cancelVideoDownload() error {
	d.cleanup()
	if errors.Is(context.Cause(d.Context), ErrCanceledByUser) {
		removePartialFiles(d.Video.VideoDir, d.destinations)
	}
	d.DLTracker.SetStatus(d.Video, consts.DLStatusCanceled, d.Video.DownloadStatus.Percent, "", d.Context.Err())
	return fmt.Errorf("user canceled video download for %s: %w", d.Video.URL, d.Context.Err())
}

// removePartialFiles removes the .part and .ytdl files yt-dlp left for each destination it reported, if inside dir.
//
// Only these exact paths are removed, so other downloads and files in the directory are never touched.
func removePartialFiles(dir string, destinations []string) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		logger.Pl.W("Not removing partial downloads, failed to resolve directory %q: %v", dir, err)
		return
	}
	for _, dest := range destinations {
		absDest, err := filepath.Abs(dest)
		if err != nil {
			logger.Pl.W("Not removing partial download for %q: %v", dest, err)
			continue
		}
		rel, err := filepath.Rel(absDir, absDest)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			logger.Pl.W("Not removing partial download for %q, it's outside the video directory %q", dest, dir)
			continue
		}
		for _, p := range []string{absDest + ".part", absDest + ".ytdl"} {
			if err := os.Remove(p); err == nil {
				logger.Pl.I("Removed partial download %q", p)
			} else if !errors.Is(err, fs.ErrNotExist) {
				logger.Pl.W("Failed to remove partial download %q: %v", p, err)
			}
		}
	}
}

// cancelJSONDownload cancels the download and cleans up resources.
func (d *JSONDownload) cancelJSONDownload() error {
	d.cleanup()
	return fmt.Errorf("user canceled JSON download for %s: %w", d.Video.URL, d.Context.Err())
}

// checkBotDetection checks for detection of bot activity, blocking the video's domain if found.
func checkBotDetection(v *models.Video, cu *models.ChannelURL, db *sql.DB, inputErr error) error {
	if inputErr == nil {
		return nil
	}
	if v == nil || cu == nil {
		return fmt.Errorf("video or channel URL model is nil when checking bot detection")
	}

	lowerErr := strings.ToLower(inputErr.Error())
	if !strings.Contains(lowerErr, "confirm you’re not a bot") && // Curly apostrophe (used by some tube sites)
		!strings.Contains(lowerErr, "confirm you're not a bot") && // Straight apostrophe
		!strings.Contains(lowerErr, "not a robot") {
		return nil
	}

	// Avoid using the 'ChannelURL' URL, which can be the manual download value.
	host := hostname(v.URL)
	switch {
	case host == "":
		logger.Pl.E("Failed to parse URL %q after bot detection", v.URL)
	case db == nil:
		logger.Pl.E("Cannot record bot detection for %q: database is nil", host)
	default:
		blockContext := blocking.GetBlockContext(cu)
		if err := blocking.BlockDomain(db, host, blockContext); err != nil {
			logger.Pl.E("Failed to block %q after bot detection: %v", host, err)
		} else if err := blocking.AddBlockedVideo(db, host, blockContext, v.ID, v.ChannelID); err != nil {
			logger.Pl.W("Failed to record blocked video %d: %v", v.ID, err)
		}
	}

	return fmt.Errorf("url %q %s. Aborting without retries. Error message: %w", v.URL, consts.BotActivitySentinel, inputErr)
}

// CheckIfAvoidURL checks if the hostname of the URL is blocked for the channel URL's context.
func CheckIfAvoidURL(v *models.Video, cu *models.ChannelURL, db *sql.DB) error {
	if v == nil || cu == nil || db == nil {
		return nil
	}

	siteHost := hostname(v.URL)
	if siteHost == "" {
		logger.Pl.E("Could not parse URL %q, will check against full URL instead", v.URL)
		siteHost = v.URL
	}

	blockCtx := blocking.GetBlockContext(cu)
	if isBlocked, _, remaining := blocking.IsBlocked(siteHost, blockCtx); isBlocked {
		if err := blocking.AddBlockedVideo(db, siteHost, blockCtx, v.ID, v.ChannelID); err != nil {
			logger.Pl.W("Failed to record blocked video %d: %v", v.ID, err)
		}
		return fmt.Errorf("%s is blocked for %s requests until %s", siteHost, blockCtx, time.Now().Add(remaining).Format("2006-01-02 15:04 MST"))
	}
	return nil
}

// hostname returns a URL's hostname, or "" if it can't be parsed.
func hostname(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// buildVideoCodecList builds the codec preference string for yt-dlp.
func buildVideoCodecList(codecs []string) (arg string) {
	if len(codecs) == 0 {
		return ""
	}

	// Add video codecs in order.
	arg = "("
	for _, c := range codecs {
		arg += "bv*[vcodec~='" + ytdlpCodecRegex(c) + "']/"
	}
	arg += "bv*)"

	return arg
}

// buildAudioCodecList builds the codec preference string for yt-dlp.
func buildAudioCodecList(codecs []string) (arg string) {
	if len(codecs) == 0 {
		return ""
	}

	// Add audio codecs in order.
	arg = "("
	for _, c := range codecs {
		arg += "ba[acodec~='" + ytdlpCodecRegex(c) + "']/"
	}
	arg += "ba)"

	return arg
}

// ytdlpCodecRegex returns a regex matching the codec strings yt-dlp reports for a codec, which sites give under other
// names (e.g. "avc1.640028" for h264, "hvc1" or "hev1" for hevc). The patterns follow yt-dlp's own format sorting.
func ytdlpCodecRegex(codec string) string {
	if c, exists := consts.YtdlpCodecRegex[codec]; exists {
		return c
	}
	return codec
}

// refreshFlareSolverr solves through FlareSolverr again if Cloudflare blocked a download for a FlareSolverr site,
// so the next attempt uses fresh cookies. Reports whether it tried.
func refreshFlareSolverr(cu *models.ChannelURL, videoURL string, fsGen int, dlErr error) bool {
	fs := cu.FlareSolverr
	if fs == nil || !models.IsCloudflareBlock(dlErr.Error()) {
		return false
	}

	logger.Pl.W("Cloudflare blocked the download of %q, refreshing its FlareSolverr solve...", videoURL)
	if err := fs.Refresh(fsGen); err != nil {
		logger.Pl.E("FlareSolverr solve failed for %q: %v", videoURL, err)
	}
	return true
}
