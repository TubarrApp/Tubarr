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
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/go-chi/chi/v5"
)

// handleListChannels lists Tubarr channels.
func handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, found, err := ss.cs.GetAllChannels()
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

	c, found, err := ss.cs.GetChannelModel("id", id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(c); err != nil {
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

			chanURLs = append(chanURLs, &models.ChannelURL{
				URL:       u,
				Username:  parsedUsername,
				Password:  parsedPassword,
				LoginURL:  parsedLoginURL,
				LastScan:  now,
				CreatedAt: now,
				UpdatedAt: now,
			})
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

	c.ChanMetarrArgs = getMetarrArgsStrings(w, r, c)
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
}

// handleUpdateChannel updates parameters for a given channel.
func handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Updated channel " + id))
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
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "channel not found", http.StatusNotFound)
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

// handleLatestDownloads retrieves the latest downloads for a given channel.
func handleLatestDownloads(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, found, err := ss.cs.GetChannelModel(consts.QChanID, id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "channel not found", http.StatusNotFound)
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

	// Model conversions
	filters, err := validation.ValidateFilterOps(strings.Fields(filtersStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid download filters %q: %v", filtersStr, err), http.StatusBadRequest)
		return nil
	}
	moveOps, err := validation.ValidateMoveOps(strings.Fields(moveOpsStr))
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
func getMetarrArgsStrings(w http.ResponseWriter, r *http.Request, c *models.Channel) *models.MetarrArgs {
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
	transcodeCodecStr := r.FormValue("metarr_transcode_codec")
	transcodeAudioCodecStr := r.FormValue("metarr_transcode_audio_codec")
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
	transcodeCodec, err := validation.ValidateTranscodeCodec(transcodeCodecStr, useGPU)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video codec string %q: %v", transcodeCodecStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeAudioCodec, err := validation.ValidateTranscodeAudioCodec(transcodeAudioCodecStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid audio codec codec string %q: %v", transcodeAudioCodecStr, err), http.StatusBadRequest)
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

	// Models
	filenameOps, err := validation.ValidateFilenameOps(strings.Fields(filenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filename ops %q: %v", filenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredFilenameOps, err := validation.ValidateFilteredFilenameOps(strings.Fields(filteredFilenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filtered filename ops %q: %v", filteredFilenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	metaOps, err := validation.ValidateMetaOps(strings.Fields(metaOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid meta ops %q: %v", metaOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredMetaOps, err := validation.ValidateFilteredMetaOps(strings.Fields(filteredMetaOpsStr))
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
		TranscodeCodec:          transcodeCodec,
		TranscodeAudioCodec:     transcodeAudioCodec,
		TranscodeQuality:        transcodeQuality,
		ExtraFFmpegArgs:         extraFFmpegArgs,
	}
}
