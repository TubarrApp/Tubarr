package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/auth"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/state"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/viper"
)

// handleAddChannelFromFile adds a new channel from a specified config file path using Viper.
func (ss *serverStore) handleAddChannelFromFile(w http.ResponseWriter, r *http.Request) {
	var input models.ChannelInputPtrs

	// Grab config filepath from form.
	addFromFile := r.FormValue("add_from_config_file")
	if addFromFile == "" {
		http.Error(w, "no config file entered, could not add new channel", http.StatusBadRequest)
		return
	}

	// Use local viper instance for consistency with directory handler.
	v := viper.New()
	if err := file.LoadConfigFile(v, addFromFile); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Load viper variables into the struct from local instance.
	if err := parsing.LoadViperIntoStruct(v, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings if present.
	urlSettings, err := parsing.ParseURLSettingsFromViper(v)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse url-settings: %v", err), http.StatusBadRequest)
		return
	}
	input.URLSettings = urlSettings

	// Fill channel from config file input.
	c, authMap := fillChannelFromConfigFile(w, input)
	if c == nil {
		return
	}

	// Add channel to database.
	channelID, err := ss.cs.AddChannel(c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.ID = channelID

	if len(authMap) > 0 {
		if err := ss.cs.AddAuth(channelID, authMap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Add notifications if present.
	if input.Notification != nil && len(*input.Notification) != 0 {
		v, err := parsing.ParseNotifications(*input.Notification)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := ss.cs.AddNotifyURLs(channelID, v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Ignore run if desired.
	if input.IgnoreRun != nil && *input.IgnoreRun {
		if !state.CrawlStateActive(c.Name) {
			state.LockCrawlState(c.Name)

			//nolint:contextcheck
			go func(ctx context.Context) {
				defer state.UnlockCrawlState(c.Name)
				logger.Pl.I("Starting ignore crawl for channel %q via web request", c.Name)

				if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
					logger.Pl.E("Failed to run ignore crawl for channel %q: %v", c.Name, err)
				} else {
					logger.Pl.S("Successfully completed ignore crawl for channel %q", c.Name)
				}
			}(ss.ctx)
		} else {
			logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
		}
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

	// Track results
	successes := make([]string, 0, len(batchConfigFiles))
	failures := make([]struct {
		file   string
		reason string
	}, 0, len(batchConfigFiles))

	logger.Pl.I("Adding channels from config files %v", batchConfigFiles)

	for _, batchConfigFile := range batchConfigFiles {
		var input models.ChannelInputPtrs
		v := viper.New()

		// Load in config file
		if err := file.LoadConfigFile(v, batchConfigFile); err != nil {
			failures = append(failures, struct {
				file   string
				reason string
			}{batchConfigFile, err.Error()})
			continue
		}

		// Load viper variables into the struct from local instance
		if err := parsing.LoadViperIntoStruct(v, &input); err != nil {
			failures = append(failures, struct {
				file   string
				reason string
			}{batchConfigFile, err.Error()})
			continue
		}

		// Parse per-URL settings if present
		urlSettings, err := parsing.ParseURLSettingsFromViper(v)
		if err != nil {
			failures = append(failures, struct {
				file   string
				reason string
			}{batchConfigFile, fmt.Sprintf("failed to parse url-settings: %v", err)})
			continue
		}
		input.URLSettings = urlSettings

		c, authMap := fillChannelFromConfigFile(w, input)
		if c == nil {
			failures = append(failures, struct {
				file   string
				reason string
			}{batchConfigFile, "channel returned nil"})
			continue
		}

		channelID, err := ss.cs.AddChannel(c)
		if err != nil {
			failures = append(failures, struct {
				file   string
				reason string
			}{batchConfigFile, err.Error()})
			continue
		}

		c.ID = channelID

		if len(authMap) > 0 {
			if err := ss.cs.AddAuth(channelID, authMap); err != nil {
				failures = append(failures, struct {
					file   string
					reason string
				}{batchConfigFile, err.Error()})
				continue
			}
		}

		if input.Notification != nil && len(*input.Notification) != 0 {
			v, err := parsing.ParseNotifications(*input.Notification)
			if err != nil {
				failures = append(failures, struct {
					file   string
					reason string
				}{batchConfigFile, err.Error()})
				continue
			}

			if err := ss.cs.AddNotifyURLs(channelID, v); err != nil {
				failures = append(failures, struct {
					file   string
					reason string
				}{batchConfigFile, err.Error()})
				continue
			}
		}

		// Ignore run if desired.
		if input.IgnoreRun != nil && *input.IgnoreRun {
			if !state.CrawlStateActive(c.Name) {
				state.LockCrawlState(c.Name)

				//nolint:contextcheck
				go func(ctx context.Context) {
					defer state.UnlockCrawlState(c.Name)
					logger.Pl.I("Starting ignore crawl for channel %q via web request", c.Name)

					if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
						logger.Pl.E("Failed to run ignore crawl for channel %q: %v", c.Name, err)
					} else {
						logger.Pl.S("Successfully completed ignore crawl for channel %q", c.Name)
					}
				}(ss.ctx)
			} else {
				logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
			}
		}

		// Track success
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

	// Build response with properly formatted data (consistent with handleGetChannel)
	response := make([]map[string]any, 0, len(channels))
	for _, c := range channels {
		channelMap := make(map[string]any)
		channelMap["id"] = c.ID
		channelMap["name"] = c.Name
		channelMap["urls"] = c.GetURLs()
		channelMap["last_scan"] = c.LastScan
		channelMap["created_at"] = c.CreatedAt
		channelMap["updated_at"] = c.UpdatedAt
		channelMap["channel_config_file"] = c.ChannelConfigFile
		channelMap["new_video_notification"] = c.NewVideoNotification

		// Convert settings to map with string representations
		if c.ChanSettings != nil {
			channelMap["settings"] = settingsJSONMap(c.ChanSettings)
		}

		// Convert metarr args to map with string representations
		if c.ChanMetarrArgs != nil {
			channelMap["metarr"] = metarrArgsJSONMap(c.ChanMetarrArgs)
		}

		// Include URL models with their settings converted
		if len(c.URLModels) > 0 {
			urlModels := make([]map[string]any, 0, len(c.URLModels))
			for _, urlModel := range c.URLModels {
				urlMap := make(map[string]any)
				urlMap["id"] = urlModel.ID
				urlMap["url"] = urlModel.URL
				urlMap["username"] = urlModel.Username
				urlMap["login_url"] = urlModel.LoginURL

				if urlModel.ChanURLSettings != nil {
					urlMap["settings"] = settingsJSONMap(urlModel.ChanURLSettings)
				}
				if urlModel.ChanURLMetarrArgs != nil {
					urlMap["metarr"] = metarrArgsJSONMap(urlModel.ChanURLMetarrArgs)
				}
				urlModels = append(urlModels, urlMap)
			}
			channelMap["url_models"] = urlModels
		}

		response = append(response, channelMap)
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

	// Build response with properly formatted data for the edit form
	response := make(map[string]any)
	response[consts.QChanID] = c.ID
	response[consts.QChanName] = c.Name
	response["urls"] = c.GetURLs()
	response[consts.QChanLastScan] = c.LastScan
	response[consts.QChanCreatedAt] = c.CreatedAt
	response[consts.QChanUpdatedAt] = c.UpdatedAt

	// Convert global settings to map with string representations
	if c.ChanSettings != nil {
		settingsMap := settingsJSONMap(c.ChanSettings)
		response[consts.QChanSettings] = settingsMap
	}

	// Convert global metarr args to map with string representations
	if c.ChanMetarrArgs != nil {
		metarrMap := metarrArgsJSONMap(c.ChanMetarrArgs)
		response[consts.QChanMetarr] = metarrMap
	}

	// Build auth_details array
	authDetails := make([]map[string]string, 0)
	for _, urlModel := range c.URLModels {
		if urlModel.Username != "" || urlModel.LoginURL != "" {
			authDetails = append(authDetails, map[string]string{
				"channel_url": urlModel.URL,
				"username":    urlModel.Username,
				"password":    urlModel.Password,
				"login_url":   urlModel.LoginURL,
			})
		}
	}
	response["auth_details"] = authDetails

	// Build url_settings map.
	urlSettings := make(map[string]map[string]any)
	for _, urlModel := range c.URLModels {
		if urlModel.ChanURLSettings != nil || urlModel.ChanURLMetarrArgs != nil {
			urlSettings[urlModel.URL] = make(map[string]any)

			// Add settings with display strings
			if urlModel.ChanURLSettings != nil {
				settingsMap := settingsJSONMap(urlModel.ChanURLSettings)
				urlSettings[urlModel.URL][consts.QChanURLSettings] = settingsMap
			}

			// Add metarr with display strings
			if urlModel.ChanURLMetarrArgs != nil {
				metarrMap := metarrArgsJSONMap(urlModel.ChanURLMetarrArgs)
				urlSettings[urlModel.URL][consts.QChanURLMetarr] = metarrMap
			}
		}
	}
	response["url_settings"] = urlSettings

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
	authDetails := strings.Fields(r.FormValue("auth_details"))
	username := r.FormValue("username")
	loginURL := r.FormValue("login_url")
	password := r.FormValue("password")
	now := time.Now()

	// Parse authentication details.
	authMap, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, urls, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid authentication details: %v", err), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings JSON if provided.
	urlSettingsRaw := make(map[string]map[string]map[string]any)
	if urlSettingsJSON := r.FormValue("url_settings"); urlSettingsJSON != "" {
		if err := json.Unmarshal([]byte(urlSettingsJSON), &urlSettingsRaw); err != nil {
			http.Error(w, fmt.Sprintf("invalid url_settings JSON: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Parse the URL settings into proper structures.
	urlSettingsMap := make(map[string]struct {
		Settings *models.Settings
		Metarr   *models.MetarrArgs
	})
	for channelURL, settingsData := range urlSettingsRaw {
		parsed := struct {
			Settings *models.Settings
			Metarr   *models.MetarrArgs
		}{}

		if settingsMap, hasSettings := settingsData["settings"]; hasSettings {
			if parsed.Settings, err = parseSettingsFromMap(settingsMap); err != nil {
				http.Error(w, fmt.Sprintf("Invalid Settings for URL %q: %v", channelURL, err), http.StatusBadRequest)
				return
			}
		}
		if metarrMap, hasMetarr := settingsData["metarr"]; hasMetarr {
			if parsed.Metarr, err = parseMetarrArgsFromMap(metarrMap); err != nil {
				http.Error(w, fmt.Sprintf("Invalid Metarr Args for URL %q: %v", channelURL, err), http.StatusBadRequest)
				return
			}
		}

		urlSettingsMap[channelURL] = parsed
	}

	// Validate and create ChannelURL models.
	var chanURLs = make([]*models.ChannelURL, 0, len(urls))
	for _, u := range urls {
		if u != "" {
			if _, err := url.Parse(u); err != nil {
				http.Error(w, fmt.Sprintf("invalid channel URL %q: %v", u, err), http.StatusBadRequest)
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
		c.ChanSettings = &models.Settings{}
	}

	c.ChanMetarrArgs = getMetarrArgsStrings(w, r)
	if c.ChanMetarrArgs == nil {
		c.ChanMetarrArgs = &models.MetarrArgs{}
	}

	// Add to database.
	if c.ID, err = ss.cs.AddChannel(c); err != nil {
		http.Error(w, fmt.Sprintf("failed to add channel with name %q: %v", name, err), http.StatusInternalServerError)
		return
	}

	// Ignore run if desired.
	if r.FormValue("ignore_run") == "true" {
		if !state.CrawlStateActive(c.Name) {
			logger.Pl.I("Running ignore crawl for channel %q. No videos before this point will be downloaded to this channel.", c.Name)
			state.LockCrawlState(c.Name)

			//nolint:contextcheck
			go func(ctx context.Context) {
				defer state.UnlockCrawlState(c.Name)
				logger.Pl.I("Starting ignore crawl for channel %q via web request", c.Name)

				if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
					logger.Pl.E("Failed to run ignore crawl for channel %q: %v", c.Name, err)
				} else {
					logger.Pl.S("Successfully completed ignore crawl for channel %q", c.Name)
				}
			}(ss.ctx)
		} else {
			logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
		}
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

	// Get existing channel.
	existingChannel, found, err := ss.cs.GetChannelModel(consts.QChanID, idStr, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || existingChannel == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	// Update channel name if provided.
	name := r.FormValue("name")
	if name != "" && name != existingChannel.Name {
		if err := ss.cs.UpdateChannelValue(consts.QChanID, idStr, consts.QChanName, name); err != nil {
			http.Error(w, fmt.Sprintf("failed to update channel name: %v", err), http.StatusInternalServerError)
			return
		}
		existingChannel.Name = name
	}

	// Update channel settings.
	newSettings := getSettingsStrings(w, r)
	if newSettings != nil {
		_, err = ss.cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
			*s = *newSettings
			return nil
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to update channel settings: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Update channel Metarr args.
	newMetarr := getMetarrArgsStrings(w, r)
	if newMetarr != nil {
		_, err = ss.cs.UpdateChannelMetarrArgsJSON(consts.QChanID, idStr, func(m *models.MetarrArgs) error {
			*m = *newMetarr
			return nil
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to update channel metarr args: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Handle URL updates.
	urls := strings.Fields(r.FormValue("urls"))
	authDetails := strings.Fields(r.FormValue("auth_details"))
	username := r.FormValue("username")
	loginURL := r.FormValue("login_url")
	password := r.FormValue("password")
	now := time.Now()

	// Parse authentication details.
	authMap, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, urls, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid authentication details: %v", err), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings JSON if provided.
	urlSettingsRaw := make(map[string]map[string]map[string]any)
	if urlSettingsJSON := r.FormValue("url_settings"); urlSettingsJSON != "" {
		if err := json.Unmarshal([]byte(urlSettingsJSON), &urlSettingsRaw); err != nil {
			http.Error(w, fmt.Sprintf("invalid url_settings JSON: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Parse the URL settings into proper structures.
	urlSettingsMap := make(map[string]struct {
		Settings *models.Settings
		Metarr   *models.MetarrArgs
	})
	for channelURL, settingsData := range urlSettingsRaw {
		parsed := struct {
			Settings *models.Settings
			Metarr   *models.MetarrArgs
		}{}

		if settingsMap, hasSettings := settingsData["settings"]; hasSettings {
			if parsed.Settings, err = parseSettingsFromMap(settingsMap); err != nil {
				http.Error(w, fmt.Sprintf("Invalid Settings for URL %q: %v", channelURL, err), http.StatusBadRequest)
				return
			}
		}
		if metarrMap, hasMetarr := settingsData["metarr"]; hasMetarr {
			if parsed.Metarr, err = parseMetarrArgsFromMap(metarrMap); err != nil {
				http.Error(w, fmt.Sprintf("Invalid Metarr Args for URL %q: %v", channelURL, err), http.StatusBadRequest)
				return
			}
		}

		urlSettingsMap[channelURL] = parsed
	}

	// Build map of existing URLs.
	existingURLMap := make(map[string]*models.ChannelURL)
	for _, urlModel := range existingChannel.URLModels {
		existingURLMap[urlModel.URL] = urlModel
	}

	// Build map of new URLs.
	newURLMap := make(map[string]bool)
	for _, u := range urls {
		if u != "" {
			newURLMap[u] = true
		}
	}

	// Delete URLs that are no longer in the list.
	for existingURL, urlModel := range existingURLMap {
		if !newURLMap[existingURL] {
			// URL was removed, delete it.
			if err := ss.cs.DeleteChannelURL(urlModel.ID); err != nil {
				logger.Pl.E("Failed to delete channel URL %q: %v", existingURL, err)
			}
		}
	}

	// Add or update URLs.
	for _, u := range urls {
		if u == "" {
			continue
		}

		if _, err := url.Parse(u); err != nil {
			http.Error(w, fmt.Sprintf("invalid channel URL %q: %v", u, err), http.StatusBadRequest)
			return
		}

		// Get auth details for this URL if they exist.
		var parsedUsername, parsedPassword, parsedLoginURL string
		if _, exists := authMap[u]; exists {
			parsedUsername = authMap[u].Username
			parsedPassword = authMap[u].Password
			parsedLoginURL = authMap[u].LoginURL
		}

		// Check if URL already exists.
		if existingURLModel, exists := existingURLMap[u]; exists {
			// Update existing URL.
			existingURLModel.Username = parsedUsername
			existingURLModel.Password = parsedPassword
			existingURLModel.LoginURL = parsedLoginURL
			existingURLModel.UpdatedAt = now

			// Apply per-URL custom settings if they exist, otherwise clear them.
			if urlSettings, hasCustom := urlSettingsMap[u]; hasCustom {
				existingURLModel.ChanURLSettings = urlSettings.Settings
				existingURLModel.ChanURLMetarrArgs = urlSettings.Metarr
			} else {
				// No custom settings for this URL - clear any existing ones.
				existingURLModel.ChanURLSettings = nil
				existingURLModel.ChanURLMetarrArgs = nil
			}

			if err := ss.cs.UpdateChannelURLSettings(existingURLModel); err != nil {
				http.Error(w, fmt.Sprintf("failed to update URL %q: %v", u, err), http.StatusInternalServerError)
				return
			}
		} else {
			// Add new URL.
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

			// Add the new URL to the channel.
			if _, err := ss.cs.AddChannelURL(channelID, chanURL, true); err != nil {
				http.Error(w, fmt.Sprintf("failed to add URL %q: %v", u, err), http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("Channel updated successfully")); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleDeleteChannel deletes a channel from Tubarr.
func (ss *serverStore) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Delete associated downloads from state manager.
	if err := ss.cs.DeleteChannel(consts.QChanID, id); err != nil {
		logger.Pl.E("Failed to delete channel %d: %v", id, err)
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
	bodyBytes := make([]byte, r.ContentLength)
	if _, err := r.Body.Read(bodyBytes); err != nil && err.Error() != "EOF" {
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

// handleGetDownloads retrieves active downloads for a given channel.
func (ss *serverStore) handleGetDownloads(w http.ResponseWriter, r *http.Request) {
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

	// Get active downloads with progress (filtered by channel in memory).
	videos := ss.getActiveDownloads(c)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(videos); err != nil {
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

	// Update database status first.
	if err := ss.cancelDownload(videoID); err != nil {
		http.Error(w, fmt.Sprintf("failed to cancel download: %v", err), http.StatusInternalServerError)
		return
	}

	// Cancel the actual running download process.
	var videoURL string
	if videoURL, err = ss.vs.GetVideoURLByID(videoID); err != nil {
		logger.Pl.E("Could not get video URL for ID %d: %v", videoID, err)
	}
	cancelled := ss.ds.CancelDownload(videoID, videoURL)
	if !cancelled {
		logger.Pl.W("Download for video ID %d was marked as cancelled in DB but no active download process was found", videoID)
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message": "Download cancelled successfully"}`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
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
	if !state.CrawlStateActive(c.Name) {
		state.LockCrawlState(c.Name)

		//nolint:contextcheck
		go func(ctx context.Context) {
			defer state.UnlockCrawlState(c.Name)
			logger.Pl.I("Starting crawl for channel %q (ID: %d) via web request", c.Name, id)

			if err := app.CrawlChannel(ctx, ss.s, c); err != nil {
				logger.Pl.E("Failed to crawl channel %q: %v", c.Name, err)
			} else {
				logger.Pl.S("Successfully completed crawl for channel %q", c.Name)
			}
		}(ss.ctx)

		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(`{"message": "Channel crawl started"}`)); err != nil {
			logger.Pl.E("Failed to write response message: %v", err)
		}
		return
	}
	logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)

	w.WriteHeader(http.StatusAlreadyReported)
	if _, err := w.Write([]byte(`{"message": "Channel crawl already running for channel"}`)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
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
	if !state.CrawlStateActive(c.Name) {
		state.LockCrawlState(c.Name)

		//nolint:contextcheck
		go func(ctx context.Context) {
			defer state.UnlockCrawlState(c.Name)
			logger.Pl.I("Starting ignore crawl for channel %q (ID: %d) via web request", c.Name, id)

			if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
				logger.Pl.E("Failed to run ignore crawl for channel %q: %v", c.Name, err)
			} else {
				logger.Pl.S("Successfully completed ignore crawl for channel %q", c.Name)
			}
		}(ss.ctx)

		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(`{"message": "Channel ignore crawl started"}`)); err != nil {
			logger.Pl.E("Failed to write response message: %v", err)
		}
		return
	}
	logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)

	w.WriteHeader(http.StatusAlreadyReported)
	if _, err := w.Write([]byte(`{"message": "Channel ignore crawl already running for channel"}`)); err != nil {
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
	logging.Level = level
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(fmt.Appendf(nil, "Logging level set to %d", level)); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
}

// handleGetLogLevel retrieves the current logging level.
func (ss *serverStore) handleGetLogLevel(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int{"level": logging.Level}); err != nil {
		logger.Pl.E("Could not encode log level to JSON: %v", err)
	}
}

// handleGetTubarrLogs serves the log entries from memory.
func (ss *serverStore) handleGetTubarrLogs(w http.ResponseWriter, _ *http.Request) {
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
		if _, err := w.Write(line); err != nil {
			logger.Pl.E("Failed to write Tubarr log line %q: %v", line, err)
		}
	}
}

// handleGetMetarrLogs serves the Metarr log entries from memory.
func (ss *serverStore) handleGetMetarrLogs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Reload file after Metarr exits.
	if vars.MetarrFinished {
		vars.MetarrLogs = file.LoadMetarrLogs()
		vars.MetarrFinished = false
	}

	// Print file logs first (only once per Tubarr startup).
	for _, line := range vars.MetarrLogs {
		if _, err := w.Write(line); err != nil {
			logger.Pl.E("Failed to write Metarr log line %q: %v", line, err)
		}
	}

	// Try to append live RAM logs if Metarr is running.
	resp, err := http.Get("http://127.0.0.1:6387/logs")
	if err == nil { // if err IS nil
		if resp != nil {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					logger.Pl.E("Failed to close response body for Metarr log fetch: %v", err)
				}
			}()

			if _, err := io.Copy(w, resp.Body); err != nil {
				logger.Pl.E("Failed to close response body for Metarr log fetch: %v", err)
			}
		}
	}
}

// handleNewVideoNotificationSeen marks a new video notification as seen.
func (ss *serverStore) handleNewVideoNotificationSeen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := ss.cs.UpdateChannelValue(consts.QChanID, id, consts.QChanNewVideoNotification, false); err != nil {
		http.Error(w, fmt.Sprintf("failed to mark new video notification as seen for channel ID %q: %v", id, err), http.StatusInternalServerError)
		return
	}
	// Parse form data.
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse form data: %v", err), http.StatusBadRequest)
		return
	}
}
