package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/blocking"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/downloads"
	"tubarr/internal/file"
	"tubarr/internal/limits"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/scraper"
	"tubarr/internal/siterules"
	"tubarr/internal/state"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/viper"
)

// addChannelFromConfig adds a channel from a config file, with its logins and notifications.
//
// Errors in the file wrap contracts.ErrInvalidChannel.
func (ss *serverStore) addChannelFromConfig(path string) (c *models.Channel, ignoreRun bool, err error) {
	input, err := parsing.LoadChannelInputFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	if c, err = app.AddChannelFromInput(ss.cs, input); err != nil {
		return nil, false, err
	}
	return c, input.IgnoreRun != nil && *input.IgnoreRun, nil
}

// startNewChannelIgnoreCrawl starts an ignore crawl of a channel just added, reloaded so its per-URL settings apply.
func (ss *serverStore) startNewChannelIgnoreCrawl(c *models.Channel) {
	saved, found, err := ss.cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
	if err != nil || !found {
		logger.Pl.E("Failed to load channel %q for its ignore crawl (found: %t): %v", c.Name, found, err)
		return
	}
	ss.startIgnoreCrawl(saved)
}

// startIgnoreCrawl runs an ignore crawl of a channel in the background, unless one is already running, returning
// whether it started.
//
// The crawl outlives the request, so it runs under the server's context.
//
//nolint:contextcheck
func (ss *serverStore) startIgnoreCrawl(c *models.Channel) bool {
	crawlCtx, unlock, ok := state.TryLockCrawl(ss.ctx, c.ID)
	if !ok {
		logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
		return false
	}
	logger.Pl.I("Running ignore crawl for channel %q. No videos before this point will be downloaded to this channel.", c.Name)
	go func(ctx context.Context) {
		defer unlock()
		if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
			logger.Pl.E("Failed to run ignore crawl for channel %q: %v", c.Name, err)
		} else {
			logger.Pl.S("Successfully completed ignore crawl for channel %q", c.Name)
		}
	}(crawlCtx)
	return true
}

// errChannelNotFound is returned when the channel being changed doesn't exist.
var errChannelNotFound = errors.New("channel not found")

// channelErrStatus returns the HTTP status for a channel store error.
func channelErrStatus(err error) int {
	switch {
	case errors.Is(err, errChannelNotFound):
		return http.StatusNotFound
	case errors.Is(err, contracts.ErrInvalidChannel):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// handleAddChannelFromFile adds a new channel from a specified config file path using Viper.
func (ss *serverStore) handleAddChannelFromFile(w http.ResponseWriter, r *http.Request) {
	addFromFile := r.FormValue("add_from_config_file")
	if addFromFile == "" {
		http.Error(w, "no config file entered, could not add new channel", http.StatusBadRequest)
		return
	}

	c, ignoreRun, err := ss.addChannelFromConfig(addFromFile)
	if err != nil {
		http.Error(w, err.Error(), channelErrStatus(err))
		return
	}
	if ignoreRun {
		ss.startNewChannelIgnoreCrawl(c)
	}
	writeText(w, http.StatusCreated, fmt.Sprintf("Channel %q added successfully", c.Name))
}

// handleAddChannelsFromDir adds new channel from all config files in the directory path using Viper.
func (ss *serverStore) handleAddChannelsFromDir(w http.ResponseWriter, r *http.Request) {
	// Grab config file from form
	addFromDir := r.FormValue("add_from_config_dir")
	if addFromDir == "" {
		http.Error(w, "no config directory entered, could not add new channel", http.StatusBadRequest)
		return
	}

	// Scan directory for config files
	batchConfigFiles, err := file.ScanDirectoryForConfigFiles(addFromDir)
	if err != nil {
		http.Error(w, string(fmt.Appendf(nil, "failed to scan directory: %v", err)), http.StatusBadRequest)
		return
	}

	if len(batchConfigFiles) == 0 {
		writeText(w, http.StatusOK, fmt.Sprintf("No config files found in directory %q", addFromDir))
		return
	}

	type failure struct {
		file   string
		reason string
	}
	successes := make([]string, 0, len(batchConfigFiles))
	failures := make([]failure, 0, len(batchConfigFiles))

	logger.Pl.I("Adding channels from config files %v", batchConfigFiles)

	for _, batchConfigFile := range batchConfigFiles {
		c, ignoreRun, err := ss.addChannelFromConfig(batchConfigFile)
		if err != nil {
			failures = append(failures, failure{batchConfigFile, err.Error()})
			continue
		}
		if ignoreRun {
			ss.startNewChannelIgnoreCrawl(c)
		}
		successes = append(successes, c.Name)
	}

	successLen := len(successes)
	failLen := len(failures)

	if successLen == 0 && failLen != 0 {
		http.Error(w, fmt.Sprintf("failed to add any channels from directory %q: %+v", addFromDir, failures), http.StatusBadRequest)
		return
	}

	// Build detailed response message
	var responseMsg string
	if failLen > 0 {
		responseMsg = fmt.Sprintf("Processed %d config file(s) from directory %q:\n- Successfully added: %d channel(s)\n- Failed: %d channel(s)\n\nFailures:\n",
			len(batchConfigFiles), addFromDir, successLen, failLen)
		for _, failure := range failures {
			responseMsg += fmt.Sprintf("  - %s: %v\n", failure.file, failure.reason)
		}
	} else {
		responseMsg = fmt.Sprintf("Successfully added %d channel(s) from directory %q", successLen, addFromDir)
	}
	writeText(w, http.StatusCreated, responseMsg)
}

// handleListChannels lists Tubarr channels.
func (ss *serverStore) handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, found, err := ss.cs.GetAllChannels(false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSON(w, r, http.StatusOK, []any{})
		return
	}
	failedMetarr, err := ss.vs.CountFailedMetarrVideos(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := make([]channelResponse, 0, len(channels))
	for _, c := range channels {
		cr := channelResponse{
			ID:                   c.ID,
			Name:                 c.Name,
			URLs:                 c.GetURLs(),
			LastScan:             c.LastScan,
			CreatedAt:            c.CreatedAt,
			UpdatedAt:            c.UpdatedAt,
			ChannelConfigFile:    c.ChannelConfigFile,
			NewVideoNotification: c.NewVideoNotification,
			LoadError:            loadError(c),
			FailedPostProcessing: failedMetarr[c.ID],
			Settings:             newSettingsResponse(c.ChanSettings),
			Metarr:               newMetarrArgsResponse(c.ChanMetarrArgs),
		}
		for _, urlModel := range c.URLModels {
			cr.URLModels = append(cr.URLModels, channelURLResponse{
				ID:       urlModel.ID,
				URL:      urlModel.URL,
				Username: urlModel.Username,
				LoginURL: urlModel.LoginURL,
				Settings: newSettingsResponse(urlModel.ChanURLSettings),
				Metarr:   newMetarrArgsResponse(urlModel.ChanURLMetarrArgs),
			})
		}
		response = append(response, cr)
	}
	writeJSON(w, r, http.StatusOK, response)
}

// handleGetChannel returns the data for a specific channel.
func (ss *serverStore) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, consts.WebParamChannelID)

	// As saved, so each URL shows only its own settings in the edit form.
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel nil or not found", http.StatusNotFound)
		return
	}

	response := channelDetailResponse{
		ID:   c.ID,
		Name: c.Name,
		URLs: slices.DeleteFunc(c.GetURLs(), func(url string) bool {
			return url == consts.ManualDownloadsCol
		}),
		LastScan:          c.LastScan,
		CreatedAt:         c.CreatedAt,
		UpdatedAt:         c.UpdatedAt,
		ChannelConfigFile: c.ChannelConfigFile,
		LoadError:         loadError(c),
		Settings:          newSettingsResponse(c.ChanSettings),
		Metarr:            newMetarrArgsResponse(c.ChanMetarrArgs),
		AuthDetails:       []authDetailResponse{},
		URLSettings:       map[string]urlSettingsResponse{},
	}

	// Logins and custom settings per URL, leaving out manual downloads. Passwords are never sent back, only whether one is saved.
	for _, urlModel := range c.URLModels {
		if urlModel.URL == consts.ManualDownloadsCol {
			continue
		}
		if urlModel.Username != "" || urlModel.LoginURL != "" {
			response.AuthDetails = append(response.AuthDetails, authDetailResponse{
				ChannelURL:  urlModel.URL,
				Username:    urlModel.Username,
				HasPassword: urlModel.Password != "",
				LoginURL:    urlModel.LoginURL,
			})
		}
		if urlModel.ChanURLSettings != nil || urlModel.ChanURLMetarrArgs != nil {
			response.URLSettings[urlModel.URL] = urlSettingsResponse{
				Settings: newSettingsResponse(urlModel.ChanURLSettings),
				Metarr:   newMetarrArgsResponse(urlModel.ChanURLMetarrArgs),
			}
		}
	}

	// Notification URLs.
	notifications, err := ss.cs.GetNotifyURLs(c.ID)
	if err != nil {
		logger.Pl.E("Failed to fetch notification URLs for channel %d: %v", c.ID, err)
	}
	for _, n := range notifications {
		if n.NotifyURL != "" {
			response.Notifications = append(response.Notifications, notificationLine(n))
		}
	}

	// The settings the channel's config file replaces each run.
	if c.ChannelConfigFile != "" {
		response.ConfigFile = newConfigFileResponse(c.ChannelConfigFile)
	}
	writeJSON(w, r, http.StatusOK, response)
}

// handleCreateChannel creates a new channel entry.
func (ss *serverStore) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	f, err := parseChannelForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if f.name == "" {
		http.Error(w, "channel name is required", http.StatusBadRequest)
		return
	}

	now := time.Now()
	c := &models.Channel{
		Name:              f.name,
		ChannelConfigFile: f.configFile,
		ChanSettings:      f.settings,
		ChanMetarrArgs:    f.metarr,
		CreatedAt:         now,
		LastScan:          now,
		UpdatedAt:         now,
	}
	for _, u := range f.urls {
		cu := &models.ChannelURL{URL: u, LastScan: now, CreatedAt: now, UpdatedAt: now}

		// Auth details for this URL, if any.
		if a, exists := f.authMap[u]; exists {
			cu.Username, cu.Password, cu.LoginURL = a.Username, a.Password, a.LoginURL
		}

		// Apply per-URL custom settings if they exist.
		if s, hasCustom := f.urlSettings[u]; hasCustom {
			cu.ChanURLSettings, cu.ChanURLMetarrArgs = s.Settings, s.Metarr
		}
		c.URLModels = append(c.URLModels, cu)
	}

	// Add to database, with its notifications.
	if err := ss.cs.AddChannelWithDetails(c, nil, f.notifications); err != nil {
		http.Error(w, fmt.Sprintf("failed to add channel with name %q: %v", f.name, err), channelErrStatus(err))
		return
	}

	if r.FormValue("ignore_run") == "true" {
		ss.startNewChannelIgnoreCrawl(c)
	}
	writeText(w, http.StatusCreated, fmt.Sprintf("Channel %q added successfully", c.Name))
}

// handleUpdateChannel updates parameters for a given channel.
func (ss *serverStore) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	// Get channel ID from URL.
	idStr := chi.URLParam(r, consts.WebParamChannelID)
	channelID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	// Parse and validate everything before writing, so a rejected update changes nothing.
	f, err := parseChannelForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(f.urls) == 0 {
		http.Error(w, "at least one channel URL is required", http.StatusBadRequest)
		return
	}
	now := time.Now()

	// Update applies to the current channel and a failure changes nothing.
	if err := ss.cs.WithTx(func(cs contracts.ChannelStore) error {
		existingChannel, found, err := cs.GetChannelModel(consts.QChanID, idStr, false)
		if err != nil {
			return fmt.Errorf("failed to load channel: %w", err)
		}
		if !found || existingChannel == nil {
			return errChannelNotFound
		}

		if f.name != "" && f.name != existingChannel.Name {
			if err := cs.UpdateChannelValue(consts.QChanID, idStr, consts.QChanName, f.name); err != nil {
				return fmt.Errorf("failed to update channel name: %w", err)
			}
		}
		if f.configFile != existingChannel.ChannelConfigFile {
			if err := cs.UpdateChannelValue(consts.QChanID, idStr, consts.QChanConfigFile, f.configFile); err != nil {
				return fmt.Errorf("failed to update channel config file: %w", err)
			}
		}
		if _, err := cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
			*s = *f.settings
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update channel settings: %w", err)
		}
		if _, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanID, idStr, func(m *models.MetarrArgs) error {
			*m = *f.metarr
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update channel metarr args: %w", err)
		}

		// Rename, delete, add and update URLs.
		if err := syncChannelURLs(cs, channelID, existingChannel.URLModels, f, now); err != nil {
			return err
		}

		// Replace notifications.
		if err = replaceNotifications(cs, channelID, f); err != nil {
			return err
		}

		return nil
	}); err != nil {
		logger.Pl.E("Failed to update channel %d: %v", channelID, err)
		http.Error(w, err.Error(), channelErrStatus(err))
		return
	}
	writeText(w, http.StatusOK, "Channel updated successfully")
}

// handleDeleteChannel deletes a channel from Tubarr.
func (ss *serverStore) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, consts.WebParamChannelID)
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}

	// Stop the channel's crawl, downloads and Metarr runs, which would otherwise carry on for a deleted channel.
	if aborted, n := state.AbortAll(c.ID), app.StopMetarr(c.ID, nil); aborted || n > 0 {
		logger.Pl.I("Stopped running work for channel %q before deleting it (%d Metarr process(es))", c.Name, n)
	}

	if err := ss.cs.DeleteChannel(consts.QChanID, id); err != nil {
		logger.Pl.E("Failed to delete channel %q: %v", id, err)
		http.Error(w, fmt.Sprintf("failed to delete channel %q: %v", id, err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetAllVideos retrieves all downloaded videos for a given channel (ignored ones are on the Available page).
func (ss *serverStore) handleGetAllVideos(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}

	videos, _, err := ss.cs.GetDownloadedVideos(c)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve downloaded videos for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}

	// Blank paths for files moved or deleted outside Tubarr, so the page shows them as missing.
	for _, v := range videos {
		if v.VideoFilePath == "" {
			continue
		}
		if _, err := os.Stat(v.VideoFilePath); err != nil {
			v.VideoFilePath = ""
		}
	}
	ss.applyThumbnailOverrides(videos)
	writeJSON(w, r, http.StatusOK, videos)
}

// handleDeleteChannelVideos deletes given video entries from a channel.
func (ss *serverStore) handleDeleteChannelVideos(w http.ResponseWriter, r *http.Request) {
	// Get channel ID from URL path
	idStr := chi.URLParam(r, consts.WebParamChannelID)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not parse channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	// Read and parse body for DELETE requests.
	//
	// DELETE requests need special handling for form data in the body.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Pl.E("Failed to read body: %v", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse the URL-encoded form data from body.
	values, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		logger.Pl.E("Failed to parse query: %v", err)
		http.Error(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	// Get video URLs from form array.
	urls := values["urls[]"]
	logger.Pl.D(1, "Parsed form data: %+v", values)
	logger.Pl.D(1, "Video URLs to delete: %v", urls)

	if len(urls) == 0 {
		http.Error(w, "no video URLs provided", http.StatusBadRequest)
		return
	}

	if err := ss.cs.DeleteVideosByURLs(id, urls, values.Get("delete_files") == "true"); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete video URLs: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleGetDownloads retrieves active downloads for all channels.
func (ss *serverStore) handleGetDownloads(w http.ResponseWriter, r *http.Request) {
	channels, found, err := ss.cs.GetAllChannels(false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSON(w, r, http.StatusOK, []any{})
		return
	}

	// Collect active downloads from all channels
	allDownloads := make([]map[string]any, 0)
	for _, c := range channels {
		videos := ss.getActiveDownloads(c)

		// Convert each video to a map and add channel name
		for _, video := range videos {
			videoMap := make(map[string]any)
			videoMap[consts.QVidID] = video.ID
			videoMap[consts.QVidTitle] = video.Title
			videoMap[consts.QVidChanID] = video.ChannelID
			videoMap[consts.QVidChanURLID] = video.ChannelURLID
			videoMap[consts.QVidChanURL] = video.ChannelURL
			videoMap[consts.QVidChanName] = c.Name
			videoMap[consts.QVidURL] = video.URL
			videoMap[consts.QVidThumbnailURL] = video.ThumbnailURL
			video.DownloadStatus.ETA = downloads.ETA(video.ID, video.DownloadStatus.Percent)
			videoMap[consts.QVidDLStatus] = video.DownloadStatus

			allDownloads = append(allDownloads, videoMap)
		}
	}
	writeJSON(w, r, http.StatusOK, allDownloads)
}

// handleCancelDownload cancels an active download by video ID.
func (ss *serverStore) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	videoIDStr := chi.URLParam(r, consts.WebParamVideoID)
	videoID, err := strconv.ParseInt(videoIDStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video ID %q: %v", videoIDStr, err), http.StatusBadRequest)
		return
	}

	if err := ss.cancelDownload(r.Context(), videoID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNoActiveDownload) {
			status = http.StatusNotFound
		}
		http.Error(w, fmt.Sprintf("failed to cancel download: %v", err), status)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"message": "Download canceled successfully"})
}

// handleCancelDownloads cancels several downloads by video ID.
func (ss *serverStore) handleCancelDownloads(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VideoIDs []int64 `json:"video_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if len(body.VideoIDs) == 0 {
		http.Error(w, "no video IDs given", http.StatusBadRequest)
		return
	}

	canceled := 0
	for _, id := range body.VideoIDs {
		if err := ss.cancelDownload(r.Context(), id); err != nil {
			if !errors.Is(err, errNoActiveDownload) {
				logger.Pl.E("Failed to cancel download for video ID %d: %v", id, err)
			}
			continue
		}
		canceled++
	}
	if canceled == 0 {
		http.Error(w, "no matching downloads found", http.StatusNotFound)
		return
	}

	logger.Pl.I("Canceled %d download(s) via web request", canceled)
	writeJSON(w, r, http.StatusOK, map[string]int{"canceled": canceled})
}

// handleCrawlChannel initiates a crawl for a specific channel.
func (ss *serverStore) handleCrawlChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	if c.LoadErr != nil {
		http.Error(w, c.LoadErr.Error(), http.StatusConflict)
		return
	}

	// An optional body picks which URLs to crawl; picked paused URLs are crawled too.
	var requestBody struct {
		URLIDs []int64 `json:"url_ids"`
	}
	if err := decodeJSON(r, &requestBody); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if requestBody.URLIDs != nil {
		selected := make([]*models.ChannelURL, 0, len(requestBody.URLIDs))
		for _, cu := range c.URLModels {
			if cu.IsManual || !slices.Contains(requestBody.URLIDs, cu.ID) {
				continue
			}
			if cu.Paused() {
				unpaused := *cu.ChanURLSettings // Copied, so the URL stays paused.
				unpaused.Paused = false
				cu.ChanURLSettings = &unpaused
			}
			selected = append(selected, cu)
		}
		if len(selected) == 0 {
			http.Error(w, "no channel URLs selected to crawl", http.StatusBadRequest)
			return
		}
		c.URLModels = selected
	}

	// Start crawl in background.
	if crawlCtx, unlock, ok := state.TryLockCrawl(ss.ctx, c.ID); ok {
		// Clean expired blocks first.
		if err := blocking.CleanExpiredBlocks(ss.db); err != nil {
			logger.Pl.W("Failed to clean expired blocks: %v", err)
		}

		//nolint:contextcheck
		go func(ctx context.Context) {
			defer unlock()
			logger.Pl.I("Starting crawl for channel %q (ID: %d) via web request", c.Name, c.ID)

			if performedCrawls, err := app.CrawlChannel(ctx, ss.s, c); err != nil {
				logger.Pl.E("Encountered errors crawling channel %q: %v", c.Name, err)
			} else if performedCrawls {
				logger.Pl.S("Completed crawl for channel %q", c.Name)
			}
		}(crawlCtx)

		writeJSON(w, r, http.StatusAccepted, map[string]string{"message": "Channel crawl started"})
		return
	}
	logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
	writeJSON(w, r, http.StatusConflict, map[string]string{"message": "Channel crawl already running for channel"})
}

// handleStopQueue stops a channel's job from starting queued work, letting in-progress downloads and Metarr processes finish.
func (ss *serverStore) handleStopQueue(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	if !state.StopQueue(c.ID) {
		http.Error(w, "no crawl or download running for channel", http.StatusConflict)
		return
	}

	logger.Pl.I("Stopping queued work for channel %q via web request", c.Name)
	writeText(w, http.StatusAccepted, "Stopping.")
}

// handleAbortAll stops all of a channel's work, killing in-progress downloads and Metarr processes.
func (ss *serverStore) handleAbortAll(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	aborted := state.AbortAll(c.ID)
	n := app.StopMetarr(c.ID, nil)
	if !aborted && n == 0 {
		http.Error(w, "nothing running for channel", http.StatusConflict)
		return
	}

	logger.Pl.I("Aborting all work for channel %q via web request (%d Metarr process(es))", c.Name, n)
	writeText(w, http.StatusAccepted, "Stopping.")
}

// handleStopMetarr stops a channel's queued and running Metarr processes, or only those for the video IDs in the body.
func (ss *serverStore) handleStopMetarr(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		VideoIDs []int64 `json:"video_ids"`
	}
	if err := decodeJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	n := app.StopMetarr(c.ID, body.VideoIDs)
	if n == 0 {
		http.Error(w, "no matching Metarr processes running for channel", http.StatusConflict)
		return
	}

	logger.Pl.I("Stopping %d Metarr process(es) for channel %q via web request", n, c.Name)
	writeText(w, http.StatusAccepted, "Stopping.")
}

// handleGetActivity returns the IDs of channels with running crawls/downloads, crawls being stopped, and Metarr processes,
// and of the videos in Metarr.
func (ss *serverStore) handleGetActivity(w http.ResponseWriter, r *http.Request) {
	crawling, stopping := state.CrawlActivity()
	metarrChannels, metarrVideos := app.MetarrActivity()
	writeJSON(w, r, http.StatusOK, map[string][]int64{
		"crawling":      crawling,
		"stopping":      stopping,
		"metarr":        metarrChannels,
		"metarr_videos": metarrVideos,
	})
}

// handleGetPostProcessing returns the videos queued for or running in Metarr.
func (ss *serverStore) handleGetPostProcessing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, app.MetarrJobs())
}

// channelFromRequest loads the channel the request's {id} names, its URLs' unset settings filled in from the channel's
// as jobs use them, writing an error response if it can't.
func (ss *serverStore) channelFromRequest(w http.ResponseWriter, r *http.Request) (*models.Channel, bool) {
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, chi.URLParam(r, consts.WebParamChannelID), true)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	if !found || c == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return nil, false
	}
	return c, true
}

// channelURLFromRequest returns the channel URL named in the request path, where ID 0 is the channel's manual
// downloads URL (the ID of a video whose channel URL was removed).
func (ss *serverStore) channelURLFromRequest(w http.ResponseWriter, r *http.Request, c *models.Channel) (*models.ChannelURL, bool) {
	urlID, err := strconv.ParseInt(chi.URLParam(r, consts.WebParamURLID), 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid channel URL ID: %v", err), http.StatusBadRequest)
		return nil, false
	}
	cu, err := app.ManualChannelURL(ss.cs, c, urlID)
	if err != nil {
		if urlID != 0 {
			http.Error(w, "channel URL not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return nil, false
	}
	return cu, true
}

// handleIgnoreCrawlChannel initiates a crawl for a specific channel.
func (ss *serverStore) handleIgnoreCrawlChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	if c.LoadErr != nil {
		http.Error(w, c.LoadErr.Error(), http.StatusConflict)
		return
	}

	// Clean expired blocks first.
	if err := blocking.CleanExpiredBlocks(ss.db); err != nil {
		logger.Pl.W("Failed to clean expired blocks: %v", err)
	}
	if !ss.startIgnoreCrawl(c) {
		writeJSON(w, r, http.StatusConflict, map[string]string{"message": "Channel ignore crawl already running for channel"})
		return
	}
	writeJSON(w, r, http.StatusAccepted, map[string]string{"message": "Channel ignore crawl started"})
}

// handleDownloadURLs downloads specified video URLs to a channel.
func (ss *serverStore) handleDownloadURLs(w http.ResponseWriter, r *http.Request) {
	// Parse request body.
	var requestBody struct {
		ManualVideos []models.ManualVideo `json:"videos"`
	}

	if err := decodeJSON(r, &requestBody); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if len(requestBody.ManualVideos) == 0 {
		http.Error(w, "no videos provided", http.StatusBadRequest)
		return
	}

	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	if c.LoadErr != nil {
		http.Error(w, c.LoadErr.Error(), http.StatusConflict)
		return
	}
	if bad := app.CheckManualVideos(c, requestBody.ManualVideos); len(bad) > 0 {
		http.Error(w, "can't download:\n"+strings.Join(bad, "\n"), http.StatusBadRequest)
		return
	}
	if bad := app.UnavailableDestinations(c, requestBody.ManualVideos); len(bad) > 0 {
		http.Error(w, "can't download now:\n"+strings.Join(bad, "\n"), http.StatusConflict)
		return
	}

	// Start download in background, once any running crawl or download of the channel finishes.
	queued := state.CrawlStateActive(c.ID)
	//nolint:contextcheck
	go func() {
		ctx, unlock, err := state.LockCrawl(ss.ctx, c.ID)
		if err != nil {
			logger.Pl.W("Manual download of %d URL(s) for channel %q did not start: %v", len(requestBody.ManualVideos), c.Name, err)
			return
		}
		defer unlock()
		logger.Pl.I("Starting manual download of %d URL(s) for channel %q (ID: %d) via web request", len(requestBody.ManualVideos), c.Name, c.ID)

		if err := app.ManualDownloadToChannel(ctx, ss.s, ss.cs, c, requestBody.ManualVideos); err != nil {
			logger.Pl.E("Encountered errors downloading URLs for channel %q: %v", c.Name, err)
		} else {
			logger.Pl.S("Successfully completed manual download for channel %q", c.Name)
		}
	}()
	writeJSON(w, r, http.StatusAccepted, map[string]bool{"queued": queued})
}

// handleAddAvailableURLs adds video URLs to a channel's Available list, returning the added videos and the URLs already
// in the channel.
func (ss *serverStore) handleAddAvailableURLs(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		ManualVideos []models.ManualVideo `json:"videos"`
	}
	if err := decodeJSON(r, &requestBody); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if len(requestBody.ManualVideos) == 0 {
		http.Error(w, "no videos provided", http.StatusBadRequest)
		return
	}

	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	if bad := app.CheckManualVideos(c, requestBody.ManualVideos); len(bad) > 0 {
		http.Error(w, "can't add:\n"+strings.Join(bad, "\n"), http.StatusBadRequest)
		return
	}
	if slices.ContainsFunc(requestBody.ManualVideos, func(mv models.ManualVideo) bool { return mv.Auth != nil }) {
		http.Error(w, "authentication details aren't stored, so videos with them can only be downloaded directly", http.StatusBadRequest)
		return
	}

	added, existing, err := app.AddManualAvailable(ss.s, c, requestBody.ManualVideos)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"videos": added, "existing": existing})
}

// handleGetAvailableVideos returns videos awaiting the user's choice to download or dismiss them across all channels,
// with the ignored ones which can be restored if asked for.
//
// Failed videos from channel URLs which auto download are marked as retried by their next crawl.
func (ss *serverStore) handleGetAvailableVideos(w http.ResponseWriter, r *http.Request) {
	statuses := app.AvailableStatuses
	if r.URL.Query().Get("ignored") == "1" {
		statuses = slices.Concat(statuses, []consts.DownloadStatus{consts.DLStatusIgnored})
	}
	videos, err := ss.vs.GetAvailableVideos(statuses...)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve available videos: %v", err), http.StatusInternalServerError)
		return
	}
	channels, _, err := ss.cs.GetAllChannels(true)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	retried := make(map[int64]bool)
	for _, c := range channels {
		for _, cu := range c.URLModels {
			retried[cu.ID] = !cu.IsManual && c.AutoDownloads(cu)
		}
	}
	shown := make([]*models.Video, 0, len(videos))
	for _, v := range videos {
		v.RetriesOnCrawl = v.DownloadStatus.Status == consts.DLStatusFailed && retried[v.ChannelURLID]
		shown = append(shown, &v.Video)
	}
	ss.applyThumbnailOverrides(shown)
	writeJSON(w, r, http.StatusOK, videos)
}

// handleAvailableAction downloads or dismisses a channel's chosen Available videos, or restores ignored ones to Available.
//
// Downloads start once any running crawl or download of the channel finishes, so the channel's Stop Queue and Abort All buttons cover them.
func (ss *serverStore) handleAvailableAction(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		Action    string                `json:"action"`
		VideoIDs  []int64               `json:"video_ids"`
		Overrides *models.OverrideLines `json:"overrides"` // Download As, for one video.
		Metarr    *metarrArgsInput      `json:"metarr"`    // Download As's other Metarr args, with the overrides.
		Auth      *models.ManualAuth    `json:"auth"`      // Login details for this download only, never stored.
	}
	if err := decodeJSON(r, &requestBody); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if len(requestBody.VideoIDs) == 0 {
		http.Error(w, "no video IDs provided", http.StatusBadRequest)
		return
	}

	var status consts.DownloadStatus
	from := app.AvailableStatuses
	switch requestBody.Action {
	case "download":
		status = consts.DLStatusQueued
	case "dismiss":
		status = consts.DLStatusIgnored
	case "restore":
		status = consts.DLStatusAvailable
		from = []consts.DownloadStatus{consts.DLStatusIgnored}
	default:
		http.Error(w, fmt.Sprintf("unknown action %q (expected 'download', 'dismiss' or 'restore')", requestBody.Action), http.StatusBadRequest)
		return
	}
	if requestBody.Auth != nil && (status != consts.DLStatusQueued || !app.ValidManualAuth(requestBody.Auth)) {
		http.Error(w, "authentication needs a download, a username and a full http(s) login URL", http.StatusBadRequest)
		return
	}

	var overrides *models.VideoOverrides
	if requestBody.Overrides != nil {
		if status != consts.DLStatusQueued || len(requestBody.VideoIDs) != 1 {
			http.Error(w, "overrides need a download of one video", http.StatusBadRequest)
			return
		}
		var err error
		if overrides, err = parseOverrideLines(requestBody.Overrides, requestBody.Metarr); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	if c.LoadErr != nil && status == consts.DLStatusQueued {
		http.Error(w, c.LoadErr.Error(), http.StatusConflict)
		return
	}
	if overrides != nil {
		channelURLID, err := ss.vs.GetVideoChannelURLID(requestBody.VideoIDs[0])
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := checkOverridesFit(ss.cs, c, channelURLID, overrides); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	claimed, err := ss.vs.ClaimVideos(c.ID, requestBody.VideoIDs, from, status)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to update videos: %v", err), http.StatusInternalServerError)
		return
	}
	if overrides != nil && len(claimed) == 0 {
		http.Error(w, "video is no longer available, it may already be downloading", http.StatusConflict)
		return
	}

	if status == consts.DLStatusIgnored && len(claimed) > 0 {
		app.RemoveDismissedFromNew(ss.cs, c, claimed)
	}

	if status == consts.DLStatusQueued && len(claimed) > 0 {
		// Download As overrides replace any earlier ones, and a plain download clears them.
		claimedIDs := make([]int64, 0, len(claimed))
		for _, v := range claimed {
			claimedIDs = append(claimedIDs, v.ID)
		}
		if err := ss.vs.SetVideoOverrides(claimedIDs, overrides); err != nil {
			if _, resetErr := ss.vs.ClaimVideos(c.ID, claimedIDs, []consts.DownloadStatus{consts.DLStatusQueued}, consts.DLStatusFailed); resetErr != nil {
				logger.Pl.E("Failed to return videos to the Available list for channel %q: %v", c.Name, resetErr)
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		//nolint:contextcheck
		go func() {
			logger.Pl.I("Starting download of %d available video(s) for channel %q via web request", len(claimed), c.Name)
			if err := app.DownloadAvailable(ss.ctx, ss.s, c, claimed, requestBody.Auth); err != nil {
				logger.Pl.E("Encountered errors downloading available videos for channel %q: %v", c.Name, err)
			} else {
				logger.Pl.S("Completed download of available videos for channel %q", c.Name)
			}
		}()
	}

	writeJSON(w, r, http.StatusAccepted, map[string]int{"count": len(claimed)})
}

// handleGetOverrideOps fills the Download As dialog with the directories and ops a channel URL's videos use, and the
// given video's earlier overrides if it has any.
func (ss *serverStore) handleGetOverrideOps(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	file.UpdateFromConfigFile(ss.cs, c) // As a download does, so the ops shown are the ones it would run.
	cu, ok := ss.channelURLFromRequest(w, r, c)
	if !ok {
		return
	}

	var overrides *models.VideoOverrides
	if videoParam := r.URL.Query().Get("video"); videoParam != "" {
		videoID, err := strconv.ParseInt(videoParam, 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid video ID: %v", err), http.StatusBadRequest)
			return
		}
		if overrides, err = ss.vs.GetVideoOverrides(videoID); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
	}

	var response struct {
		Defaults       models.OverrideLines  `json:"defaults"`        // The channel URL's, for Revert to Defaults.
		Saved          *models.OverrideLines `json:"saved"`           // The video's earlier overrides, if any.
		DefaultsMetarr *metarrArgsResponse   `json:"defaults_metarr"` // The channel URL's other Metarr args.
		SavedMetarr    *metarrArgsResponse   `json:"saved_metarr"`    // The video's earlier other Metarr args, if any.
	}
	var err error
	if response.Defaults, err = metarr.ChannelURLOverrideLines(cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	response.DefaultsMetarr = newMetarrArgsResponse(cu.ChanURLMetarrArgs)
	if overrides != nil {
		saved := metarr.VideoOverrideLines(overrides, cu.URL)
		response.Saved = &saved
		response.SavedMetarr = newMetarrArgsResponse(&overrides.MetarrArgs)
	}
	writeJSON(w, r, http.StatusOK, response)
}

// handleGetThumbnail shows a video's thumbnail: its Download As image file, else its Download As or crawled URL's
// image, from the cache.
func (ss *serverStore) handleGetThumbnail(w http.ResponseWriter, r *http.Request) {
	videoID, err := strconv.ParseInt(chi.URLParam(r, consts.WebParamChannelID), 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video ID: %v", err), http.StatusBadRequest)
		return
	}
	o, err := ss.vs.GetVideoOverrides(videoID)
	if err != nil || (o != nil && o.StripThumbnail) {
		http.NotFound(w, r)
		return
	}

	var thumbURL string
	switch {
	case o != nil && o.Thumbnail != "" && !validation.IsHTTPURL(o.Thumbnail):
		serveThumbnailFile(w, r, o.Thumbnail)
		return
	case o != nil && o.Thumbnail != "":
		thumbURL = o.Thumbnail
	default:
		if thumbURL, err = ss.vs.GetVideoThumbnailURL(videoID); err != nil || !validation.IsHTTPURL(thumbURL) {
			http.NotFound(w, r)
			return
		}
	}

	path, err := file.CacheThumbnail(r.Context(), thumbURL)
	if err != nil {
		logger.Pl.W("Failed to cache thumbnail %q for video %d, sending the browser to it instead: %v", thumbURL, videoID, err)
		http.Redirect(w, r, thumbURL, http.StatusFound)
		return
	}
	serveThumbnailFile(w, r, path)
}

// serveThumbnailFile serves an image file, and nothing else, as the path may be whatever was typed into Download As.
func serveThumbnailFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Pl.E("Failed to close thumbnail %q: %v", path, err)
		}
	}()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	if !strings.HasPrefix(http.DetectContentType(head[:n]), "image/") {
		http.NotFound(w, r)
		return
	}
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// applyThumbnailOverrides marks videos with a custom Download As thumbnail, and blanks stripped ones.
func (ss *serverStore) applyThumbnailOverrides(videos []*models.Video) {
	overrides, err := ss.vs.ThumbnailOverrides()
	if err != nil {
		logger.Pl.E("Failed to read videos' Download As thumbnails: %v", err)
		return
	}
	for _, v := range videos {
		o, ok := overrides[v.ID]
		switch {
		case !ok:
		case o.StripThumbnail:
			v.ThumbnailURL = ""
		case o.Thumbnail != "":
			v.CustomThumbnail = true
		}
	}
}

// handleUploadThumbnail saves an image as a video's Download As thumbnail, returning its path.
func (ss *serverStore) handleUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoID, err := strconv.ParseInt(chi.URLParam(r, consts.WebParamChannelID), 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video ID: %v", err), http.StatusBadRequest)
		return
	}
	if _, err := ss.vs.GetVideoURLByID(videoID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}

	upload, _, err := r.FormFile("thumbnail")
	if err != nil {
		http.Error(w, fmt.Sprintf("no thumbnail image received (the limit is 10 MB): %v", err), http.StatusBadRequest)
		return
	}
	defer func() {
		if err := upload.Close(); err != nil {
			logger.Pl.E("Failed to close uploaded thumbnail: %v", err)
		}
		if r.MultipartForm != nil {
			if err := r.MultipartForm.RemoveAll(); err != nil {
				logger.Pl.E("Failed to remove uploaded thumbnail's temporary files: %v", err)
			}
		}
	}()
	data, err := io.ReadAll(upload)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read thumbnail image: %v", err), http.StatusBadRequest)
		return
	}

	path, err := file.SaveThumbnail(videoID, data)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, file.ErrThumbnailType) {
			status = http.StatusUnsupportedMediaType
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"path": path})
}

// handleGetOverrideOpsFile returns the ops in an ops file which apply to a channel URL, for a Download As dialog box.
func (ss *serverStore) handleGetOverrideOpsFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "no ops file path given", http.StatusBadRequest)
		return
	}
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	cu, ok := ss.channelURLFromRequest(w, r, c)
	if !ok {
		return
	}

	lines, err := metarr.FileOverrideLines(r.URL.Query().Get("box"), path, cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, r, http.StatusOK, map[string][]string{"lines": lines})
}

// handleRunMetarr runs Metarr on the given videos, or all pending/failed videos if none are given. Overrides, for one
// video, replace its options for this and later runs.
func (ss *serverStore) handleRunMetarr(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		VideoIDs  []int64               `json:"video_ids"`
		Overrides *models.OverrideLines `json:"overrides"`   // Edit & Run, for one video.
		Metarr    *metarrArgsInput      `json:"metarr"`      // Edit & Run's other Metarr args, with the overrides.
		Rerun     bool                  `json:"rerun"`       // Also runs on given videos Metarr already processed.
		Failed    bool                  `json:"failed_only"` // Runs on all the channel's videos Metarr failed on.
	}
	if err := decodeJSON(r, &requestBody); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if requestBody.Rerun && len(requestBody.VideoIDs) == 0 {
		http.Error(w, "a re-run needs the videos to run on", http.StatusBadRequest)
		return
	}
	if requestBody.Failed && (len(requestBody.VideoIDs) > 0 || requestBody.Rerun || requestBody.Overrides != nil) {
		http.Error(w, "a run on failed videos takes no videos, re-run or overrides", http.StatusBadRequest)
		return
	}

	var overrides *models.VideoOverrides
	if requestBody.Overrides != nil {
		if len(requestBody.VideoIDs) != 1 {
			http.Error(w, "overrides need one video", http.StatusBadRequest)
			return
		}
		var err error
		if overrides, err = parseOverrideLines(requestBody.Overrides, requestBody.Metarr); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}
	if c.LoadErr != nil {
		http.Error(w, c.LoadErr.Error(), http.StatusConflict)
		return
	}

	if requestBody.Failed {
		awaiting, err := ss.vs.GetVideosAwaitingMetarr(c.ID, false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, v := range awaiting {
			if v.MetarrStatus == consts.MetarrStatusFailed {
				requestBody.VideoIDs = append(requestBody.VideoIDs, v.ID)
			}
		}
		if len(requestBody.VideoIDs) == 0 {
			http.Error(w, "no videos in this channel failed post-processing", http.StatusConflict)
			return
		}
	}

	// Checked before saving edited options, which would otherwise replace the ones a running video uses.
	if n := app.MetarrBusy(c.ID, requestBody.VideoIDs); n > 0 {
		http.Error(w, fmt.Sprintf("%d of the video(s) are already post-processing", n), http.StatusConflict)
		return
	}

	// Edited options replace the video's earlier ones, kept for later runs.
	if overrides != nil {
		awaiting, err := ss.vs.GetVideosAwaitingMetarr(c.ID, requestBody.Rerun)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		i := slices.IndexFunc(awaiting, func(v *models.Video) bool { return v.ID == requestBody.VideoIDs[0] })
		if i < 0 {
			http.Error(w, "video isn't waiting for Metarr in this channel", http.StatusConflict)
			return
		}
		if err := checkOverridesFit(ss.cs, c, awaiting[i].ChannelURLID, overrides); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := ss.vs.SetVideoOverrides(requestBody.VideoIDs, overrides); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	//nolint:contextcheck
	go func(ctx context.Context) {
		file.UpdateFromConfigFile(ss.cs, c)
		if err := app.RunMetarr(ctx, ss.s, c, requestBody.VideoIDs, requestBody.Rerun); err != nil {
			logger.Pl.E("Encountered errors running Metarr for channel %q: %v", c.Name, err)
		} else {
			logger.Pl.S("Completed Metarr run for channel %q", c.Name)
		}
	}(ss.ctx)
	writeText(w, http.StatusAccepted, "Metarr run started.")
}

// handleLatestDownloads retrieves the latest downloads for a given channel.
func (ss *serverStore) handleLatestDownloads(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}

	videos, err := ss.vs.GetLatestFinishedVideos(r.Context(), c.ID, 10)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve downloaded videos for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}
	total, err := ss.vs.CountFinishedVideos(r.Context(), c.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not count downloaded videos for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}

	// Blank paths for files moved or deleted outside Tubarr, so the page shows them as missing.
	for _, v := range videos {
		if v.VideoFilePath == "" {
			continue
		}
		if _, err := os.Stat(v.VideoFilePath); err != nil {
			v.VideoFilePath = ""
		}
	}
	ss.applyThumbnailOverrides(videos)
	writeJSON(w, r, http.StatusOK, map[string]any{"videos": videos, "total": total})
}

// handleSetLogLevel sets the logging level in the database.
func (ss *serverStore) handleSetLogLevel(w http.ResponseWriter, r *http.Request) {
	levelStr := chi.URLParam(r, consts.WebParamLogLevel)
	level, err := strconv.Atoi(levelStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not set logging level using input %q: %v", levelStr, err), http.StatusBadRequest)
		return
	}
	level = min(max(level, -1), 5)
	logging.SetLevel(level)
	if err := ss.tss.SetSetting(keys.DebugLevel, strconv.Itoa(level)); err != nil {
		logger.Pl.W("Failed to save logging level: %v", err)
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Logging level set to %d", level))
}

// handleHealth reports whether the server and database are working (not whether jobs are running).
func (ss *serverStore) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	if err := ss.db.PingContext(ctx); err != nil {
		logger.Pl.E("Health check failed, database unreachable: %v", err)
		writeJSON(w, r, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetIdle reports running jobs, returning 503 if any are running.
func (ss *serverStore) handleGetIdle(w http.ResponseWriter, r *http.Request) {
	running := app.RunningProcesses()
	status := http.StatusOK
	if running > 0 {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, r, status, map[string]int32{"running": running})
}

// handleGetLogLevel retrieves the current logging level.
func (ss *serverStore) handleGetLogLevel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]int{"level": logging.Level()})
}

// handleGetTubarrLogs serves the log entries from memory.
func (ss *serverStore) handleGetTubarrLogs(w http.ResponseWriter, r *http.Request) {
	logs := logging.GetRecentLogsForProgram("Tubarr")
	if logs == nil {
		http.Error(w, "tubarr logger not initialized", http.StatusNotFound)
		return
	}
	writeLogLines(w, r, logs)
}

// handleGetMetarrLogs serves the Metarr log entries from memory.
func (ss *serverStore) handleGetMetarrLogs(w http.ResponseWriter, r *http.Request) {
	// Copy the lines under the lock, so a slow client doesn't hold up incoming logs (lines are never modified once stored).
	vars.MetarrLogsMutex.RLock()
	logs := slices.Clone(vars.MetarrLogs)
	vars.MetarrLogsMutex.RUnlock()
	writeLogLines(w, r, logs)
}

// writeLogLines sends log lines as plain text, stopping if the client leaves.
func writeLogLines(w http.ResponseWriter, r *http.Request, lines [][]byte) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		if r.Context().Err() != nil {
			return
		}
		if _, err := w.Write(line); err != nil {
			return // Client disconnected or network error.
		}
	}
}

// handlePostMetarrLogs receives log entries from Metarr instances via POST.
func (ss *serverStore) handlePostMetarrLogs(w http.ResponseWriter, r *http.Request) {
	// Read the log body from the request.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Pl.E("Failed to read Metarr log body: %v", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer func() {
		if closeErr := r.Body.Close(); closeErr != nil {
			logger.Pl.E("Could not close response body: %v", closeErr)
		}
	}()

	if len(body) == 0 {
		// No logs to add, return success.
		w.WriteHeader(http.StatusOK)
		return
	}

	// Split body into lines (each log entry should end with a newline).
	lines := strings.Split(string(body), "\n")

	// Lock and append non-empty lines to the global log store.
	vars.MetarrLogsMutex.Lock()
	defer vars.MetarrLogsMutex.Unlock()

	for _, line := range lines {
		if len(line) > 0 {
			// Append newline back since Split removes it.
			vars.MetarrLogs = append(vars.MetarrLogs, []byte(line+"\n"))
		}
	}

	// Enforce log limit by trimming oldest entries if exceeded.
	if len(vars.MetarrLogs) > vars.MaxMetarrLogs {
		// Keep only the most recent MaxMetarrLogs entries.
		excess := len(vars.MetarrLogs) - vars.MaxMetarrLogs
		vars.MetarrLogs = vars.MetarrLogs[excess:]
	}

	w.WriteHeader(http.StatusOK)
}

// handleGetBlockedDomains returns all currently blocked domains with their contexts and timestamps.
func (ss *serverStore) handleGetBlockedDomains(w http.ResponseWriter, r *http.Request) {
	// Clean expired blocks first.
	if err := blocking.CleanExpiredBlocks(ss.db); err != nil {
		logger.Pl.W("Failed to clean expired blocks: %v", err)
	}

	allBlocked := blocking.GetAllBlockedDomains()

	// Build response structure.
	type BlockedDomainResponse struct {
		Domain        string                          `json:"domain"`
		Contexts      map[vars.BlockContext]time.Time `json:"contexts"`
		TimeRemaining map[vars.BlockContext]string    `json:"time_remaining"`
	}

	response := make([]BlockedDomainResponse, 0, len(allBlocked))
	for domain, contexts := range allBlocked {
		activeContexts := make(map[vars.BlockContext]time.Time)
		timeRemaining := make(map[vars.BlockContext]string)

		// Only include non-expired contexts.
		for blockCtx, blockedAt := range contexts {
			isBlocked, _, remaining := blocking.IsBlocked(domain, blockCtx)
			if isBlocked {
				activeContexts[blockCtx] = blockedAt
				timeRemaining[blockCtx] = remaining.Round(time.Second).String()
			}
		}

		// Skip domains with no active blocks.
		if len(activeContexts) == 0 {
			continue
		}

		response = append(response, BlockedDomainResponse{
			Domain:        domain,
			Contexts:      activeContexts,
			TimeRemaining: timeRemaining,
		})
	}
	writeJSON(w, r, http.StatusOK, response)
}

// handleUnblockDomain unblocks a domain for all contexts or a specific context.
//
// Context can be specified via query parameter: ?context=unauth|cookie|auth
func (ss *serverStore) handleUnblockDomain(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, consts.WebParamDomain)
	contextParam := r.URL.Query().Get(consts.WebParamContext)

	if domain == "" {
		http.Error(w, "domain parameter is required", http.StatusBadRequest)
		return
	}

	blockCtx, err := blocking.ParseBlockContext(contextParam)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Unblock the domain.
	if err := blocking.UnblockDomain(ss.db, domain, blockCtx); err != nil {
		http.Error(w, fmt.Sprintf("failed to unblock domain: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, r, http.StatusOK, map[string]string{"message": "Domain unblocked successfully"})
}

// handleNewVideoNotificationSeen marks a new video notification as seen.
func (ss *serverStore) handleNewVideoNotificationSeen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, consts.WebParamChannelID)
	c, ok := ss.channelFromRequest(w, r)
	if !ok {
		return
	}

	// Set notification to false.
	if err := ss.cs.SetNotificationDot(c.ID, false); err != nil {
		http.Error(w, fmt.Sprintf("failed to mark new video notification as seen for channel ID %q: %v", id, err), http.StatusInternalServerError)
		return
	}

	// Lock for new video URL array update.
	vars.UpdateNewVideoURLMutex.Lock()
	defer vars.UpdateNewVideoURLMutex.Unlock()

	// Update new video URL array.
	if err := ss.cs.UpdateNewVideoURLs(consts.QChanID, id, []string{}); err != nil {
		http.Error(w, fmt.Sprintf("failed to empty new video URL slice for channel with ID %q: %v", id, err), http.StatusInternalServerError)
		return
	}
}

// handleTogglePauseChannel toggles the pause state of a channel.
func (ss *serverStore) handleTogglePauseChannel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, consts.WebParamChannelID)
	if _, ok := ss.channelFromRequest(w, r); !ok {
		return
	}

	// Track the new pause state (set inside the update function).
	var newPauseState bool

	// Update channel settings with toggled pause state.
	_, err := ss.cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
		// Toggle the current pause state.
		newPauseState = !s.Paused
		s.Paused = newPauseState
		return nil
	})
	if err != nil {
		logger.Pl.E("Failed to update channel pause state: %v", err)
		http.Error(w, fmt.Sprintf("failed to update channel pause state: %v", err), http.StatusInternalServerError)
		return
	}

	// Return success with new state.
	action := "resumed"
	if newPauseState {
		action = "paused"
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"message": fmt.Sprintf("Channel %s successfully.", action), "paused": newPauseState})
}

// handleGetCrawlConcurrency returns the current crawl concurrency limit.
func (ss *serverStore) handleGetCrawlConcurrency(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]int{"limit": viper.GetInt(keys.CrawlConcurrency)})
}

// handleSetCrawlConcurrency updates the crawl concurrency limit.
func (ss *serverStore) handleSetCrawlConcurrency(w http.ResponseWriter, r *http.Request) {
	limitStr := r.FormValue("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 0 {
		http.Error(w, fmt.Sprintf("invalid limit value %q, must be 0 or greater", limitStr), http.StatusBadRequest)
		return
	}

	// Persist to DB and update viper (takes effect on next crawl cycle).
	if err := ss.tss.SetSetting(keys.CrawlConcurrency, limitStr); err != nil {
		logger.Pl.W("Failed to persist crawl concurrency to DB: %v", err)
	}
	viper.Set(keys.CrawlConcurrency, limit)
	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Crawl concurrency updated", "limit": limit})
}

// handleGetGlobalDownloadConcurrency returns the current global download concurrency limit.
func (ss *serverStore) handleGetGlobalDownloadConcurrency(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]int{"limit": viper.GetInt(keys.GlobalDownloadConcurrency)})
}

// handleSetGlobalDownloadConcurrency updates the global download concurrency limit.
func (ss *serverStore) handleSetGlobalDownloadConcurrency(w http.ResponseWriter, r *http.Request) {
	limitStr := r.FormValue("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 0 {
		http.Error(w, fmt.Sprintf("invalid limit value %q, must be 0 or greater", limitStr), http.StatusBadRequest)
		return
	}

	// Persist to DB, update viper, and reinitialize the semaphore.
	if err := ss.tss.SetSetting(keys.GlobalDownloadConcurrency, limitStr); err != nil {
		logger.Pl.W("Failed to persist global download concurrency to DB: %v", err)
	}
	viper.Set(keys.GlobalDownloadConcurrency, limit)
	downloads.InitGlobalDownloadLimit(limit)
	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Global download concurrency updated", "limit": limit})
}

// handleGetPostProcessConcurrency returns the global post-processing (Metarr) concurrency limit.
func (ss *serverStore) handleGetPostProcessConcurrency(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]int{"limit": viper.GetInt(keys.PostProcessConcurrency)})
}

// handleSetPostProcessConcurrency updates the global post-processing (Metarr) concurrency limit.
func (ss *serverStore) handleSetPostProcessConcurrency(w http.ResponseWriter, r *http.Request) {
	limitStr := r.FormValue("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 0 {
		http.Error(w, fmt.Sprintf("invalid limit value %q, must be 0 or greater", limitStr), http.StatusBadRequest)
		return
	}

	if err := ss.tss.SetSetting(keys.PostProcessConcurrency, limitStr); err != nil {
		logger.Pl.W("Failed to persist post-processing concurrency to DB: %v", err)
	}
	viper.Set(keys.PostProcessConcurrency, limit)
	app.InitGlobalMetarrLimit(limit)
	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Post-processing concurrency updated", "limit": limit})
}

// handleGetDomainLimits returns all per-domain download and crawl concurrency limits.
func (ss *serverStore) handleGetDomainLimits(w http.ResponseWriter, r *http.Request) {
	limits, err := ss.tss.GetDomainLimits()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get domain limits: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, r, http.StatusOK, limits)
}

// handleSetDomainLimit sets the download and crawl concurrency limits for a specific domain (0 or blank = unlimited).
func (ss *serverStore) handleSetDomainLimit(w http.ResponseWriter, r *http.Request) {
	hostname := limits.NormalizeDomain(r.FormValue("hostname"))
	if hostname == "" {
		http.Error(w, "hostname parameter is required", http.StatusBadRequest)
		return
	}
	var limit models.DomainLimit
	for param, dst := range map[string]*int{"download": &limit.Download, "crawl": &limit.Crawl} {
		val := r.FormValue(param)
		if val == "" {
			continue
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			http.Error(w, fmt.Sprintf("invalid %s limit %q: must be 0 or greater", param, val), http.StatusBadRequest)
			return
		}
		*dst = n
	}
	if err := ss.tss.SetDomainLimit(hostname, limit); err != nil {
		http.Error(w, fmt.Sprintf("failed to set domain limit: %v", err), http.StatusInternalServerError)
		return
	}
	limits.DomainDownloads.Set(hostname, limit.Download)
	limits.DomainCrawls.Set(hostname, limit.Crawl)
	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Domain limit updated", "hostname": hostname, "download": limit.Download, "crawl": limit.Crawl})
}

// handleDeleteDomainLimit removes the download and crawl concurrency limits for a specific domain.
func (ss *serverStore) handleDeleteDomainLimit(w http.ResponseWriter, r *http.Request) {
	hostname := limits.NormalizeDomain(chi.URLParam(r, consts.WebParamHostname))
	if hostname == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
		return
	}
	if err := ss.tss.DeleteDomainLimit(hostname); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete domain limit: %v", err), http.StatusInternalServerError)
		return
	}
	limits.DomainDownloads.Set(hostname, 0)
	limits.DomainCrawls.Set(hostname, 0)
	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Domain limit removed", "hostname": hostname})
}

// handleGetScrapeConfigFile returns the currently configured custom scrape site config file path.
func (ss *serverStore) handleGetScrapeConfigFile(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]string{"path": viper.GetString(keys.ScrapeConfigFile)})
}

// handleSetScrapeConfigFile updates (or, given an empty path, clears) the custom scrape site config file.
func (ss *serverStore) handleSetScrapeConfigFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))

	// Parse before touching any global state, so a bad path can't wipe out a working config.
	var sites []models.SiteScraper
	if path != "" {
		var err error
		sites, err = parsing.ParseScrapeSelectorsFile(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to load scrape config file: %v", err), http.StatusBadRequest)
			return
		}
	}

	siterules.Reset()
	if len(sites) > 0 {
		siterules.Register(sites)
	}

	if err := ss.tss.SetSetting(keys.ScrapeConfigFile, path); err != nil {
		logger.Pl.W("Failed to persist scrape config file path to DB: %v", err)
	}
	viper.Set(keys.ScrapeConfigFile, path)
	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Scrape config file updated", "path": path, "sites": len(sites)})
}

// handleGetFlareSolverrURL returns the currently configured FlareSolverr URL.
func (ss *serverStore) handleGetFlareSolverrURL(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]string{"url": viper.GetString(keys.FlareSolverrURL)})
}

// handleSetFlareSolverrURL updates (or, given an empty URL, clears) the FlareSolverr URL.
func (ss *serverStore) handleSetFlareSolverrURL(w http.ResponseWriter, r *http.Request) {
	fsURL := strings.TrimSpace(r.FormValue("url"))
	if fsURL != "" {
		if err := scraper.ValidateFlareSolverrURL(fsURL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := ss.tss.SetSetting(keys.FlareSolverrURL, fsURL); err != nil {
		logger.Pl.W("Failed to persist FlareSolverr URL to DB: %v", err)
	}
	viper.Set(keys.FlareSolverrURL, fsURL)
	writeJSON(w, r, http.StatusOK, map[string]string{"message": "FlareSolverr URL updated", "url": fsURL})
}

// handleGetTLS returns the HTTPS state and custom certificate paths.
func (ss *serverStore) handleGetTLS(w http.ResponseWriter, r *http.Request) {
	certPath, keyPath, certLocked := tlsPaths(ss.tss)
	enabled, locked := httpsSetting(ss.tss)
	writeJSON(w, r, http.StatusOK, map[string]any{
		"https":       ss.certs != nil, // Serving HTTPS now.
		"enabled":     enabled,         // Serving HTTPS after a restart.
		"locked":      locked,
		"cert":        certPath,
		"key":         keyPath,
		"cert_locked": certLocked,
		"active":      ss.certs.active(),
	})
}

// handleSetTLS updates (or, given an empty certificate path, clears) the custom HTTPS certificate.
func (ss *serverStore) handleSetTLS(w http.ResponseWriter, r *http.Request) {
	if _, _, locked := tlsPaths(nil); locked {
		http.Error(w, "certificate is set by --"+keys.WebTLSCert+" or "+consts.EnvWebTLSCert+", change it there", http.StatusConflict)
		return
	}
	certPath := strings.TrimSpace(r.FormValue("cert"))
	keyPath := strings.TrimSpace(r.FormValue("key"))
	if certPath == "" && keyPath != "" {
		http.Error(w, "a key needs a certificate path", http.StatusBadRequest)
		return
	}

	// Load before saving, so a bad path can't replace a working certificate.
	if certPath != "" {
		if err := checkKeyPair(certPath, keyPath); err != nil {
			http.Error(w, fmt.Sprintf("failed to load certificate: %v", err), http.StatusBadRequest)
			return
		}
	}

	for k, val := range map[string]string{keys.WebTLSCert: certPath, keys.WebTLSKey: keyPath} {
		if err := ss.tss.SetSetting(k, val); err != nil {
			http.Error(w, fmt.Sprintf("failed to save setting %q: %v", k, err), http.StatusInternalServerError)
			return
		}
	}
	ss.certs.setPaths(certPath, keyPath)

	msg := "Certificate updated"
	if ss.certs == nil {
		msg = "Certificate saved, used once HTTPS is on"
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"message": msg, "active": ss.certs.active()})
}

// handleSetHTTPSEnabled saves whether to serve HTTPS, applied when Tubarr restarts.
func (ss *serverStore) handleSetHTTPSEnabled(w http.ResponseWriter, r *http.Request) {
	if _, locked := httpsSetting(nil); locked {
		http.Error(w, "HTTPS is set by --"+keys.WebHTTPS+" or "+consts.EnvWebHTTPS+", change it there", http.StatusConflict)
		return
	}
	enabled, err := strconv.ParseBool(r.FormValue("enabled"))
	if err != nil {
		http.Error(w, "enabled must be true or false", http.StatusBadRequest)
		return
	}
	if err := ss.tss.SetSetting(keys.WebHTTPS, strconv.FormatBool(enabled)); err != nil {
		http.Error(w, fmt.Sprintf("failed to save setting %q: %v", keys.WebHTTPS, err), http.StatusInternalServerError)
		return
	}

	msg := "Saved"
	if enabled != (ss.certs != nil) {
		msg = "Saved, restart Tubarr to apply"
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"message": msg, "enabled": enabled})
}

// handleTestFlareSolverr solves a simple page through FlareSolverr to check it works.
// Uses the URL in the request if given, so it can be tested before saving.
func (ss *serverStore) handleTestFlareSolverr(w http.ResponseWriter, r *http.Request) {
	fsURL := strings.TrimSpace(r.FormValue("url"))
	if fsURL == "" {
		fsURL = viper.GetString(keys.FlareSolverrURL)
	}
	if fsURL == "" {
		http.Error(w, "no FlareSolverr URL entered", http.StatusBadRequest)
		return
	}

	status, userAgent, cookies, elapsed, err := scraper.TestFlareSolverr(r.Context(), fsURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("FlareSolverr test failed: %v", err), http.StatusBadGateway)
		return
	}

	writeJSON(w, r, http.StatusOK, map[string]any{"status": status, "user_agent": userAgent, "cookies": cookies, "seconds": elapsed.Seconds()})
}

// handleReloadScrapeConfigFile re-parses the currently configured scrape config file from disk,
// without changing which file is configured. Useful if the file was edited and saved while Tubarr is running.
func (ss *serverStore) handleReloadScrapeConfigFile(w http.ResponseWriter, r *http.Request) {
	path := viper.GetString(keys.ScrapeConfigFile)
	if path == "" {
		siterules.Reset()
		writeJSON(w, r, http.StatusOK, map[string]any{"message": "No scrape config file set; using built-in defaults", "sites": 0})
		return
	}

	// Parse before touching any global state, so a bad edit can't wipe out a working config.
	sites, err := parsing.ParseScrapeSelectorsFile(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to reload scrape config file: %v", err), http.StatusBadRequest)
		return
	}

	siterules.Reset()
	siterules.Register(sites)
	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Scrape config file reloaded", "path": path, "sites": len(sites)})
}

// handleGetScrapeConfigContent returns the configured scrape config file's contents, for editing in the web UI.
//
// Includes a version (hash of the contents), so a later save can detect edits made elsewhere in the meantime.
func (ss *serverStore) handleGetScrapeConfigContent(w http.ResponseWriter, r *http.Request) {
	path := viper.GetString(keys.ScrapeConfigFile)
	resp := map[string]any{"path": path, "content": "", "version": ""}
	if path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read scrape config file: %v", err), http.StatusInternalServerError)
			return
		}
		resp["content"], resp["version"] = string(content), contentVersion(content)
	}
	writeJSON(w, r, http.StatusOK, resp)
}

// handleSetScrapeConfigContent validates new contents for the configured scrape config file, then saves and loads them.
//
// An invalid edit is rejected without touching the file. So is a save of a file changed on disk since it was loaded.
func (ss *serverStore) handleSetScrapeConfigContent(w http.ResponseWriter, r *http.Request) {
	path := viper.GetString(keys.ScrapeConfigFile)
	if path == "" {
		http.Error(w, "no scrape config file is set", http.StatusBadRequest)
		return
	}

	current, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read scrape config file: %v", err), http.StatusInternalServerError)
		return
	}
	if r.FormValue("version") != contentVersion(current) {
		http.Error(w, "the scrape config file has changed on disk since it was loaded. Reload it before saving.", http.StatusConflict)
		return
	}

	content := r.FormValue("content")
	sites, err := parsing.SaveScrapeSelectorsFile(path, content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	siterules.Reset()
	siterules.Register(sites)

	writeJSON(w, r, http.StatusOK, map[string]any{"message": "Scrape config file saved", "sites": len(sites), "version": contentVersion([]byte(content))})
}

// contentVersion returns a version string for file contents, which changes whenever the contents do.
func contentVersion(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// handleGetScrapeSites returns every currently registered scrape site (built-in and custom)
// along with its selectors, for display in the web UI.
func (ss *serverStore) handleGetScrapeSites(w http.ResponseWriter, r *http.Request) {
	sites := siterules.List()

	type fetchResp struct {
		URL      string `json:"url"`
		Selector string `json:"selector,omitempty"`
		Attr     string `json:"attr,omitempty"`
		JSONPath string `json:"json_path,omitempty"`
		Regex    string `json:"regex,omitempty"`
	}
	type selectorResp struct {
		Field    string      `json:"field"`
		Selector string      `json:"selector"`
		Attr     string      `json:"attr,omitempty"`
		JSONPath string      `json:"json_path,omitempty"`
		Regex    string      `json:"regex,omitempty"`
		Fetch    []fetchResp `json:"fetch,omitempty"`
	}
	regexString := func(re *regexp.Regexp) string {
		if re == nil {
			return ""
		}
		return re.String()
	}
	type crawlResp struct {
		Selector      string         `json:"selector"`
		Attr          string         `json:"attr,omitempty"`
		JSONPath      string         `json:"json_path,omitempty"`
		Include       string         `json:"include,omitempty"`
		Exclude       string         `json:"exclude,omitempty"`
		StripQuery    bool           `json:"strip_query,omitempty"`
		Item          string         `json:"item,omitempty"`
		ItemPath      string         `json:"item_path,omitempty"`
		PreviewFields []selectorResp `json:"preview_fields,omitempty"`
	}
	type siteResp struct {
		Domain              string         `json:"domain"`
		Selectors           []selectorResp `json:"selectors"`
		Crawl               *crawlResp     `json:"crawl,omitempty"`
		Impersonate         string         `json:"impersonate,omitempty"`
		UserAgent           string         `json:"user_agent,omitempty"`
		FlareSolverr        bool           `json:"flaresolverr,omitempty"`
		FlareSolverrTimeout int            `json:"flaresolverr_timeout,omitempty"` // Seconds.
	}

	resp := make([]siteResp, 0, len(sites))
	for _, site := range sites {
		selectors := make([]selectorResp, 0, len(site.Metadata))
		for _, rule := range site.Metadata {
			sel := selectorResp{Field: rule.Name, Selector: rule.Selector, Attr: rule.Attr, JSONPath: rule.JSONPath, Regex: regexString(rule.Regex)}
			for _, f := range rule.Fetch {
				sel.Fetch = append(sel.Fetch, fetchResp{URL: f.URL, Selector: f.Selector, Attr: f.Attr, JSONPath: f.JSONPath, Regex: regexString(f.Regex)})
			}
			selectors = append(selectors, sel)
		}
		var crawl *crawlResp
		if c := site.Crawl; c != nil {
			crawl = &crawlResp{Selector: c.Selector, Attr: c.Attr, JSONPath: c.JSONPath, StripQuery: c.StripQuery, Item: c.Item, ItemPath: c.ItemPath}
			for _, f := range c.PreviewFields {
				crawl.PreviewFields = append(crawl.PreviewFields, selectorResp{Field: f.Name, Selector: f.Selector, Attr: f.Attr, JSONPath: f.JSONPath, Regex: regexString(f.Regex)})
			}
			if c.Include != nil {
				crawl.Include = c.Include.String()
			}
			if c.Exclude != nil {
				crawl.Exclude = c.Exclude.String()
			}
		}
		fsTimeout := site.FlareSolverrTimeout
		if site.FlareSolverr && fsTimeout == 0 {
			fsTimeout = consts.FlareSolverrDefaultTimeout
		}
		resp = append(resp, siteResp{
			Domain:              site.Site,
			Selectors:           selectors,
			Crawl:               crawl,
			Impersonate:         site.Impersonate,
			UserAgent:           site.UserAgent,
			FlareSolverr:        site.FlareSolverr,
			FlareSolverrTimeout: int(fsTimeout.Seconds()),
		})
	}

	writeJSON(w, r, http.StatusOK, resp)
}

// handleTestScrapeSite tests the currently registered custom scraping rules against a URL
// and returns whatever metadata gets discovered, for previewing a scrape config file.
func (ss *serverStore) handleTestScrapeSite(w http.ResponseWriter, r *http.Request) {
	testURL := strings.TrimSpace(r.FormValue("url"))
	if testURL == "" {
		http.Error(w, "url parameter is required", http.StatusBadRequest)
		return
	}

	matched, site, metadata, err := scraper.TestScrapeSite(r.Context(), testURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to scrape URL: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, r, http.StatusOK, map[string]any{"matched": matched, "site": site, "metadata": metadata})
}

// handleTestCrawlSite runs the crawler Tubarr would use for a channel URL and returns the video URLs found.
func (ss *serverStore) handleTestCrawlSite(w http.ResponseWriter, r *http.Request) {
	testURL := strings.TrimSpace(r.FormValue("url"))
	if testURL == "" {
		http.Error(w, "url parameter is required", http.StatusBadRequest)
		return
	}

	matched, source, videos, err := scraper.TestCrawlSite(r.Context(), testURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to crawl URL: %v", err), http.StatusInternalServerError)
		return
	}

	urls := make([]string, 0, len(videos))
	for _, v := range videos {
		urls = append(urls, v.URL)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"matched": matched, "source": source, "urls": urls, "videos": videos})
}
