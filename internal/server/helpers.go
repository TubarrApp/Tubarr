package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedvalidation"
)

func fillChannelFromConfigFile(w http.ResponseWriter, input models.ChannelInputPtrs) (*models.Channel, map[string]*models.ChannelAccessDetails) {
	c, m, err := parsing.BuildChannelFromInput(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, make(map[string]*models.ChannelAccessDetails)
	}
	return c, m
}

// metarrArgsJSONMap returns a map of Metarr JSON keys and their values.
func metarrArgsJSONMap(mArgs *models.MetarrArgs) map[string]any {
	metarrMap := make(map[string]any)
	if mArgs.OutputDir != "" {
		metarrMap["metarr_output_directory"] = mArgs.OutputDir
	}
	if mArgs.OutputExt != "" {
		metarrMap["metarr_output_ext"] = mArgs.OutputExt
	}
	if mArgs.RenameStyle != "" {
		metarrMap["metarr_rename_style"] = mArgs.RenameStyle
	}
	if mArgs.Concurrency > 0 {
		metarrMap["metarr_concurrency"] = mArgs.Concurrency
	}
	if mArgs.MaxCPU > 0 {
		metarrMap["metarr_max_cpu_usage"] = mArgs.MaxCPU
	}
	if mArgs.MinFreeMem != "" {
		metarrMap["metarr_min_free_mem"] = mArgs.MinFreeMem
	}
	if mArgs.UseGPU != "" {
		metarrMap["metarr_gpu"] = mArgs.UseGPU
	}
	if len(mArgs.TranscodeVideoCodecs) != 0 {
		metarrMap["metarr_video_transcode_codecs"] = mArgs.TranscodeVideoCodecs
	}
	if len(mArgs.TranscodeAudioCodecs) != 0 {
		metarrMap["metarr_transcode_audio_codecs"] = mArgs.TranscodeAudioCodecs
	}
	if mArgs.TranscodeQuality != "" {
		metarrMap["metarr_transcode_quality"] = mArgs.TranscodeQuality
	}
	if mArgs.TranscodeVideoFilter != "" {
		metarrMap["metarr_transcode_video_filter"] = mArgs.TranscodeVideoFilter
	}
	if mArgs.ExtraFFmpegArgs != "" {
		metarrMap["metarr_extra_ffmpeg_args"] = mArgs.ExtraFFmpegArgs
	}
	if len(mArgs.MetaOps) > 0 {
		metarrMap["metarr_meta_ops"] = strings.Join(models.MetaOpsArrayToSlice(mArgs.MetaOps), "\n")
	}
	if len(mArgs.FilenameOps) > 0 {
		metarrMap["metarr_filename_ops"] = strings.Join(models.FilenameOpsArrayToSlice(mArgs.FilenameOps), "\n")
	}
	if len(mArgs.FilteredMetaOps) > 0 {
		filteredMetaStrings := make([]string, 0)
		for i := range mArgs.FilteredMetaOps {
			filteredMetaStrings = append(filteredMetaStrings, models.FilteredMetaOpsToSlice(mArgs.FilteredMetaOps[i])...)
		}
		metarrMap["filtered_meta_ops"] = strings.Join(filteredMetaStrings, "\n")
	}
	if len(mArgs.FilteredFilenameOps) > 0 {
		filteredFilenameStrings := make([]string, 0)
		for i := range mArgs.FilteredFilenameOps {
			filteredFilenameStrings = append(filteredFilenameStrings, models.FilteredFilenameOpsToSlice(mArgs.FilteredFilenameOps[i])...)
		}
		metarrMap["filtered_filename_ops"] = strings.Join(filteredFilenameStrings, "\n")
	}
	return metarrMap
}

// settingsJSONMap returns a map of settings JSON keys and their values.
func settingsJSONMap(settings *models.Settings) map[string]any {
	settingsMap := make(map[string]any)
	if settings.VideoDir != "" {
		settingsMap["video_directory"] = settings.VideoDir
	}
	if settings.JSONDir != "" {
		settingsMap["json_directory"] = settings.JSONDir
	}
	if settings.Concurrency > 0 {
		settingsMap["max_concurrency"] = settings.Concurrency
	}
	if settings.CrawlFreq > 0 {
		settingsMap["crawl_freq"] = settings.CrawlFreq
	}
	if settings.Retries > 0 {
		settingsMap["download_retries"] = settings.Retries
	}
	if settings.MaxFilesize != "" {
		settingsMap["max_filesize"] = settings.MaxFilesize
	}
	if settings.FromDate != "" {
		settingsMap["from_date"] = settings.FromDate
	}
	if settings.ToDate != "" {
		settingsMap["to_date"] = settings.ToDate
	}
	if settings.FilterFile != "" {
		settingsMap["filter_file"] = settings.FilterFile
	}
	if len(settings.Filters) > 0 {
		settingsMap["filters"] = strings.Join(models.FiltersArrayToSlice(settings.Filters), "\n")
	}
	if settings.YtdlpOutputExt != "" {
		settingsMap["ytdlp_output_ext"] = settings.YtdlpOutputExt
	}
	if settings.CookiesFromBrowser != "" {
		settingsMap["cookies_from_browser"] = settings.CookiesFromBrowser
	}
	if settings.ExternalDownloader != "" {
		settingsMap["external_downloader"] = settings.ExternalDownloader
	}
	if settings.ExternalDownloaderArgs != "" {
		settingsMap["external_downloader_args"] = settings.ExternalDownloaderArgs
	}
	if settings.ExtraYTDLPVideoArgs != "" {
		settingsMap["extra_ytdlp_video_args"] = settings.ExtraYTDLPVideoArgs
	}
	if settings.ExtraYTDLPMetaArgs != "" {
		settingsMap["extra_ytdlp_meta_args"] = settings.ExtraYTDLPMetaArgs
	}
	if settings.MetaFilterMoveOpFile != "" {
		settingsMap["move_ops_file"] = settings.MetaFilterMoveOpFile
	}
	if len(settings.MetaFilterMoveOps) > 0 {
		moveOpStrings := make([]string, len(settings.MetaFilterMoveOps))
		for i := range settings.MetaFilterMoveOps {
			moveOpStrings[i] = models.MetaFilterMoveOpsToString(settings.MetaFilterMoveOps[i])
		}
		settingsMap["move_ops"] = strings.Join(moveOpStrings, "\n")
	}
	if settings.UseGlobalCookies {
		settingsMap["use_global_cookies"] = true
	}
	if settings.Paused {
		settingsMap["paused"] = true
	}
	return settingsMap
}

// parseSettingsFromMap parses Settings from a map[string]any (from JSON).
//
// This is used when parsing per-URL settings from the frontend.
func parseSettingsFromMap(data map[string]any) (*models.Settings, error) {
	settings := &models.Settings{}

	// Extract string fields
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
		if _, err := validation.ValidateFile(v, false); err != nil {
			return nil, err
		}
		settings.FilterFile = v
	}
	if v, ok := data["move_ops_file"].(string); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return nil, err
		}
		settings.MetaFilterMoveOpFile = v
	}
	if v, ok := data["json_directory"].(string); ok {
		if _, err := validation.ValidateDirectory(v, true); err != nil {
			return nil, err
		}
		settings.JSONDir = v
	}
	if v, ok := data["video_directory"].(string); ok {
		if _, err := validation.ValidateDirectory(v, true); err != nil {
			return nil, err
		}
		settings.VideoDir = v
	}
	if v, ok := data["max_filesize"].(string); ok {
		validFilesize, err := validation.ValidateMaxFilesize(v)
		if err != nil {
			return nil, err
		}
		settings.MaxFilesize = validFilesize
	}
	if v, ok := data["ytdlp_output_ext"].(string); ok {
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return nil, err
		}
		settings.YtdlpOutputExt = v
	}
	if v, ok := data["from_date"].(string); ok {
		validDate, err := validation.ValidateToFromDate(v)
		if err != nil {
			return nil, err
		}
		settings.FromDate = validDate
	}
	if v, ok := data["to_date"].(string); ok {
		validDate, err := validation.ValidateToFromDate(v)
		if err != nil {
			return nil, err
		}
		settings.ToDate = validDate
	}

	// Extract integer fields
	if v, ok := data["max_concurrency"].(int); ok {
		valid := sharedvalidation.ValidateConcurrencyLimit(v)
		settings.Concurrency = valid
	}
	if v, ok := data["crawl_freq"].(int); ok {
		settings.CrawlFreq = max(v, 0)
	}
	if v, ok := data["download_retries"].(int); ok {
		settings.Retries = max(v, 0)
	}

	// Extract boolean fields
	if v, ok := data["use_global_cookies"].(bool); ok {
		settings.UseGlobalCookies = v
	}

	// Parse model fields from strings (newline-separated, not space-separated)
	if filtersStr, ok := data["filters"].(string); ok && filtersStr != "" {
		lines := splitNonEmptyLines(filtersStr)
		filters, err := parsing.ParseFilterOps(lines)
		if err != nil {
			return nil, err
		}
		settings.Filters = filters
	}
	if moveOpsStr, ok := data["move_ops"].(string); ok && moveOpsStr != "" {
		lines := splitNonEmptyLines(moveOpsStr)
		moveOps, err := parsing.ParseMetaFilterMoveOps(lines)
		if err != nil {
			return nil, err
		}
		settings.MetaFilterMoveOps = moveOps
	}

	return settings, nil
}

// parseMetarrArgsFromMap parses MetarrArgs from a map[string]any (from JSON).
//
// This is used when parsing per-URL metarr settings from the frontend.
func parseMetarrArgsFromMap(data map[string]any) (*models.MetarrArgs, error) {
	metarr := &models.MetarrArgs{}

	// Extract string fields
	if v, ok := data["metarr_output_ext"].(string); ok {
		metarr.OutputExt = v
	}
	if v, ok := data["metarr_filename_ops_file"].(string); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return nil, err
		}
		metarr.FilenameOpsFile = v
	}
	if v, ok := data["filtered_filename_ops_file"].(string); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return nil, err
		}
		metarr.FilteredFilenameOpsFile = v
	}
	if v, ok := data["metarr_meta_ops_file"].(string); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return nil, err
		}
		metarr.MetaOpsFile = v
	}
	if v, ok := data["filtered_meta_ops_file"].(string); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return nil, err
		}
		metarr.FilteredMetaOpsFile = v
	}
	if v, ok := data["metarr_extra_ffmpeg_args"].(string); ok {
		metarr.ExtraFFmpegArgs = v
	}
	if v, ok := data["metarr_rename_style"].(string); ok {
		if err := validation.ValidateRenameFlag(v); err != nil {
			return nil, err
		}
		metarr.RenameStyle = v
	}
	if v, ok := data["metarr_min_free_mem"].(string); ok {
		if _, err := sharedvalidation.ValidateMinFreeMem(v); err != nil {
			return nil, err
		}
		metarr.MinFreeMem = v
	}
	if v, ok := data["metarr_gpu_directory"].(string); ok {
		metarr.GPUDir = v
	}
	if v, ok := data["metarr_gpu"].(string); ok {
		validGPU, _, err := validation.ValidateGPU(v, metarr.GPUDir)
		if err != nil {
			return nil, err
		}
		metarr.UseGPU = validGPU
	}
	if v, ok := data["metarr_output_directory"].(string); ok {
		if _, err := validation.ValidateDirectory(v, true); err != nil {
			return nil, err
		}
		metarr.OutputDir = v
	}
	if v, ok := data["metarr_transcode_video_filter"].(string); ok {
		metarr.TranscodeVideoFilter = v
	}
	if v, ok := data["metarr_video_transcode_codecs"].([]string); ok {
		validPairs, err := validation.ValidateVideoTranscodeCodecSlice(v, metarr.UseGPU)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeVideoCodecs = validPairs
	}
	if v, ok := data["metarr_transcode_audio_codecs"].([]string); ok {
		validPairs, err := validation.ValidateAudioTranscodeCodecSlice(v)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeAudioCodecs = validPairs
	}
	if v, ok := data["metarr_transcode_quality"].(string); ok {
		validQuality, err := sharedvalidation.ValidateTranscodeQuality(v)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeQuality = validQuality
	}

	// Extract integer fields
	if v, ok := data["metarr_concurrency"].(int); ok {
		metarr.Concurrency = max(v, 0)
	}

	// Extract float fields
	if v, ok := data["metarr_max_cpu_usage"].(float64); ok {
		metarr.MaxCPU = v
	}

	// Parse model fields from strings (newline-separated, not space-separated)
	if filenameOpsStr, ok := data["metarr_filename_ops"].(string); ok && filenameOpsStr != "" {
		lines := splitNonEmptyLines(filenameOpsStr)
		filenameOps, err := parsing.ParseFilenameOps(lines)
		if err != nil {
			return nil, err
		}
		metarr.FilenameOps = filenameOps
	}
	if filteredFilenameOpsStr, ok := data["filtered_filename_ops"].(string); ok && filteredFilenameOpsStr != "" {
		lines := splitNonEmptyLines(filteredFilenameOpsStr)
		filteredFilenameOps, err := parsing.ParseFilteredFilenameOps(lines)
		if err != nil {
			return nil, err
		}
		metarr.FilteredFilenameOps = filteredFilenameOps
	}
	if metaOpsStr, ok := data["metarr_meta_ops"].(string); ok && metaOpsStr != "" {
		lines := splitNonEmptyLines(metaOpsStr)
		metaOps, err := parsing.ParseMetaOps(lines)
		if err != nil {
			return nil, err
		}
		metarr.MetaOps = metaOps
	}
	if filteredMetaOpsStr, ok := data["filtered_meta_ops"].(string); ok && filteredMetaOpsStr != "" {
		lines := splitNonEmptyLines(filteredMetaOpsStr)
		filteredMetaOps, err := parsing.ParseFilteredMetaOps(lines)
		if err != nil {
			return nil, err
		}
		metarr.FilteredMetaOps = filteredMetaOps
	}

	return metarr, nil
}

// getSettingsStrings retrieves Settings model strings, converting where needed.
func getSettingsStrings(w http.ResponseWriter, r *http.Request) *models.Settings {
	// -- Initialize --
	// Strings not needing validation
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
	maxConcurrency = sharedvalidation.ValidateConcurrencyLimit(maxConcurrency)
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
	useGlobalCookies := (useGlobalCookiesStr == "true")

	// Model conversions (newline-separated, not space-separated)
	filters, err := parsing.ParseFilterOps(splitNonEmptyLines(filtersStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid download filters %q: %v", filtersStr, err), http.StatusBadRequest)
		return nil
	}
	moveOps, err := parsing.ParseMetaFilterMoveOps(splitNonEmptyLines(moveOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid move ops %q: %v", moveOpsStr, err), http.StatusBadRequest)
		return nil
	}

	return &models.Settings{
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

		Filters:              filters,
		FilterFile:           filterFile,
		MetaFilterMoveOps:    moveOps,
		MetaFilterMoveOpFile: moveOpFile,

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
	if _, err := sharedvalidation.ValidateMinFreeMem(minFreeMem); err != nil {
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
	transcodeQuality, err := sharedvalidation.ValidateTranscodeQuality(transcodeQualityStr)
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
	maxConcurrency = sharedvalidation.ValidateConcurrencyLimit(maxConcurrency)

	maxCPU := 100.00
	if maxCPUStr != "" {
		maxCPU, err = strconv.ParseFloat(maxCPUStr, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert max CPU limit string %q: %v", maxCPUStr, err), http.StatusBadRequest)
			return nil
		}
	}

	// Models (newline-separated, not space-separated)
	filenameOps, err := parsing.ParseFilenameOps(splitNonEmptyLines(filenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filename ops %q: %v", filenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredFilenameOps, err := parsing.ParseFilteredFilenameOps(splitNonEmptyLines(filteredFilenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filtered filename ops %q: %v", filteredFilenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	metaOps, err := parsing.ParseMetaOps(splitNonEmptyLines(metaOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid meta ops %q: %v", metaOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredMetaOps, err := parsing.ParseFilteredMetaOps(splitNonEmptyLines(filteredMetaOpsStr))
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
