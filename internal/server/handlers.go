package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/validation"

	"github.com/go-chi/chi/v5"
)

// handleListChannels lists Tubarr channels.
func handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, found, err := cs.GetAllChannels()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channels)
}

// handleGetChannel returns the data for a specific channel.
func handleGetChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	c, found, err := cs.GetChannelModel("id", id)
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
	name := chi.URLParam(r, "name")
	now := time.Now()

	// Create model
	c := &models.Channel{
		Name:         name,
		ChanSettings: getSettingsStrings(w, r),
		CreatedAt:    now,
		LastScan:     now,
		UpdatedAt:    now,
	}
	c.ChanMetarrArgs = getMetarrArgsStrings(w, r, c)

	// Add to database
	if _, err := cs.AddChannel(c); err != nil {
		http.Error(w, fmt.Sprintf("failed to add channel with name %q: %v", name, err), http.StatusInternalServerError)
		return
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
	cs.DeleteChannel(consts.QChanID, id)
	w.WriteHeader(http.StatusNoContent)
}

// handleLatestDownloads retrieves the latest downloads for a given channel.
func handleLatestDownloads(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, found, err := cs.GetChannelModel(consts.QChanID, id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	downloadedURLs, err := cs.GetAlreadyDownloadedURLs(c)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not retrieve downloaded URLs for channel %q: %v", c.Name, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(downloadedURLs[:10])
}

// ----------------- Helpers ----------------------------------------------------------------------------------------

// getSettingsStrings retrieves Settings model strings, converting where needed.
func getSettingsStrings(w http.ResponseWriter, r *http.Request) *models.Settings {
	// -- Initialize --
	// Strings not needing validation
	channelConfigFile := chi.URLParam(r, "channel_config_file")
	cookieSource := chi.URLParam(r, "cookie_source")
	externalDownloader := chi.URLParam(r, "external_downloader")
	externalDownloaderArgs := chi.URLParam(r, "external_downloader_args")
	extraYtdlpVideoArgs := chi.URLParam(r, "extra_ytdlp_video_args")
	extraYtdlpMetaArgs := chi.URLParam(r, "extra_ytdlp_meta_args")
	filterFile := chi.URLParam(r, "filter_file")
	moveOpFile := chi.URLParam(r, "move_ops_file")

	// Strings needing validation
	maxFilesizeStr := chi.URLParam(r, "max_filesize")
	ytdlpOutExt := chi.URLParam(r, "ytdlp_output_ext")
	fromDateStr := chi.URLParam(r, "from_date")
	toDateStr := chi.URLParam(r, "to_date")

	// Integers
	maxConcurrencyStr := chi.URLParam(r, "max_concurrency")
	crawlFreqStr := chi.URLParam(r, "crawl_freq")
	retriesStr := chi.URLParam(r, "download_retries")

	// Bools
	useGlobalCookiesStr := chi.URLParam(r, "use_global_cookies")

	// Models
	filtersStr := chi.URLParam(r, "filters")
	moveOpsStr := chi.URLParam(r, "move_ops")

	// -- Validation --
	// Strings
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
		CookieSource:           cookieSource,
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
	}
}

// getMetarrArgsStrings retrieves MetarrArgs model strings, converting where needed.
func getMetarrArgsStrings(w http.ResponseWriter, r *http.Request, c *models.Channel) *models.MetarrArgs {
	// -- Initialize --
	// Strings not needing validation
	outExt := chi.URLParam(r, "metarr_output_ext")
	filenameOpsFile := chi.URLParam(r, "metarr_filename_ops_file")
	filteredFilenameOpsFile := chi.URLParam(r, "filtered_filename_ops_file")
	metaOpsFile := chi.URLParam(r, "metarr_meta_ops_file")
	filteredMetaOpsFile := chi.URLParam(r, "filtered_meta_ops_file")
	outputDirStr := chi.URLParam(r, "metarr_output_directory")
	extraFFmpegArgs := chi.URLParam(r, "metarr_extra_ffmpeg_args")

	// Strings requiring validation
	renameStyle := chi.URLParam(r, "metarr_rename_style")
	minFreeMem := chi.URLParam(r, "metarr_min_free_mem")
	useGPUStr := chi.URLParam(r, "metarr_gpu")
	gpuDirStr := chi.URLParam(r, "metarr_gpu_directory")
	transcodeVideoFilterStr := chi.URLParam(r, "metarr_transcode_video_filter")
	transcodeCodecStr := chi.URLParam(r, "metarr_transcode_codec")
	transcodeAudioCodecStr := chi.URLParam(r, "metarr_transcode_audio_codec")
	transcodeQualityStr := chi.URLParam(r, "metarr_transcode_quality")

	// Ints
	maxConcurrencyStr := chi.URLParam(r, "metarr_concurrency")
	maxCPUStr := chi.URLParam(r, "metarr_max_cpu_usage")

	// Models
	filenameOpsStr := chi.URLParam(r, "metarr_filename_ops")
	filteredFilenameOpsStr := chi.URLParam(r, "filtered_filename_ops")
	filteredMetaOpsStr := chi.URLParam(r, "filtered_meta_ops")
	metaOpsStr := chi.URLParam(r, "metarr_meta_ops")

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
	outDirMap, err := validation.ValidateMetarrOutputDirs(outputDirStr, nil, c)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot get output directories. Input string %q. Error: %v", outputDirStr, err), http.StatusBadRequest)
		return nil
	}

	// Integers & Floats
	maxConcurrency, err := strconv.Atoi(maxConcurrencyStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert max concurrency string %q: %v", maxConcurrencyStr, err), http.StatusBadRequest)
		return nil
	}
	maxConcurrency = validation.ValidateConcurrencyLimit(maxConcurrency)
	maxCPU, err := strconv.ParseFloat(maxCPUStr, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert max CPU limit string %q: %v", maxCPUStr, err), http.StatusBadRequest)
		return nil
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
		OutputDir:               outputDirStr,
		OutputDirMap:            outDirMap,
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
