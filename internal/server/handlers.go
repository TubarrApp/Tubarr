package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/auth"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/paths"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/state"
	"tubarr/internal/utils/logging"

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

	// Use local viper instance for consistency with directory handler
	v := viper.New()
	if err := file.LoadConfigFile(v, addFromFile); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Load viper variables into the struct from local instance
	if err := parsing.LoadViperIntoStruct(v, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse per-URL settings if present
	urlSettings, err := parsing.ParseURLSettingsFromViper(v)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse url-settings: %v", err), http.StatusBadRequest)
		return
	}
	input.URLSettings = urlSettings

	c, authMap := fillChannelFromConfigFile(w, input)
	if c == nil {
		return
	}

	fmt.Println()
	for _, u := range c.URLModels {
		logging.I("Got channel URL output ext: %q", u.ChanURLMetarrArgs.OutputExt)
		logging.I("Got max filesize: %q", u.ChanURLSettings.MaxFilesize)
	}
	fmt.Println()

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

	if err := ss.cs.DeleteVideosByURLs(id, urls); err != nil {
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
	levelStr := chi.URLParam(r, "level")
	level, err := strconv.Atoi(levelStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not set logging level using input %q: %v", levelStr, err), http.StatusBadRequest)
		return
	}
	logging.Level = level
	w.WriteHeader(http.StatusOK)
	w.Write(fmt.Appendf(nil, "Logging level set to %d", level))
}

// handleGetLogLevel retrieves the current logging level.
func handleGetLogLevel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"level": logging.Level})
}

// handleGetTubarrLogs serves the log file contents.
func handleGetTubarrLogs(w http.ResponseWriter, r *http.Request) {
	// Get log file path from abstractions
	tubarrLogFilePath := paths.TubarrLogFilePath
	if tubarrLogFilePath == "" {
		http.Error(w, "tubarr log file path not configured", http.StatusInternalServerError)
		return
	}

	// Open and read the log file
	file, err := os.Open(tubarrLogFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "tubarr log file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to open tubarr log file: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set content type as plain text
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Copy file contents to response
	if _, err := io.Copy(w, file); err != nil {
		logging.E("Failed to send tubarr log file: %v", err)
	}
}

// handleGetMetarrLogs serves the Metarr log file contents from ~/.metarr/metarr.log.
func handleGetMetarrLogs(w http.ResponseWriter, r *http.Request) {
	metarrLogPath := paths.MetarrLogFilePath
	if metarrLogPath == "" {
		http.Error(w, "metarr log file path not configured", http.StatusInternalServerError)
		return
	}

	// Open and read the log file
	file, err := os.Open(metarrLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "metarr log file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to open Metarr log file: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set content type as plain text
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Copy file contents to response
	if _, err := io.Copy(w, file); err != nil {
		logging.E("Failed to send Metarr log file: %v", err)
	}
}
