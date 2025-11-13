package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/auth"
	"tubarr/internal/domain/consts"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/state"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/viper"
)

// handleAddChannelFromFile adds a new channel from a specified config file path using Viper.
func handleAddChannelFromFile(w http.ResponseWriter, r *http.Request) {
	var input models.ChannelInputPtrs

	// Grab config file from form
	addFromFile := r.FormValue("add_from_config_file")
	if addFromFile == "" {
		http.Error(w, "no config file entered, could not add new channel", http.StatusBadRequest)
		return
	}

	// Load in config file
	if err := file.LoadConfigFile(addFromFile); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Load viper variables into the struct
	if err := parsing.LoadViperIntoStruct(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c, authMap := fillChannelFromConfigFile(w, input)
	if c == nil {
		return
	}

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

	if input.Notification != nil && len(*input.Notification) != 0 {
		v, err := validation.ValidateNotificationStrings(*input.Notification)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := ss.cs.AddNotifyURLs(channelID, v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	ctx := context.Background()
	if input.IgnoreRun != nil && *input.IgnoreRun {
		if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
			logging.E("Failed to complete ignore crawl run: %v", err)
		}
	}

	w.WriteHeader(http.StatusCreated)
	w.Write(fmt.Appendf(nil, "Channel %q added successfully", c.Name))
}

// handleAddChannelsFromDir adds new channel from all config files in the directory path using Viper.
func handleAddChannelsFromDir(w http.ResponseWriter, r *http.Request) {
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
		w.Write(fmt.Appendf(nil, "No config files found in directory %q", addFromDir))
		return
	}

	// Track results
	var successes []string
	var failures []struct {
		file   string
		reason string
	}

	logging.I("Adding channels from config files %v", batchConfigFiles)

	for _, batchConfigFile := range batchConfigFiles {
		var input models.ChannelInputPtrs
		v := viper.New()

		// Load in config file
		if err := file.LoadConfigFileLocal(v, batchConfigFile); err != nil {
			failures = append(failures, struct {
				file   string
				reason string
			}{batchConfigFile, err.Error()})
			continue
		}

		// Load viper variables into the struct from local instance
		if err := parsing.LoadViperIntoStructLocal(v, &input); err != nil {
			failures = append(failures, struct {
				file   string
				reason string
			}{batchConfigFile, err.Error()})
			continue
		}

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
			v, err := validation.ValidateNotificationStrings(*input.Notification)
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

		ctx := context.Background()
		if input.IgnoreRun != nil && *input.IgnoreRun {
			if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
				logging.E("Failed to complete ignore crawl run: %v", err)
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
	w.Write([]byte(responseMsg))
}

// handleListChannels lists Tubarr channels.
func handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, found, err := ss.cs.GetAllChannels(false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channels)
}

// handleGetChannel returns the data for a specific channel.
func handleGetChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	c, found, err := ss.cs.GetChannelModel("id", id, false)
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
	response["id"] = c.ID
	response["name"] = c.Name
	response["urls"] = c.GetURLs()
	response["last_scan"] = c.LastScan
	response["created_at"] = c.CreatedAt
	response["updated_at"] = c.UpdatedAt

	// Convert global settings to map with string representations
	if c.ChanSettings != nil {
		settingsMap := settingsJSONMap(c.ChanSettings)
		response["settings"] = settingsMap
	}

	// Convert global metarr args to map with string representations
	if c.ChanMetarrArgs != nil {
		metarrMap := metarrArgsJSONMap(c.ChanMetarrArgs)
		response["metarr"] = metarrMap
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

	// Build url_settings map with per-URL custom settings and display strings
	urlSettings := make(map[string]map[string]any)
	for _, urlModel := range c.URLModels {
		if urlModel.ChanURLSettings != nil || urlModel.ChanURLMetarrArgs != nil {
			urlSettings[urlModel.URL] = make(map[string]any)

			// Add settings with display strings
			if urlModel.ChanURLSettings != nil {
				settingsMap := settingsJSONMap(urlModel.ChanURLSettings)
				urlSettings[urlModel.URL]["settings"] = settingsMap
			}

			// Add metarr with display strings
			if urlModel.ChanURLMetarrArgs != nil {
				metarrMap := metarrArgsJSONMap(urlModel.ChanURLMetarrArgs)
				urlSettings[urlModel.URL]["metarr"] = metarrMap
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
func handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse form data: %v", err), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	urls := strings.Fields(r.FormValue("urls"))
	authDetails := strings.Fields(r.FormValue("auth_details"))
	username := r.FormValue("username")
	loginURL := r.FormValue("login_url")
	password := r.FormValue("password")
	now := time.Now()

	// Parse and validate authentication details
	authMap, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, urls, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid authentication details: %v", err), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings JSON if provided
	urlSettingsRaw := make(map[string]map[string]map[string]any)
	if urlSettingsJSON := r.FormValue("url_settings"); urlSettingsJSON != "" {
		if err := json.Unmarshal([]byte(urlSettingsJSON), &urlSettingsRaw); err != nil {
			http.Error(w, fmt.Sprintf("invalid url_settings JSON: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Parse the URL settings into proper structures
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
			parsed.Settings = parseSettingsFromMap(settingsMap)
		}
		if metarrMap, hasMetarr := settingsData["metarr"]; hasMetarr {
			parsed.Metarr = parseMetarrArgsFromMap(metarrMap)
		}

		urlSettingsMap[channelURL] = parsed
	}

	// Add channel URLs
	var chanURLs = make([]*models.ChannelURL, 0, len(urls))
	for _, u := range urls {
		if u != "" {
			if _, err := url.Parse(u); err != nil {
				http.Error(w, fmt.Sprintf("invalid channel URL %q: %v", u, err), http.StatusBadRequest)
				return
			}
			var parsedUsername, parsedPassword, parsedLoginURL string
			if _, exists := authMap[u]; exists {
				parsedUsername = authMap[u].Username
				parsedPassword = authMap[u].Password
				parsedLoginURL = authMap[u].LoginURL
			}

			chanURL := &models.ChannelURL{
				URL:       u,
				Username:  parsedUsername,
				Password:  parsedPassword,
				LoginURL:  parsedLoginURL,
				LastScan:  now,
				CreatedAt: now,
				UpdatedAt: now,
			}

			// Apply per-URL custom settings if they exist
			if urlSettings, hasCustom := urlSettingsMap[u]; hasCustom {
				chanURL.ChanURLSettings = urlSettings.Settings
				chanURL.ChanURLMetarrArgs = urlSettings.Metarr
			}

			chanURLs = append(chanURLs, chanURL)
		}
	}

	// Create model
	c := &models.Channel{
		Name:      name,
		URLModels: chanURLs,
		CreatedAt: now,
		LastScan:  now,
		UpdatedAt: now,
	}

	// Get and validate settings
	c.ChanSettings = getSettingsStrings(w, r)
	if c.ChanSettings == nil {
		c.ChanSettings = &models.Settings{}
	}

	c.ChanMetarrArgs = getMetarrArgsStrings(w, r)
	if c.ChanMetarrArgs == nil {
		c.ChanMetarrArgs = &models.MetarrArgs{}
	}

	// Add to database
	if c.ID, err = ss.cs.AddChannel(c); err != nil {
		http.Error(w, fmt.Sprintf("failed to add channel with name %q: %v", name, err), http.StatusInternalServerError)
		return
	}

	// Ignore run if desired
	ctx := context.Background()
	if r.FormValue("ignore_run") == "true" {
		log.Printf("Running ignore crawl for channel %q. No videos before this point will be downloaded to this channel.", c.Name)
		if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
			http.Error(w, fmt.Sprintf("failed to run ignore crawl on channel %q: %v", name, err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
	w.Write(fmt.Appendf(nil, "Channel %q added successfully", c.Name))
}

// handleUpdateChannel updates parameters for a given channel.
func handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	// Get channel ID from URL
	idStr := chi.URLParam(r, "id")
	channelID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse form data: %v", err), http.StatusBadRequest)
		return
	}

	// Get existing channel
	existingChannel, found, err := ss.cs.GetChannelModel(consts.QChanID, idStr, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || existingChannel == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	// Update channel name if provided
	name := r.FormValue("name")
	if name != "" && name != existingChannel.Name {
		if err := ss.cs.UpdateChannelValue(consts.QChanID, idStr, consts.QChanName, name); err != nil {
			http.Error(w, fmt.Sprintf("failed to update channel name: %v", err), http.StatusInternalServerError)
			return
		}
		existingChannel.Name = name
	}

	// Update channel settings
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

	// Update channel metarr args
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

	// Handle URL updates
	urls := strings.Fields(r.FormValue("urls"))
	authDetails := strings.Fields(r.FormValue("auth_details"))
	username := r.FormValue("username")
	loginURL := r.FormValue("login_url")
	password := r.FormValue("password")
	now := time.Now()

	// Parse authentication details
	authMap, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, urls, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid authentication details: %v", err), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings JSON if provided
	urlSettingsRaw := make(map[string]map[string]map[string]any)
	if urlSettingsJSON := r.FormValue("url_settings"); urlSettingsJSON != "" {
		if err := json.Unmarshal([]byte(urlSettingsJSON), &urlSettingsRaw); err != nil {
			http.Error(w, fmt.Sprintf("invalid url_settings JSON: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Parse the URL settings into proper structures
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
			parsed.Settings = parseSettingsFromMap(settingsMap)
		}
		if metarrMap, hasMetarr := settingsData["metarr"]; hasMetarr {
			parsed.Metarr = parseMetarrArgsFromMap(metarrMap)
		}

		urlSettingsMap[channelURL] = parsed
	}

	// Build map of existing URLs
	existingURLMap := make(map[string]*models.ChannelURL)
	for _, urlModel := range existingChannel.URLModels {
		existingURLMap[urlModel.URL] = urlModel
	}

	// Build map of new URLs
	newURLMap := make(map[string]bool)
	for _, u := range urls {
		if u != "" {
			newURLMap[u] = true
		}
	}

	// Delete URLs that are no longer in the list
	for existingURL, urlModel := range existingURLMap {
		if !newURLMap[existingURL] {
			// URL was removed, delete it
			if err := ss.cs.DeleteChannelURL(urlModel.ID); err != nil {
				logging.E("Failed to delete channel URL %q: %v", existingURL, err)
			}
		}
	}

	// Add or update URLs
	for _, u := range urls {
		if u == "" {
			continue
		}

		if _, err := url.Parse(u); err != nil {
			http.Error(w, fmt.Sprintf("invalid channel URL %q: %v", u, err), http.StatusBadRequest)
			return
		}

		var parsedUsername, parsedPassword, parsedLoginURL string
		if _, exists := authMap[u]; exists {
			parsedUsername = authMap[u].Username
			parsedPassword = authMap[u].Password
			parsedLoginURL = authMap[u].LoginURL
		}

		// Check if URL already exists
		if existingURLModel, exists := existingURLMap[u]; exists {
			// Update existing URL
			existingURLModel.Username = parsedUsername
			existingURLModel.Password = parsedPassword
			existingURLModel.LoginURL = parsedLoginURL
			existingURLModel.UpdatedAt = now

			// Apply per-URL custom settings if they exist, otherwise clear them
			if urlSettings, hasCustom := urlSettingsMap[u]; hasCustom {
				existingURLModel.ChanURLSettings = urlSettings.Settings
				existingURLModel.ChanURLMetarrArgs = urlSettings.Metarr
			} else {
				// No custom settings for this URL - clear any existing ones
				existingURLModel.ChanURLSettings = nil
				existingURLModel.ChanURLMetarrArgs = nil
			}

			if err := ss.cs.UpdateChannelURLSettings(existingURLModel); err != nil {
				http.Error(w, fmt.Sprintf("failed to update URL %q: %v", u, err), http.StatusInternalServerError)
				return
			}
		} else {
			// Add new URL
			chanURL := &models.ChannelURL{
				URL:       u,
				Username:  parsedUsername,
				Password:  parsedPassword,
				LoginURL:  parsedLoginURL,
				LastScan:  now,
				CreatedAt: now,
				UpdatedAt: now,
			}

			// Apply per-URL custom settings if they exist
			if urlSettings, hasCustom := urlSettingsMap[u]; hasCustom {
				chanURL.ChanURLSettings = urlSettings.Settings
				chanURL.ChanURLMetarrArgs = urlSettings.Metarr
			}

			if _, err := ss.cs.AddChannelURL(channelID, chanURL, true); err != nil {
				http.Error(w, fmt.Sprintf("failed to add URL %q: %v", u, err), http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Channel updated successfully"))
}

// handleUpdateChannelURLSettings updates parameters for a given channel URL.
func handleUpdateChannelURLSettings(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	cURLStr := chi.URLParam(r, "channel_url")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	cURL, found, err := ss.cs.GetChannelURLModel(id, cURLStr, false)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found || cURL == nil {
		http.Error(w, "channel URL nil or not found", http.StatusNotFound)
		return
	}

	cURL.ChanURLSettings = getSettingsStrings(w, r)
	cURL.ChanURLMetarrArgs = getMetarrArgsStrings(w, r)

	if err := ss.cs.UpdateChannelURLSettings(cURL); err != nil {
		http.Error(w, fmt.Sprintf("Could not update channel URL %q Metarr Arguments: %v", cURL.URL, err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Updated channel URL in channel " + idStr))
}

// handleDeleteChannelURL deletes a URL for a given channel.
func handleDeleteChannelURL(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Updated channel URL " + id))
}

// handleAddChannelURL deletes a URL for a given channel.
func handleAddChannelURL(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Added channel URL for channel " + id))
}

// handleDeleteChannel deletes a channel from Tubarr.
func handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ss.cs.DeleteChannel(consts.QChanID, id)
	w.WriteHeader(http.StatusNoContent)
}

// handleGetAllVideos retrieves all videos, ignored or finished, for a given channel.
func handleGetAllVideos(w http.ResponseWriter, r *http.Request) {
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

	// Get video downloads with full metadata
	videos, _, err := ss.cs.GetDownloadedOrIgnoredVideos(c)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve downloaded videos for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(videos)
}

// handleDeleteChannelVideos deletes given video entries from a channel.
func handleDeleteChannelVideos(w http.ResponseWriter, r *http.Request) {
	// Get channel ID from URL path
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not parse channel ID %q: %v", idStr, err), http.StatusBadRequest)
		return
	}

	// Read and parse body for DELETE requests
	//
	// DELETE requests need special handling for form data in the body
	bodyBytes := make([]byte, r.ContentLength)
	if _, err := r.Body.Read(bodyBytes); err != nil && err.Error() != "EOF" {
		logging.E("Failed to read body: %v", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse the URL-encoded form data from body
	values, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		logging.E("Failed to parse query: %v", err)
		http.Error(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	// Get video URLs from form array
	urls := values["urls[]"]
	logging.D(1, "Parsed form data: %+v", values)
	logging.D(1, "Video URLs to delete: %v", urls)

	if len(urls) == 0 {
		http.Error(w, "no video URLs provided", http.StatusBadRequest)
		return
	}

	if err := ss.cs.DeleteVideoURLs(id, urls); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete video URLs: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleGetDownloads retrieves active downloads for a given channel.
func handleGetDownloads(w http.ResponseWriter, r *http.Request) {
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

	// Get active downloads with progress (filtered by channel in memory, no DB query!)
	videos, err := ss.getActiveDownloads(c)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve active downloads for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(videos)
}

// handleCancelDownload cancels an active download by video ID.
func handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	videoIDStr := chi.URLParam(r, "videoID")
	videoID, err := strconv.ParseInt(videoIDStr, 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video ID %q: %v", videoIDStr, err), http.StatusBadRequest)
		return
	}

	// Update database status first
	if err := ss.cancelDownload(videoID); err != nil {
		http.Error(w, fmt.Sprintf("failed to cancel download: %v", err), http.StatusInternalServerError)
		return
	}

	// Cancel the actual running download process
	var videoURL string
	if videoURL, err = ss.vs.GetVideoURLByID(videoID); err != nil {
		logging.E("Could not get video URL for ID %d: %v", videoID, err)
	}
	cancelled := ss.ds.CancelDownload(videoID, videoURL)
	if !cancelled {
		logging.W("Download for video ID %d was marked as cancelled in DB but no active download process was found", videoID)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Download cancelled successfully"}`))
}

// handleCrawlChannel initiates a crawl for a specific channel.
func handleCrawlChannel(w http.ResponseWriter, r *http.Request) {
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

	// Start crawl in background
	if !state.CrawlStateActive(c.Name) {
		state.LockCrawlState(c.Name)
		go func() {
			defer state.UnlockCrawlState(c.Name)

			ctx := context.Background()
			logging.I("Starting crawl for channel %q (ID: %d) via web request", c.Name, id)
			if err := app.CrawlChannel(ctx, ss.s, c); err != nil {
				logging.E("Failed to crawl channel %q: %v", c.Name, err)
			} else {
				logging.S("Successfully completed crawl for channel %q", c.Name)
			}
		}()

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"message": "Channel crawl started"}`))
		return
	}
	w.WriteHeader(http.StatusAlreadyReported)
	w.Write([]byte(`{"message": "Channel crawl already running for channel"}`))
}

// handleIgnoreCrawlChannel initiates a crawl for a specific channel.
func handleIgnoreCrawlChannel(w http.ResponseWriter, r *http.Request) {
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

	// Start crawl in background
	if !state.CrawlStateActive(c.Name) {
		state.LockCrawlState(c.Name)
		go func() {
			defer state.UnlockCrawlState(c.Name)

			ctx := context.Background()
			logging.I("Starting ignore crawl for channel %q (ID: %d) via web request", c.Name, id)
			if err := app.CrawlChannelIgnore(ctx, ss.s, c); err != nil {
				logging.E("Failed to run ignore crawl for channel %q: %v", c.Name, err)
			} else {
				logging.S("Successfully completed ignore crawl for channel %q", c.Name)
			}
		}()

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"message": "Channel ignore crawl started"}`))
		return
	}
	w.WriteHeader(http.StatusAlreadyReported)
	w.Write([]byte(`{"message": "Channel ignore crawl already running for channel"}`))
}

// handleLatestDownloads retrieves the latest downloads for a given channel.
func handleLatestDownloads(w http.ResponseWriter, r *http.Request) {
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

	// Get video downloads with full metadata
	videos, err := ss.getHomepageCarouselVideos(c, 10)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve downloaded videos for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(videos)
}

// handleSetLogLevel sets the logging level in the database.
func handleSetLogLevel(w http.ResponseWriter, r *http.Request) {
	levelStr := chi.URLParam(r, "logging_level")
	level, err := strconv.Atoi(levelStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not set logging level using input %q: %v", levelStr, err), http.StatusBadRequest)
		return
	}
	logging.Level = level
}

// ----------------- Helpers ----------------------------------------------------------------------------------------

// getSettingsStrings retrieves Settings model strings, converting where needed.
func getSettingsStrings(w http.ResponseWriter, r *http.Request) *models.Settings {
	// -- Initialize --
	// Strings not needing validation
	channelConfigFile := r.FormValue("channel_config_file")
	cookiesFromBrowser := r.FormValue("cookies_from_browser")
	externalDownloader := r.FormValue("external_downloader")
	externalDownloaderArgs := r.FormValue("external_downloader_args")
	extraYtdlpVideoArgs := r.FormValue("extra_ytdlp_video_args")
	extraYtdlpMetaArgs := r.FormValue("extra_ytdlp_meta_args")
	filterFile := r.FormValue("filter_file")
	moveOpFile := r.FormValue("move_ops_file")

	// Strings needing validation
	jDir := r.FormValue("json_directory")
	vDir := r.FormValue("video_directory")
	maxFilesizeStr := r.FormValue("max_filesize")
	ytdlpOutExt := r.FormValue("ytdlp_output_ext")
	fromDateStr := r.FormValue("from_date")
	toDateStr := r.FormValue("to_date")

	// Integers
	maxConcurrencyStr := r.FormValue("max_concurrency")
	crawlFreqStr := r.FormValue("crawl_freq")
	retriesStr := r.FormValue("download_retries")

	// Bools
	useGlobalCookiesStr := r.FormValue("use_global_cookies")

	// Models
	filtersStr := r.FormValue("filters")
	moveOpsStr := r.FormValue("move_ops")

	// -- Validation --
	// Strings
	if _, err := validation.ValidateDirectory(vDir, true); err != nil {
		http.Error(w, fmt.Sprintf("video directory %q is invalid: %v", vDir, err), http.StatusBadRequest)
		return nil
	}
	if _, err := validation.ValidateDirectory(jDir, true); err != nil {
		http.Error(w, fmt.Sprintf("JSON directory %q is invalid: %v", jDir, err), http.StatusBadRequest)
		return nil
	}
	maxFilesize, err := validation.ValidateMaxFilesize(maxFilesizeStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("max filesize %q is invalid: %v", maxFilesizeStr, err), http.StatusBadRequest)
		return nil
	}
	if err := validation.ValidateYtdlpOutputExtension(ytdlpOutExt); err != nil {
		http.Error(w, fmt.Sprintf("invalid YTDLP output extension %q: %v", ytdlpOutExt, err), http.StatusBadRequest)
		return nil
	}
	toDate, err := validation.ValidateToFromDate(toDateStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert to date string %q: %v", toDateStr, err), http.StatusBadRequest)
		return nil
	}
	fromDate, err := validation.ValidateToFromDate(fromDateStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert from date string %q: %v", fromDateStr, err), http.StatusBadRequest)
		return nil
	}

	// Integers
	maxConcurrency, err := strconv.Atoi(maxConcurrencyStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert max concurrency string %q: %v", maxConcurrencyStr, err), http.StatusBadRequest)
		return nil
	}
	maxConcurrency = validation.ValidateConcurrencyLimit(maxConcurrency)
	crawlFreq, err := strconv.Atoi(crawlFreqStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert crawl frequency string %q: %v", crawlFreqStr, err), http.StatusBadRequest)
		return nil
	}
	retries, err := strconv.Atoi(retriesStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert retries string %q: %v", retriesStr, err), http.StatusBadRequest)
		return nil
	}

	// Bools
	var useGlobalCookies bool = (useGlobalCookiesStr == "true")

	// Model conversions (newline-separated, not space-separated)
	filters, err := validation.ValidateFilterOps(splitNonEmptyLines(filtersStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid download filters %q: %v", filtersStr, err), http.StatusBadRequest)
		return nil
	}
	moveOps, err := validation.ValidateMoveOps(splitNonEmptyLines(moveOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid move ops %q: %v", moveOpsStr, err), http.StatusBadRequest)
		return nil
	}

	return &models.Settings{
		ChannelConfigFile:      channelConfigFile,
		Concurrency:            maxConcurrency,
		CookiesFromBrowser:     cookiesFromBrowser,
		CrawlFreq:              crawlFreq,
		ExternalDownloader:     externalDownloader,
		ExternalDownloaderArgs: externalDownloaderArgs,
		MaxFilesize:            maxFilesize,
		Retries:                retries,
		UseGlobalCookies:       useGlobalCookies,
		YtdlpOutputExt:         ytdlpOutExt,
		ExtraYTDLPVideoArgs:    extraYtdlpVideoArgs,
		ExtraYTDLPMetaArgs:     extraYtdlpMetaArgs,

		Filters:    filters,
		FilterFile: filterFile,
		MoveOps:    moveOps,
		MoveOpFile: moveOpFile,

		FromDate: fromDate,
		ToDate:   toDate,

		JSONDir:  jDir,
		VideoDir: vDir,
	}
}

// getMetarrArgsStrings retrieves MetarrArgs model strings, converting where needed.
func getMetarrArgsStrings(w http.ResponseWriter, r *http.Request) *models.MetarrArgs {
	// -- Initialize --
	// Strings not needing validation
	outExt := r.FormValue("metarr_output_ext")
	filenameOpsFile := r.FormValue("metarr_filename_ops_file")
	filteredFilenameOpsFile := r.FormValue("filtered_filename_ops_file")
	metaOpsFile := r.FormValue("metarr_meta_ops_file")
	filteredMetaOpsFile := r.FormValue("filtered_meta_ops_file")
	extraFFmpegArgs := r.FormValue("metarr_extra_ffmpeg_args")

	// Strings requiring validation
	renameStyle := r.FormValue("metarr_rename_style")
	minFreeMem := r.FormValue("metarr_min_free_mem")
	useGPUStr := r.FormValue("metarr_gpu")
	gpuDirStr := r.FormValue("metarr_gpu_directory")
	outputDir := r.FormValue("metarr_output_directory")
	transcodeVideoFilterStr := r.FormValue("metarr_transcode_video_filter")
	transcodeCodecStr := r.FormValue("metarr_video_transcode_codecs")
	transcodeAudioCodecStr := r.FormValue("metarr_transcode_audio_codecs")
	transcodeQualityStr := r.FormValue("metarr_transcode_quality")

	// Ints
	maxConcurrencyStr := r.FormValue("metarr_concurrency")
	maxCPUStr := r.FormValue("metarr_max_cpu_usage")

	// Models
	filenameOpsStr := r.FormValue("metarr_filename_ops")
	filteredFilenameOpsStr := r.FormValue("filtered_filename_ops")
	filteredMetaOpsStr := r.FormValue("filtered_meta_ops")
	metaOpsStr := r.FormValue("metarr_meta_ops")

	// -- Validation --
	//Strings
	if err := validation.ValidateRenameFlag(renameStyle); err != nil {
		http.Error(w, fmt.Sprintf("invalid rename style %q: %v", renameStyle, err), http.StatusBadRequest)
		return nil
	}
	if err := validation.ValidateMinFreeMem(minFreeMem); err != nil {
		http.Error(w, fmt.Sprintf("invalid min free mem %q: %v", minFreeMem, err), http.StatusBadRequest)
		return nil
	}
	useGPU, gpuDir, err := validation.ValidateGPU(useGPUStr, gpuDirStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid GPU type or device directory (%q : %q): %v", useGPUStr, gpuDirStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeVideoFilter, err := validation.ValidateTranscodeVideoFilter(transcodeVideoFilterStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video filter string %q: %v", transcodeVideoFilterStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeVideoCodecs, err := validation.ValidateVideoTranscodeCodecSlice(splitNonEmptyLines(transcodeCodecStr), useGPU)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video codec string %q: %v", transcodeCodecStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeAudioCodecs, err := validation.ValidateAudioTranscodeCodecSlice(splitNonEmptyLines(transcodeAudioCodecStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid audio codec string %q: %v", transcodeAudioCodecStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeQuality, err := validation.ValidateTranscodeQuality(transcodeQualityStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid transcode quality string %q: %v", transcodeQualityStr, err), http.StatusBadRequest)
		return nil
	}
	if _, err := validation.ValidateDirectory(outputDir, false); err != nil {
		http.Error(w, fmt.Sprintf("cannot get output directories. Input string %q. Error: %v", outputDir, err), http.StatusBadRequest)
		return nil
	}

	// Integers & Floats
	maxConcurrency, err := strconv.Atoi(maxConcurrencyStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert max concurrency string %q: %v", maxConcurrencyStr, err), http.StatusBadRequest)
		return nil
	}
	maxConcurrency = validation.ValidateConcurrencyLimit(maxConcurrency)

	maxCPU := 100.00
	if maxCPUStr != "" {
		maxCPU, err = strconv.ParseFloat(maxCPUStr, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert max CPU limit string %q: %v", maxCPUStr, err), http.StatusBadRequest)
			return nil
		}
	}

	// Models (newline-separated, not space-separated)
	filenameOps, err := validation.ValidateFilenameOps(splitNonEmptyLines(filenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filename ops %q: %v", filenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredFilenameOps, err := validation.ValidateFilteredFilenameOps(splitNonEmptyLines(filteredFilenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filtered filename ops %q: %v", filteredFilenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	metaOps, err := validation.ValidateMetaOps(splitNonEmptyLines(metaOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid meta ops %q: %v", metaOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredMetaOps, err := validation.ValidateFilteredMetaOps(splitNonEmptyLines(filteredMetaOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filtered meta ops %q: %v", filteredMetaOpsStr, err), http.StatusBadRequest)
		return nil
	}

	return &models.MetarrArgs{
		OutputExt:               outExt,
		FilenameOps:             filenameOps,
		FilenameOpsFile:         filenameOpsFile,
		FilteredFilenameOps:     filteredFilenameOps,
		FilteredFilenameOpsFile: filteredFilenameOpsFile,
		RenameStyle:             renameStyle,
		MetaOps:                 metaOps,
		MetaOpsFile:             metaOpsFile,
		FilteredMetaOps:         filteredMetaOps,
		FilteredMetaOpsFile:     filteredMetaOpsFile,
		OutputDir:               outputDir,
		Concurrency:             maxConcurrency,
		MaxCPU:                  maxCPU,
		MinFreeMem:              minFreeMem,
		UseGPU:                  useGPU,
		GPUDir:                  gpuDir,
		TranscodeVideoFilter:    transcodeVideoFilter,
		TranscodeVideoCodecs:    transcodeVideoCodecs,
		TranscodeAudioCodecs:    transcodeAudioCodecs,
		TranscodeQuality:        transcodeQuality,
		ExtraFFmpegArgs:         extraFFmpegArgs,
	}
}

// splitNonEmptyLines splits a string by newlines and filters out empty lines.
func splitNonEmptyLines(s string) []string {
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// parseSettingsFromMap parses Settings from a map[string]any (from JSON).
// This is used when parsing per-URL settings from the frontend.
func parseSettingsFromMap(data map[string]any) *models.Settings {
	settings := &models.Settings{}

	// Extract string fields
	if v, ok := data["channel_config_file"].(string); ok {
		settings.ChannelConfigFile = v
	}
	if v, ok := data["cookies_from_browser"].(string); ok {
		settings.CookiesFromBrowser = v
	}
	if v, ok := data["external_downloader"].(string); ok {
		settings.ExternalDownloader = v
	}
	if v, ok := data["external_downloader_args"].(string); ok {
		settings.ExternalDownloaderArgs = v
	}
	if v, ok := data["extra_ytdlp_video_args"].(string); ok {
		settings.ExtraYTDLPVideoArgs = v
	}
	if v, ok := data["extra_ytdlp_meta_args"].(string); ok {
		settings.ExtraYTDLPMetaArgs = v
	}
	if v, ok := data["filter_file"].(string); ok {
		settings.FilterFile = v
	}
	if v, ok := data["move_ops_file"].(string); ok {
		settings.MoveOpFile = v
	}
	if v, ok := data["json_directory"].(string); ok {
		settings.JSONDir = v
	}
	if v, ok := data["video_directory"].(string); ok {
		settings.VideoDir = v
	}
	if v, ok := data["max_filesize"].(string); ok {
		settings.MaxFilesize = v
	}
	if v, ok := data["ytdlp_output_ext"].(string); ok {
		settings.YtdlpOutputExt = v
	}
	if v, ok := data["from_date"].(string); ok {
		settings.FromDate = v
	}
	if v, ok := data["to_date"].(string); ok {
		settings.ToDate = v
	}

	// Extract integer fields
	if v, ok := data["max_concurrency"].(float64); ok {
		settings.Concurrency = int(v)
	}
	if v, ok := data["crawl_freq"].(float64); ok {
		settings.CrawlFreq = int(v)
	}
	if v, ok := data["download_retries"].(float64); ok {
		settings.Retries = int(v)
	}

	// Extract boolean fields
	if v, ok := data["use_global_cookies"].(bool); ok {
		settings.UseGlobalCookies = v
	}

	// Parse model fields from strings (newline-separated, not space-separated)
	if filtersStr, ok := data["filters"].(string); ok && filtersStr != "" {
		lines := splitNonEmptyLines(filtersStr)
		if filters, err := validation.ValidateFilterOps(lines); err == nil {
			settings.Filters = filters
		}
	}
	if moveOpsStr, ok := data["move_ops"].(string); ok && moveOpsStr != "" {
		lines := splitNonEmptyLines(moveOpsStr)
		if moveOps, err := validation.ValidateMoveOps(lines); err == nil {
			settings.MoveOps = moveOps
		}
	}

	return settings
}

// parseMetarrArgsFromMap parses MetarrArgs from a map[string]any (from JSON).
// This is used when parsing per-URL metarr settings from the frontend.
func parseMetarrArgsFromMap(data map[string]any) *models.MetarrArgs {
	metarr := &models.MetarrArgs{}

	// Extract string fields
	if v, ok := data["metarr_output_ext"].(string); ok {
		metarr.OutputExt = v
	}
	if v, ok := data["metarr_filename_ops_file"].(string); ok {
		metarr.FilenameOpsFile = v
	}
	if v, ok := data["filtered_filename_ops_file"].(string); ok {
		metarr.FilteredFilenameOpsFile = v
	}
	if v, ok := data["metarr_meta_ops_file"].(string); ok {
		metarr.MetaOpsFile = v
	}
	if v, ok := data["filtered_meta_ops_file"].(string); ok {
		metarr.FilteredMetaOpsFile = v
	}
	if v, ok := data["metarr_extra_ffmpeg_args"].(string); ok {
		metarr.ExtraFFmpegArgs = v
	}
	if v, ok := data["metarr_rename_style"].(string); ok {
		metarr.RenameStyle = v
	}
	if v, ok := data["metarr_min_free_mem"].(string); ok {
		metarr.MinFreeMem = v
	}
	if v, ok := data["metarr_gpu"].(string); ok {
		metarr.UseGPU = v
	}
	if v, ok := data["metarr_gpu_directory"].(string); ok {
		metarr.GPUDir = v
	}
	if v, ok := data["metarr_output_directory"].(string); ok {
		metarr.OutputDir = v
	}
	if v, ok := data["metarr_transcode_video_filter"].(string); ok {
		metarr.TranscodeVideoFilter = v
	}
	if v, ok := data["metarr_video_transcode_codecs"].([]string); ok {
		metarr.TranscodeVideoCodecs = v
	}
	if v, ok := data["metarr_transcode_audio_codecs"].([]string); ok {
		metarr.TranscodeAudioCodecs = v
	}
	if v, ok := data["metarr_transcode_quality"].(string); ok {
		metarr.TranscodeQuality = v
	}

	// Extract integer fields
	if v, ok := data["metarr_concurrency"].(float64); ok {
		metarr.Concurrency = int(v)
	}

	// Extract float fields
	if v, ok := data["metarr_max_cpu_usage"].(float64); ok {
		metarr.MaxCPU = v
	}

	// Parse model fields from strings (newline-separated, not space-separated)
	if filenameOpsStr, ok := data["metarr_filename_ops"].(string); ok && filenameOpsStr != "" {
		lines := splitNonEmptyLines(filenameOpsStr)
		if filenameOps, err := validation.ValidateFilenameOps(lines); err == nil {
			metarr.FilenameOps = filenameOps
		}
	}
	if filteredFilenameOpsStr, ok := data["filtered_filename_ops"].(string); ok && filteredFilenameOpsStr != "" {
		lines := splitNonEmptyLines(filteredFilenameOpsStr)
		if filteredFilenameOps, err := validation.ValidateFilteredFilenameOps(lines); err == nil {
			metarr.FilteredFilenameOps = filteredFilenameOps
		}
	}
	if metaOpsStr, ok := data["metarr_meta_ops"].(string); ok && metaOpsStr != "" {
		lines := splitNonEmptyLines(metaOpsStr)
		if metaOps, err := validation.ValidateMetaOps(lines); err == nil {
			metarr.MetaOps = metaOps
		}
	}
	if filteredMetaOpsStr, ok := data["filtered_meta_ops"].(string); ok && filteredMetaOpsStr != "" {
		lines := splitNonEmptyLines(filteredMetaOpsStr)
		if filteredMetaOps, err := validation.ValidateFilteredMetaOps(lines); err == nil {
			metarr.FilteredMetaOps = filteredMetaOps
		}
	}

	return metarr
}
