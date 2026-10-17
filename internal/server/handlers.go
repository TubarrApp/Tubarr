package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"tubarr/internal/auth"
	"tubarr/internal/blocking"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/downloads"
	"tubarr/internal/file"
	"tubarr/internal/limits"
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
	var input models.ChannelInputPtrs
	v := viper.New()
	if err := file.LoadConfigFile(v, path); err != nil {
		return nil, false, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	if err := parsing.LoadViperIntoStruct(v, &input); err != nil {
		return nil, false, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	if input.URLSettings, err = parsing.ParseURLSettingsFromViper(v); err != nil {
		return nil, false, fmt.Errorf("%w: failed to parse url-settings: %w", contracts.ErrInvalidChannel, err)
	}

	c, authMap, err := parsing.BuildChannelFromInput(input)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	var notifications []*models.Notification
	if input.Notification != nil && len(*input.Notification) != 0 {
		if notifications, err = parsing.ParseNotifications(*input.Notification); err != nil {
			return nil, false, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
		}
	}

	if err := ss.cs.AddChannelWithDetails(c, authMap, notifications); err != nil {
		return nil, false, err
	}
	return c, input.IgnoreRun != nil && *input.IgnoreRun, nil
}

// startIgnoreCrawl runs an ignore crawl for a new channel in the background, unless one is already running.
//
// The crawl outlives the request, so it runs under the server's context.
//
//nolint:contextcheck
func (ss *serverStore) startIgnoreCrawl(c *models.Channel) {
	crawlCtx, unlock, ok := state.TryLockCrawl(ss.ctx, c.Name)
	if !ok {
		logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
		return
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
		ss.startIgnoreCrawl(c)
	}

	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(fmt.Appendf(nil, "Channel %q added successfully", c.Name)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
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
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(fmt.Appendf(nil, "No config files found in directory %q", addFromDir)); err != nil {
			logger.Pl.E("Failed to write response message: %v", err)
		}
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
			ss.startIgnoreCrawl(c)
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

	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write([]byte(responseMsg)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleListChannels lists Tubarr channels.
func (ss *serverStore) handleListChannels(w http.ResponseWriter, _ *http.Request) {
	channels, found, err := ss.cs.GetAllChannels(false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]any{}); err != nil {
			logger.Pl.E("Failed to encode blank 'any' array: %v", err)
		}
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Pl.E("Failed to encode channels: %v", err)
	}
}

// handleGetChannel returns the data for a specific channel.
func (ss *serverStore) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, consts.QChanID)

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
		LastScan:    c.LastScan,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
		Settings:    newSettingsResponse(c.ChanSettings),
		Metarr:      newMetarrArgsResponse(c.ChanMetarrArgs),
		AuthDetails: []authDetailResponse{},
		URLSettings: map[string]urlSettingsResponse{},
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
			response.Notifications = append(response.Notifications, n.NotifyURL)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode JSON", http.StatusInternalServerError)
	}
}

// handleCreateChannel creates a new channel entry.
func (ss *serverStore) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	// Parse form data.
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse form data: %v", err), http.StatusBadRequest)
		return
	}

	// Get form values.
	name := r.FormValue("name")
	urls := strings.Fields(r.FormValue("urls"))
	username := r.FormValue("username")
	loginURL := r.FormValue("login_url")
	password := r.FormValue("password")
	now := time.Now()

	// Parse authentication details.
	authDetails, err := splitAuthDetails(r.FormValue("auth_details"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authMap, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, urls, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid authentication details: %v", err), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings JSON if provided.
	urlSettingsMap, err := parseURLSettings(r.FormValue("url_settings"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate and create ChannelURL models.
	var chanURLs = make([]*models.ChannelURL, 0, len(urls))
	for _, u := range urls {
		if u != "" {
			if err := validation.ValidateChannelURL(u); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Get auth details for this URL if they exist.
			var parsedUsername, parsedPassword, parsedLoginURL string
			if _, exists := authMap[u]; exists {
				parsedUsername = authMap[u].Username
				parsedPassword = authMap[u].Password
				parsedLoginURL = authMap[u].LoginURL
			}

			// Create channel URL model.
			chanURL := &models.ChannelURL{
				URL:       u,
				Username:  parsedUsername,
				Password:  parsedPassword,
				LoginURL:  parsedLoginURL,
				LastScan:  now,
				CreatedAt: now,
				UpdatedAt: now,
			}

			// Apply per-URL custom settings if they exist.
			if urlSettings, hasCustom := urlSettingsMap[u]; hasCustom {
				chanURL.ChanURLSettings = urlSettings.Settings
				chanURL.ChanURLMetarrArgs = urlSettings.Metarr
			}

			chanURLs = append(chanURLs, chanURL)
		}
	}

	// Create model.
	c := &models.Channel{
		Name:      name,
		URLModels: chanURLs,
		CreatedAt: now,
		LastScan:  now,
		UpdatedAt: now,
	}

	// Get and validate settings.
	c.ChanSettings = getSettingsStrings(w, r)
	if c.ChanSettings == nil {
		// Validation error already written to response.
		return
	}

	c.ChanMetarrArgs = getMetarrArgsStrings(w, r)
	if c.ChanMetarrArgs == nil {
		// Validation error already written to response.
		return
	}

	// Add to database.
	if c.ID, err = ss.cs.AddChannel(c); err != nil {
		http.Error(w, fmt.Sprintf("failed to add channel with name %q: %v", name, err), channelErrStatus(err))
		return
	}

	if r.FormValue("ignore_run") == "true" {
		ss.startIgnoreCrawl(c)
	}

	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(fmt.Appendf(nil, "Channel %q added successfully", c.Name)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleUpdateChannel updates parameters for a given channel.
func (ss *serverStore) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	// Get channel ID from URL.
	idStr := chi.URLParam(r, "id")
	channelID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	// Parse form data.
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse form data: %v", err), http.StatusBadRequest)
		return
	}

	// -- Parse and validate everything before writing, so a rejected update changes nothing. --
	name := r.FormValue("name")

	newSettings := getSettingsStrings(w, r)
	if newSettings == nil {
		// Validation error already written to response
		return
	}
	newMetarr := getMetarrArgsStrings(w, r)
	if newMetarr == nil {
		// Validation error already written to response
		return
	}

	urls := strings.Fields(r.FormValue("urls"))
	if len(urls) == 0 {
		http.Error(w, "at least one channel URL is required", http.StatusBadRequest)
		return
	}
	for _, u := range urls {
		if err := validation.ValidateChannelURL(u); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	username := r.FormValue("username")
	loginURL := r.FormValue("login_url")
	password := r.FormValue("password")
	now := time.Now()

	// Parse authentication details.
	authDetails, err := splitAuthDetails(r.FormValue("auth_details"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authMap, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, urls, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid authentication details: %v", err), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings JSON if provided.
	urlSettingsMap, err := parseURLSettings(r.FormValue("url_settings"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	notificationURLs := strings.Fields(r.FormValue("notifications"))
	notifications := make([]*models.Notification, 0, len(notificationURLs))
	for _, notifyURL := range notificationURLs {
		notifications = append(notifications, &models.Notification{ChannelID: channelID, NotifyURL: notifyURL})
	}

	// -- Read and write in one transaction, so the update applies to the current channel and a failure changes nothing. --
	if err := ss.cs.WithTx(func(cs contracts.ChannelStore) error {
		existingChannel, found, err := cs.GetChannelModel(consts.QChanID, idStr, false)
		if err != nil {
			return fmt.Errorf("failed to load channel: %w", err)
		}
		if !found || existingChannel == nil {
			return errChannelNotFound
		}

		if name != "" && name != existingChannel.Name {
			if err := cs.UpdateChannelValue(consts.QChanID, idStr, consts.QChanName, name); err != nil {
				return fmt.Errorf("failed to update channel name: %w", err)
			}
		}
		if _, err := cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
			*s = *newSettings
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update channel settings: %w", err)
		}
		if _, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanID, idStr, func(m *models.MetarrArgs) error {
			*m = *newMetarr
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update channel metarr args: %w", err)
		}

		// Delete URLs no longer listed. The manual downloads URL is never listed in the edit form, so it's kept.
		existingURLMap := make(map[string]*models.ChannelURL, len(existingChannel.URLModels))
		for _, urlModel := range existingChannel.URLModels {
			existingURLMap[urlModel.URL] = urlModel
		}
		for existingURL, urlModel := range existingURLMap {
			if !urlModel.IsManual && !slices.Contains(urls, existingURL) {
				if err := cs.DeleteChannelURL(urlModel.ID); err != nil {
					return fmt.Errorf("failed to delete URL %q: %w", existingURL, err)
				}
			}
		}

		// Add or update listed URLs.
		for _, u := range urls {
			var parsedUsername, parsedPassword, parsedLoginURL string
			if a, exists := authMap[u]; exists {
				parsedUsername, parsedPassword, parsedLoginURL = a.Username, a.Password, a.LoginURL
			}
			urlSettings := urlSettingsMap[u]

			if existingURLModel, exists := existingURLMap[u]; exists {
				// A blank password keeps the saved one, as passwords aren't sent to the edit page.
				if parsedPassword != "" || parsedUsername == "" {
					existingURLModel.Password = parsedPassword
				}
				existingURLModel.Username = parsedUsername
				existingURLModel.LoginURL = parsedLoginURL
				existingURLModel.UpdatedAt = now
				existingURLModel.ChanURLSettings = urlSettings.Settings
				existingURLModel.ChanURLMetarrArgs = urlSettings.Metarr

				if err := cs.UpdateChannelURLSettings(existingURLModel); err != nil {
					return fmt.Errorf("failed to update URL %q: %w", u, err)
				}
				continue
			}

			chanURL := &models.ChannelURL{
				URL:               u,
				Username:          parsedUsername,
				Password:          parsedPassword,
				LoginURL:          parsedLoginURL,
				ChanURLSettings:   urlSettings.Settings,
				ChanURLMetarrArgs: urlSettings.Metarr,
				LastScan:          now,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if _, err := cs.AddChannelURL(channelID, chanURL, false); err != nil {
				return fmt.Errorf("failed to add URL %q: %w", u, err)
			}
		}

		// Replace notifications.
		existingNotifications, err := cs.GetNotifyURLs(channelID)
		if err != nil {
			return fmt.Errorf("failed to fetch existing notifications: %w", err)
		}
		existingURLs := make([]string, 0, len(existingNotifications))
		existingNames := make([]string, 0, len(existingNotifications))
		for _, n := range existingNotifications {
			if n.NotifyURL != "" {
				existingURLs = append(existingURLs, n.NotifyURL)
			}
			if n.Name != "" {
				existingNames = append(existingNames, n.Name)
			}
		}
		if len(existingURLs) > 0 || len(existingNames) > 0 {
			if err := cs.DeleteNotifyURLs(channelID, existingURLs, existingNames); err != nil {
				return fmt.Errorf("failed to delete existing notifications: %w", err)
			}
		}
		if len(notifications) > 0 {
			if err := cs.AddNotifyURLs(channelID, notifications); err != nil {
				return fmt.Errorf("failed to add notification URLs: %w", err)
			}
		}
		return nil
	}); err != nil {
		logger.Pl.E("Failed to update channel %d: %v", channelID, err)
		http.Error(w, err.Error(), channelErrStatus(err))
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("Channel updated successfully")); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleDeleteChannel deletes a channel from Tubarr.
func (ss *serverStore) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, found, err := ss.cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	// Delete associated downloads from state manager.
	if err := ss.cs.DeleteChannel(consts.QChanID, id); err != nil {
		logger.Pl.E("Failed to delete channel %q: %v", id, err)
		http.Error(w, fmt.Sprintf("failed to delete channel %q: %v", id, err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetAllVideos retrieves all videos, ignored or finished, for a given channel.
func (ss *serverStore) handleGetAllVideos(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel nil or not found", http.StatusNotFound)
		return
	}

	// Get video downloads with full metadata.
	videos, _, err := ss.cs.GetDownloadedOrIgnoredVideos(c)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve downloaded videos for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(videos); err != nil {
		logger.Pl.E("Failed to encode videos: %v", err)
	}
}

// handleDeleteChannelVideos deletes given video entries from a channel.
func (ss *serverStore) handleDeleteChannelVideos(w http.ResponseWriter, r *http.Request) {
	// Get channel ID from URL path
	idStr := chi.URLParam(r, "id")
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

	if err := ss.cs.DeleteVideosByURLs(id, urls); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete video URLs: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleGetDownloads retrieves active downloads for all channels.
func (ss *serverStore) handleGetDownloads(w http.ResponseWriter, _ *http.Request) {
	channels, found, err := ss.cs.GetAllChannels(false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		// No channels found, return empty array
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]any{}); err != nil {
			logger.Pl.E("Failed to encode empty downloads array: %v", err)
		}
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
			videoMap[consts.QVidDLStatus] = video.DownloadStatus

			allDownloads = append(allDownloads, videoMap)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(allDownloads); err != nil {
		logger.Pl.E("Could not encode videos to JSON: %v", err)
	}
}

// handleCancelDownload cancels an active download by video ID.
func (ss *serverStore) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	videoIDStr := chi.URLParam(r, "videoID")
	videoID, err := strconv.ParseInt(videoIDStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video ID %q: %v", videoIDStr, err), http.StatusBadRequest)
		return
	}

	if err := ss.cancelDownload(videoID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNoActiveDownload) {
			status = http.StatusNotFound
		}
		http.Error(w, fmt.Sprintf("failed to cancel download: %v", err), status)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message": "Download canceled successfully"}`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleCancelDownloads cancels several downloads by video ID.
func (ss *serverStore) handleCancelDownloads(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VideoIDs []int64 `json:"video_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if len(body.VideoIDs) == 0 {
		http.Error(w, "no video IDs given", http.StatusBadRequest)
		return
	}

	canceled := 0
	for _, id := range body.VideoIDs {
		if err := ss.cancelDownload(id); err != nil {
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
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int{"canceled": canceled}); err != nil {
		logger.Pl.E("Failed to encode cancel response: %v", err)
	}
}

// handleCrawlChannel initiates a crawl for a specific channel.
func (ss *serverStore) handleCrawlChannel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	c, found, err := ss.cs.GetChannelModel(consts.QChanID, idStr, true)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel nil or not found", http.StatusNotFound)
		return
	}

	// Start crawl in background.
	if crawlCtx, unlock, ok := state.TryLockCrawl(ss.ctx, c.Name); ok {
		// Clean expired blocks first.
		if err := blocking.CleanExpiredBlocks(ss.db); err != nil {
			logger.Pl.W("Failed to clean expired blocks: %v", err)
		}

		//nolint:contextcheck
		go func(ctx context.Context) {
			defer unlock()
			logger.Pl.I("Starting crawl for channel %q (ID: %d) via web request", c.Name, id)

			if performedCrawls, err := app.CrawlChannel(ctx, ss.s, c); err != nil {
				logger.Pl.E("Encountered errors crawling channel %q: %v", c.Name, err)
			} else if performedCrawls {
				logger.Pl.S("Completed crawl for channel %q", c.Name)
			}
		}(crawlCtx)

		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(`{"message": "Channel crawl started"}`)); err != nil {
			logger.Pl.E("Failed to write response message: %v", err)
		}
		return
	}
	logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)

	w.WriteHeader(http.StatusConflict)
	if _, err := w.Write([]byte(`{"message": "Channel crawl already running for channel"}`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleStopQueue stops a channel's job from starting queued work, letting in-progress downloads and Metarr processes finish.
func (ss *serverStore) handleStopQueue(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromURL(w, r)
	if !ok {
		return
	}
	if !state.StopQueue(c.Name) {
		http.Error(w, "no crawl or download running for channel", http.StatusConflict)
		return
	}

	logger.Pl.I("Stopping queued work for channel %q via web request", c.Name)
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte(`Stopping.`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleAbortAll stops all of a channel's work, killing in-progress downloads and Metarr processes.
func (ss *serverStore) handleAbortAll(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromURL(w, r)
	if !ok {
		return
	}
	aborted := state.AbortAll(c.Name)
	n := app.StopMetarr(c.ID, nil)
	if !aborted && n == 0 {
		http.Error(w, "nothing running for channel", http.StatusConflict)
		return
	}

	logger.Pl.I("Aborting all work for channel %q via web request (%d Metarr process(es))", c.Name, n)
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte(`Stopping.`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleStopMetarr stops a channel's queued and running Metarr processes, or only those for the video IDs in the body.
func (ss *serverStore) handleStopMetarr(w http.ResponseWriter, r *http.Request) {
	c, ok := ss.channelFromURL(w, r)
	if !ok {
		return
	}
	var body struct {
		VideoIDs []int64 `json:"video_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	n := app.StopMetarr(c.ID, body.VideoIDs)
	if n == 0 {
		http.Error(w, "no matching Metarr processes running for channel", http.StatusConflict)
		return
	}

	logger.Pl.I("Stopping %d Metarr process(es) for channel %q via web request", n, c.Name)
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte(`Stopping.`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleGetActivity returns the names of channels with running crawls/downloads, crawls being stopped, and Metarr processes.
func (ss *serverStore) handleGetActivity(w http.ResponseWriter, _ *http.Request) {
	crawling, stopping := state.CrawlActivity()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string][]string{
		"crawling": crawling,
		"stopping": stopping,
		"metarr":   app.MetarrActivity(),
	}); err != nil {
		logger.Pl.E("Failed to encode activity: %v", err)
	}
}

// handleGetPostProcessing returns the videos queued for or running in Metarr.
func (ss *serverStore) handleGetPostProcessing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(app.MetarrJobs()); err != nil {
		logger.Pl.E("Failed to encode post-processes: %v", err)
	}
}

// channelFromURL loads the channel given by the URL's ID, writing an error response if it can't.
func (ss *serverStore) channelFromURL(w http.ResponseWriter, r *http.Request) (*models.Channel, bool) {
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, chi.URLParam(r, "id"), false)
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

// handleIgnoreCrawlChannel initiates a crawl for a specific channel.
func (ss *serverStore) handleIgnoreCrawlChannel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	c, found, err := ss.cs.GetChannelModel(consts.QChanID, idStr, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel nil or not found", http.StatusNotFound)
		return
	}

	// Start crawl in background.
	if crawlCtx, unlock, ok := state.TryLockCrawl(ss.ctx, c.Name); ok {
		// Clean expired blocks first.
		if err := blocking.CleanExpiredBlocks(ss.db); err != nil {
			logger.Pl.W("Failed to clean expired blocks: %v", err)
		}

		//nolint:contextcheck
		go func(ctx context.Context) {
			defer unlock()
			logger.Pl.I("Starting ignore crawl for channel %q (ID: %d) via web request", c.Name, id)

			if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
				logger.Pl.E("Failed to run ignore crawl for channel %q: %v", c.Name, err)
			} else {
				logger.Pl.S("Successfully completed ignore crawl for channel %q", c.Name)
			}
		}(crawlCtx)

		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(`{"message": "Channel ignore crawl started"}`)); err != nil {
			logger.Pl.E("Failed to write response message: %v", err)
		}
		return
	}
	logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)

	w.WriteHeader(http.StatusConflict)
	if _, err := w.Write([]byte(`{"message": "Channel ignore crawl already running for channel"}`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleDownloadURLs downloads specified video URLs to a channel.
func (ss *serverStore) handleDownloadURLs(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	// Parse request body.
	var requestBody struct {
		URLs []string `json:"urls"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if len(requestBody.URLs) == 0 {
		http.Error(w, "no URLs provided", http.StatusBadRequest)
		return
	}

	// Get channel.
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, idStr, true)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	// Start download in background, once any running crawl or download of the channel finishes.
	queued := state.CrawlStateActive(c.Name)
	//nolint:contextcheck
	go func() {
		ctx, unlock, err := state.LockCrawl(ss.ctx, c.Name)
		if err != nil {
			logger.Pl.W("Manual download of %d URL(s) for channel %q did not start: %v", len(requestBody.URLs), c.Name, err)
			return
		}
		defer unlock()
		logger.Pl.I("Starting manual download of %d URL(s) for channel %q (ID: %d) via web request", len(requestBody.URLs), c.Name, id)

		if err := app.ManualDownloadToChannel(ctx, ss.s, ss.cs, c, requestBody.URLs); err != nil {
			logger.Pl.E("Encountered errors downloading URLs for channel %q: %v", c.Name, err)
		} else {
			logger.Pl.S("Successfully completed manual download for channel %q", c.Name)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]bool{"queued": queued}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetAvailableVideos returns videos awaiting the user's choice to download them, across all channels.
//
// Failed and canceled videos are only included from channel URLs which don't auto download, as others retry on crawls.
func (ss *serverStore) handleGetAvailableVideos(w http.ResponseWriter, _ *http.Request) {
	videos, err := ss.vs.GetAvailableVideos(app.AvailableStatuses...)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve available videos: %v", err), http.StatusInternalServerError)
		return
	}
	channels, _, err := ss.cs.GetAllChannels(true)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	autoDownloads := make(map[int64]bool)
	for _, c := range channels {
		for _, cu := range c.URLModels {
			autoDownloads[cu.ID] = c.AutoDownloads(cu)
		}
	}
	out := make([]*models.AvailableVideo, 0, len(videos))
	for _, v := range videos {
		if v.DownloadStatus.Status != consts.DLStatusAvailable && autoDownloads[v.ChannelURLID] {
			continue
		}
		out = append(out, v)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		logger.Pl.E("Failed to encode available videos: %v", err)
	}
}

// handleAvailableAction downloads or dismisses a channel's chosen Available videos.
//
// Downloads start once any running crawl or download of the channel finishes, so the channel's Stop Queue and Abort All buttons cover them.
func (ss *serverStore) handleAvailableAction(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		Action   string  `json:"action"`
		VideoIDs []int64 `json:"video_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if len(requestBody.VideoIDs) == 0 {
		http.Error(w, "no video IDs provided", http.StatusBadRequest)
		return
	}

	var status consts.DownloadStatus
	switch requestBody.Action {
	case "download":
		status = consts.DLStatusQueued
	case "dismiss":
		status = consts.DLStatusIgnored
	default:
		http.Error(w, fmt.Sprintf("unknown action %q (expected 'download' or 'dismiss')", requestBody.Action), http.StatusBadRequest)
		return
	}

	c, found, err := ss.cs.GetChannelModel(consts.QChanID, chi.URLParam(r, "id"), true)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	claimed, err := ss.vs.ClaimVideos(c.ID, requestBody.VideoIDs, app.AvailableStatuses, status)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to update videos: %v", err), http.StatusInternalServerError)
		return
	}

	if status == consts.DLStatusIgnored && len(claimed) > 0 {
		dismissedURLs := make([]string, 0, len(claimed))
		for _, v := range claimed {
			dismissedURLs = append(dismissedURLs, v.URL)
		}
		if err := ss.cs.RemoveNewVideoURLs(c.ID, dismissedURLs); err != nil {
			logger.Pl.E("Failed to update new video URLs for channel %q: %v", c.Name, err)
		}
	}

	if status == consts.DLStatusQueued && len(claimed) > 0 {
		//nolint:contextcheck
		go func() {
			logger.Pl.I("Starting download of %d available video(s) for channel %q via web request", len(claimed), c.Name)
			if err := app.DownloadAvailable(ss.ctx, ss.s, c, claimed); err != nil {
				logger.Pl.E("Encountered errors downloading available videos for channel %q: %v", c.Name, err)
			} else {
				logger.Pl.S("Completed download of available videos for channel %q", c.Name)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]int{"count": len(claimed)}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleRunMetarr runs Metarr on the given videos, or all pending/failed videos if none are given.
func (ss *serverStore) handleRunMetarr(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")

	var requestBody struct {
		VideoIDs []int64 `json:"video_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	c, found, err := ss.cs.GetChannelModel(consts.QChanID, idStr, true)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	//nolint:contextcheck
	go func(ctx context.Context) {
		file.UpdateFromConfigFile(ss.cs, c)
		if err := app.RunMetarr(ctx, ss.s, c, requestBody.VideoIDs); err != nil {
			logger.Pl.E("Encountered errors running Metarr for channel %q: %v", c.Name, err)
		} else {
			logger.Pl.S("Completed Metarr run for channel %q", c.Name)
		}
	}(ss.ctx)

	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte(`Metarr run started.`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleLatestDownloads retrieves the latest downloads for a given channel.
func (ss *serverStore) handleLatestDownloads(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel nil or not found", http.StatusNotFound)
		return
	}

	// Get video downloads with full metadata.
	videos, err := ss.getHomepageCarouselVideos(c, 10)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve downloaded videos for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(videos); err != nil {
		logger.Pl.E("Could not encode videos to JSON: %v", err)
	}
}

// handleSetLogLevel sets the logging level in the database.
func (ss *serverStore) handleSetLogLevel(w http.ResponseWriter, r *http.Request) {
	levelStr := chi.URLParam(r, "level")
	level, err := strconv.Atoi(levelStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not set logging level using input %q: %v", levelStr, err), http.StatusBadRequest)
		return
	}
	level = min(max(level, -1), 5)
	logging.SetLevel(level)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(fmt.Appendf(nil, "Logging level set to %d", level)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleHealth reports whether the server and database are working (not whether jobs are running).
func (ss *serverStore) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	status := "ok"
	w.Header().Set("Content-Type", "application/json")
	if err := ss.db.PingContext(ctx); err != nil {
		logger.Pl.E("Health check failed, database unreachable: %v", err)
		status = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(map[string]string{"status": status}); err != nil {
		logger.Pl.E("Could not encode health status to JSON: %v", err)
	}
}

// handleGetIdle reports running jobs, returning 503 if any are running.
func (ss *serverStore) handleGetIdle(w http.ResponseWriter, _ *http.Request) {
	running := app.RunningProcesses()
	w.Header().Set("Content-Type", "application/json")
	if running > 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(map[string]int32{"running": running}); err != nil {
		logger.Pl.E("Could not encode running jobs to JSON: %v", err)
	}
}

// handleGetLogLevel retrieves the current logging level.
func (ss *serverStore) handleGetLogLevel(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int{"level": logging.Level()}); err != nil {
		logger.Pl.E("Could not encode log level to JSON: %v", err)
	}
}

// handleGetTubarrLogs serves the log entries from memory.
func (ss *serverStore) handleGetTubarrLogs(w http.ResponseWriter, r *http.Request) {
	logs := logging.GetRecentLogsForProgram("Tubarr")
	if logs == nil {
		http.Error(w, "tubarr logger not initialized", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	for _, line := range logs {
		if len(line) == 0 {
			continue
		}

		// Check if client disconnected.
		if r.Context().Err() != nil {
			return
		}

		if _, err := w.Write(line); err != nil {
			// Client disconnected or network error.
			return
		}
	}
}

// handleGetMetarrLogs serves the Metarr log entries from memory.
func (ss *serverStore) handleGetMetarrLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Copy the lines under the lock, so a slow client doesn't hold up incoming logs (lines are never modified once stored).
	vars.MetarrLogsMutex.RLock()
	logs := slices.Clone(vars.MetarrLogs)
	vars.MetarrLogsMutex.RUnlock()

	for _, line := range logs {
		// Check if client disconnected.
		if r.Context().Err() != nil {
			return
		}

		if _, err := w.Write(line); err != nil {
			// Client disconnected or network error.
			return
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
func (ss *serverStore) handleGetBlockedDomains(w http.ResponseWriter, _ *http.Request) {
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Pl.E("Failed to encode blocked domains: %v", err)
	}
}

// handleUnblockDomain unblocks a domain for all contexts or a specific context.
//
// Context can be specified via query parameter: ?context=unauth|cookie|auth
func (ss *serverStore) handleUnblockDomain(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	contextParam := r.URL.Query().Get("context")

	if domain == "" {
		http.Error(w, "domain parameter is required", http.StatusBadRequest)
		return
	}

	var ctx vars.BlockContext
	if contextParam != "" {
		ctx = vars.BlockContext(contextParam)
		// Validate context.
		if ctx != vars.BlockContextUnauth && ctx != vars.BlockContextCookie && ctx != vars.BlockContextAuth {
			http.Error(w, fmt.Sprintf("invalid context %q (must be one of: unauth, cookie, auth)", contextParam), http.StatusBadRequest)
			return
		}
	}

	// Unblock the domain.
	if err := blocking.UnblockDomain(ss.db, domain, ctx); err != nil {
		http.Error(w, fmt.Sprintf("failed to unblock domain: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message": "Domain unblocked successfully"}`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleNewVideoNotificationSeen marks a new video notification as seen.
func (ss *serverStore) handleNewVideoNotificationSeen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || c == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
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
	idStr := chi.URLParam(r, "id")
	_, found, err := ss.cs.GetChannelModel(consts.QChanID, idStr, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	// Track the new pause state (set inside the update function).
	var newPauseState bool

	// Update channel settings with toggled pause state.
	_, err = ss.cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
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
	var action string
	if newPauseState {
		action = "paused"
	} else {
		action = "resumed"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, `{"message": "Channel %s successfully.", "paused": %t}`, action, newPauseState); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleGetCrawlConcurrency returns the current crawl concurrency limit.
func (ss *serverStore) handleGetCrawlConcurrency(w http.ResponseWriter, _ *http.Request) {
	limit := viper.GetInt(keys.CrawlConcurrency)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int{"limit": limit}); err != nil {
		logger.Pl.E("Failed to encode crawl concurrency: %v", err)
	}
}

// handleSetCrawlConcurrency updates the crawl concurrency limit.
func (ss *serverStore) handleSetCrawlConcurrency(w http.ResponseWriter, r *http.Request) {
	limitStr := r.FormValue("limit")
	if limitStr == "" {
		http.Error(w, "limit parameter is required", http.StatusBadRequest)
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid limit value %q: %v", limitStr, err), http.StatusBadRequest)
		return
	}

	if limit < 0 {
		http.Error(w, "limit must be 0 or greater", http.StatusBadRequest)
		return
	}

	// Persist to DB and update viper (takes effect on next crawl cycle).
	if err := ss.tss.SetSetting(keys.CrawlConcurrency, limitStr); err != nil {
		logger.Pl.W("Failed to persist crawl concurrency to DB: %v", err)
	}
	viper.Set(keys.CrawlConcurrency, limit)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"message": "Crawl concurrency updated", "limit": limit}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetGlobalDownloadConcurrency returns the current global download concurrency limit.
func (ss *serverStore) handleGetGlobalDownloadConcurrency(w http.ResponseWriter, _ *http.Request) {
	limit := viper.GetInt(keys.GlobalDownloadConcurrency)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int{"limit": limit}); err != nil {
		logger.Pl.E("Failed to encode global download concurrency: %v", err)
	}
}

// handleSetGlobalDownloadConcurrency updates the global download concurrency limit.
func (ss *serverStore) handleSetGlobalDownloadConcurrency(w http.ResponseWriter, r *http.Request) {
	limitStr := r.FormValue("limit")
	if limitStr == "" {
		http.Error(w, "limit parameter is required", http.StatusBadRequest)
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid limit value %q: %v", limitStr, err), http.StatusBadRequest)
		return
	}

	if limit < 0 {
		http.Error(w, "limit must be 0 or greater", http.StatusBadRequest)
		return
	}

	// Persist to DB, update viper, and reinitialize the semaphore.
	if err := ss.tss.SetSetting(keys.GlobalDownloadConcurrency, limitStr); err != nil {
		logger.Pl.W("Failed to persist global download concurrency to DB: %v", err)
	}
	viper.Set(keys.GlobalDownloadConcurrency, limit)
	downloads.InitGlobalDownloadLimit(limit)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"message": "Global download concurrency updated", "limit": limit}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetPostProcessConcurrency returns the global post-processing (Metarr) concurrency limit.
func (ss *serverStore) handleGetPostProcessConcurrency(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int{"limit": viper.GetInt(keys.PostProcessConcurrency)}); err != nil {
		logger.Pl.E("Failed to encode post-processing concurrency: %v", err)
	}
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"message": "Post-processing concurrency updated", "limit": limit}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetDomainLimits returns all per-domain download and crawl concurrency limits.
func (ss *serverStore) handleGetDomainLimits(w http.ResponseWriter, _ *http.Request) {
	limits, err := ss.tss.GetDomainLimits()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get domain limits: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(limits); err != nil {
		logger.Pl.E("Failed to encode domain limits: %v", err)
	}
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"message": "Domain limit updated", "hostname": hostname, "download": limit.Download, "crawl": limit.Crawl}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleDeleteDomainLimit removes the download and crawl concurrency limits for a specific domain.
func (ss *serverStore) handleDeleteDomainLimit(w http.ResponseWriter, r *http.Request) {
	hostname := chi.URLParam(r, "hostname")
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"message": "Domain limit removed", "hostname": hostname}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetScrapeConfigFile returns the currently configured custom scrape site config file path.
func (ss *serverStore) handleGetScrapeConfigFile(w http.ResponseWriter, _ *http.Request) {
	path := viper.GetString(keys.ScrapeConfigFile)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"path": path}); err != nil {
		logger.Pl.E("Failed to encode scrape config file path: %v", err)
	}
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

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"message": "Scrape config file updated", "path": path, "sites": len(sites)}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetFlareSolverrURL returns the currently configured FlareSolverr URL.
func (ss *serverStore) handleGetFlareSolverrURL(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"url": viper.GetString(keys.FlareSolverrURL)}); err != nil {
		logger.Pl.E("Failed to encode FlareSolverr URL: %v", err)
	}
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"message": "FlareSolverr URL updated", "url": fsURL}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetTLS returns the HTTPS state and custom certificate paths.
func (ss *serverStore) handleGetTLS(w http.ResponseWriter, _ *http.Request) {
	certPath, keyPath, certLocked := tlsPaths(ss.tss)
	enabled, locked := httpsSetting(ss.tss)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"https":       ss.certs != nil, // Serving HTTPS now.
		"enabled":     enabled,         // Serving HTTPS after a restart.
		"locked":      locked,
		"cert":        certPath,
		"key":         keyPath,
		"cert_locked": certLocked,
		"active":      ss.certs.active(),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode TLS settings: %v", err)
	}
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
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"message": msg, "active": ss.certs.active()}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
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
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"message": msg, "enabled": enabled}); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
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

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"status": status, "user_agent": userAgent, "cookies": cookies, "seconds": elapsed.Seconds()}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleReloadScrapeConfigFile re-parses the currently configured scrape config file from disk,
// without changing which file is configured. Useful if the file was edited and saved while Tubarr is running.
func (ss *serverStore) handleReloadScrapeConfigFile(w http.ResponseWriter, _ *http.Request) {
	path := viper.GetString(keys.ScrapeConfigFile)
	if path == "" {
		siterules.Reset()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"message": "No scrape config file set; using built-in defaults", "sites": 0}); err != nil {
			logger.Pl.E("Failed to encode response: %v", err)
		}
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

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"message": "Scrape config file reloaded", "path": path, "sites": len(sites)}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// handleGetScrapeConfigContent returns the configured scrape config file's contents, for editing in the web UI.
//
// Includes a version (hash of the contents), so a later save can detect edits made elsewhere in the meantime.
func (ss *serverStore) handleGetScrapeConfigContent(w http.ResponseWriter, _ *http.Request) {
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode scrape config file content: %v", err)
	}
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

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"message": "Scrape config file saved", "sites": len(sites), "version": contentVersion([]byte(content))}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode response: %v", err)
	}
}

// contentVersion returns a version string for file contents, which changes whenever the contents do.
func contentVersion(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// handleGetScrapeSites returns every currently registered scrape site (built-in and custom)
// along with its selectors, for display in the web UI.
func (ss *serverStore) handleGetScrapeSites(w http.ResponseWriter, _ *http.Request) {
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode scrape sites: %v", err)
	}
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

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"matched": matched, "site": site, "metadata": metadata}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode scrape test response: %v", err)
	}
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
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"matched": matched, "source": source, "urls": urls, "videos": videos}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Pl.E("Failed to encode crawl test response: %v", err)
	}
}
