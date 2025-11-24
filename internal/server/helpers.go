package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// fillChannelFromConfigFile parses a Channel and its access details from a config file input.
func fillChannelFromConfigFile(w http.ResponseWriter, input models.ChannelInputPtrs) (*models.Channel, map[string]*models.ChannelAccessDetails) {
	c, m, err := parsing.BuildChannelFromInput(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, make(map[string]*models.ChannelAccessDetails)
	}
	return c, m
}

// metarrArgsJSONMap returns a map of MetarrArgs JSON keys and their values.
func metarrArgsJSONMap(mArgs *models.MetarrArgs) map[string]any {
	metarrMap := make(map[string]any)
	if mArgs.OutputDir != "" {
		metarrMap[jsonkeys.MetarrOutputDirectory] = mArgs.OutputDir
	}
	if mArgs.OutputExt != "" {
		metarrMap[jsonkeys.MetarrOutputExt] = mArgs.OutputExt
	}
	if mArgs.RenameStyle != "" {
		metarrMap[jsonkeys.MetarrRenameStyle] = mArgs.RenameStyle
	}
	if mArgs.Concurrency > 0 {
		metarrMap[jsonkeys.MetarrConcurrency] = mArgs.Concurrency
	}
	if mArgs.MaxCPU >= 0 {
		metarrMap[jsonkeys.MetarrMaxCPU] = mArgs.MaxCPU
	}
	if mArgs.MinFreeMem != "" {
		metarrMap[jsonkeys.MetarrMinFreeMem] = mArgs.MinFreeMem
	}
	if mArgs.TranscodeGPU != "" {
		metarrMap[jsonkeys.MetarrGPU] = mArgs.TranscodeGPU
	}
	if mArgs.TranscodeGPUDirectory != "" {
		metarrMap[jsonkeys.MetarrGPUDirectory] = mArgs.TranscodeGPUDirectory
	}
	if len(mArgs.TranscodeVideoCodecs) != 0 {
		metarrMap[jsonkeys.MetarrVideoCodecs] = mArgs.TranscodeVideoCodecs
	}
	if len(mArgs.TranscodeAudioCodecs) != 0 {
		metarrMap[jsonkeys.MetarrAudioCodecs] = mArgs.TranscodeAudioCodecs
	}
	if mArgs.TranscodeQuality != "" {
		metarrMap[jsonkeys.MetarrTranscodeQuality] = mArgs.TranscodeQuality
	}
	if mArgs.TranscodeVideoFilter != "" {
		metarrMap[jsonkeys.MetarrTranscodeVideoFilter] = mArgs.TranscodeVideoFilter
	}
	if mArgs.ExtraFFmpegArgs != "" {
		metarrMap[jsonkeys.MetarrExtraFFmpegArgs] = mArgs.ExtraFFmpegArgs
	}
	if mArgs.FilenameOpsFile != "" {
		metarrMap[jsonkeys.MetarrFilenameOpsFile] = mArgs.FilenameOpsFile
	}
	if mArgs.FilteredFilenameOpsFile != "" {
		metarrMap[jsonkeys.MetarrFilteredFilenameOpsFile] = mArgs.FilteredFilenameOpsFile
	}
	if mArgs.MetaOpsFile != "" {
		metarrMap[jsonkeys.MetarrMetaOpsFile] = mArgs.MetaOpsFile
	}
	if mArgs.FilteredMetaOpsFile != "" {
		metarrMap[jsonkeys.MetarrFilteredMetaOpsFile] = mArgs.FilteredMetaOpsFile
	}
	if len(mArgs.MetaOps) > 0 {
		metarrMap[jsonkeys.MetarrMetaOps] = strings.Join(models.MetaOpsArrayToSlice(mArgs.MetaOps), "\n")
	}
	if len(mArgs.FilenameOps) > 0 {
		metarrMap[jsonkeys.MetarrFilenameOps] = strings.Join(models.FilenameOpsArrayToSlice(mArgs.FilenameOps), "\n")
	}
	if len(mArgs.FilteredMetaOps) > 0 {
		filteredMetaStrings := make([]string, 0)
		for i := range mArgs.FilteredMetaOps {
			filteredMetaStrings = append(filteredMetaStrings, models.FilteredMetaOpsToSlice(mArgs.FilteredMetaOps[i])...)
		}
		metarrMap[jsonkeys.MetarrFilteredMetaOps] = strings.Join(filteredMetaStrings, "\n")
	}
	if len(mArgs.FilteredFilenameOps) > 0 {
		filteredFilenameStrings := make([]string, 0)
		for i := range mArgs.FilteredFilenameOps {
			filteredFilenameStrings = append(filteredFilenameStrings, models.FilteredFilenameOpsToSlice(mArgs.FilteredFilenameOps[i])...)
		}
		metarrMap[jsonkeys.MetarrFilteredFilenameOps] = strings.Join(filteredFilenameStrings, "\n")
	}
	return metarrMap
}

// settingsJSONMap returns a map of Settings JSON keys and their values.
func settingsJSONMap(settings *models.Settings) map[string]any {
	settingsMap := make(map[string]any)
	if settings.VideoDir != "" {
		settingsMap[jsonkeys.SettingsVideoDirectory] = settings.VideoDir
	}
	if settings.JSONDir != "" {
		settingsMap[jsonkeys.SettingsJSONDirectory] = settings.JSONDir
	}
	if settings.Concurrency > 0 {
		settingsMap[jsonkeys.SettingsMaxConcurrency] = settings.Concurrency
	}
	if settings.CrawlFreq > 0 {
		settingsMap[jsonkeys.SettingsCrawlFreq] = settings.CrawlFreq
	}
	if settings.Retries > 0 {
		settingsMap[jsonkeys.SettingsDownloadRetries] = settings.Retries
	}
	if settings.MaxFilesize != "" {
		settingsMap[jsonkeys.SettingsMaxFilesize] = settings.MaxFilesize
	}
	if settings.FromDate != "" {
		settingsMap[jsonkeys.SettingsFromDate] = settings.FromDate
	}
	if settings.ToDate != "" {
		settingsMap[jsonkeys.SettingsToDate] = settings.ToDate
	}
	if settings.FilterFile != "" {
		settingsMap[jsonkeys.SettingsFilterFile] = settings.FilterFile
	}
	if len(settings.Filters) > 0 {
		settingsMap[jsonkeys.SettingsFilters] = strings.Join(models.FiltersArrayToSlice(settings.Filters), "\n")
	}
	if settings.YtdlpOutputExt != "" {
		settingsMap[jsonkeys.SettingsYtdlpOutputExt] = settings.YtdlpOutputExt
	}
	if settings.CookiesFromBrowser != "" {
		settingsMap[jsonkeys.SettingsCookiesFromBrowser] = settings.CookiesFromBrowser
	}
	if settings.ExternalDownloader != "" {
		settingsMap[jsonkeys.SettingsExternalDownloader] = settings.ExternalDownloader
	}
	if settings.ExternalDownloaderArgs != "" {
		settingsMap[jsonkeys.SettingsExternalDownloaderArgs] = settings.ExternalDownloaderArgs
	}
	if settings.ExtraYTDLPVideoArgs != "" {
		settingsMap[jsonkeys.SettingsExtraYtdlpVideoArgs] = settings.ExtraYTDLPVideoArgs
	}
	if settings.ExtraYTDLPMetaArgs != "" {
		settingsMap[jsonkeys.SettingsExtraYtdlpMetaArgs] = settings.ExtraYTDLPMetaArgs
	}
	if settings.MetaFilterMoveOpFile != "" {
		settingsMap[jsonkeys.SettingsMoveOpsFile] = settings.MetaFilterMoveOpFile
	}
	if len(settings.MetaFilterMoveOps) > 0 {
		settingsMap[jsonkeys.SettingsMoveOps] = strings.Join(models.MetaFilterMoveOpsArrayToSlice(settings.MetaFilterMoveOps), "\n")
	}
	if settings.UseGlobalCookies {
		settingsMap[jsonkeys.SettingsUseGlobalCookies] = true
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
	if v, ok := data[jsonkeys.SettingsCookiesFromBrowser].(string); ok {
		settings.CookiesFromBrowser = v
	}
	if v, ok := data[jsonkeys.SettingsExternalDownloader].(string); ok {
		settings.ExternalDownloader = v
	}
	if v, ok := data[jsonkeys.SettingsExternalDownloaderArgs].(string); ok {
		settings.ExternalDownloaderArgs = v
	}
	if v, ok := data[jsonkeys.SettingsExtraYtdlpVideoArgs].(string); ok {
		settings.ExtraYTDLPVideoArgs = v
	}
	if v, ok := data[jsonkeys.SettingsExtraYtdlpMetaArgs].(string); ok {
		settings.ExtraYTDLPMetaArgs = v
	}
	if v, ok := data[jsonkeys.SettingsFilterFile].(string); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		settings.FilterFile = v
	}
	if v, ok := data[jsonkeys.SettingsMoveOpsFile].(string); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		settings.MetaFilterMoveOpFile = v
	}
	if v, ok := data[jsonkeys.SettingsJSONDirectory].(string); ok {
		if _, _, err := sharedvalidation.ValidateDirectory(v, true, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		settings.JSONDir = v
	}
	if v, ok := data[jsonkeys.SettingsVideoDirectory].(string); ok {
		if _, _, err := sharedvalidation.ValidateDirectory(v, true, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		settings.VideoDir = v
	}
	if v, ok := data[jsonkeys.SettingsMaxFilesize].(string); ok {
		validFilesize, err := validation.ValidateMaxFilesize(v)
		if err != nil {
			return nil, err
		}
		settings.MaxFilesize = validFilesize
	}
	if v, ok := data[jsonkeys.SettingsYtdlpOutputExt].(string); ok {
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return nil, err
		}
		settings.YtdlpOutputExt = v
	}
	if v, ok := data[jsonkeys.SettingsFromDate].(string); ok {
		validDate, err := validation.ValidateToFromDate(v)
		if err != nil {
			return nil, err
		}
		settings.FromDate = validDate
	}
	if v, ok := data[jsonkeys.SettingsToDate].(string); ok {
		validDate, err := validation.ValidateToFromDate(v)
		if err != nil {
			return nil, err
		}
		settings.ToDate = validDate
	}

	// Extract integer fields.
	if v, ok := data[jsonkeys.SettingsMaxConcurrency].(int); ok {
		valid := sharedvalidation.ValidateConcurrencyLimit(v)
		settings.Concurrency = valid
	}
	if v, ok := data[jsonkeys.SettingsCrawlFreq].(int); ok {
		settings.CrawlFreq = max(v, 0)
	}
	if v, ok := data[jsonkeys.SettingsDownloadRetries].(int); ok {
		settings.Retries = max(v, 0)
	}

	// Extract boolean fields.
	if v, ok := data[jsonkeys.SettingsUseGlobalCookies].(bool); ok {
		settings.UseGlobalCookies = v
	}

	// Parse model fields from strings (newline-separated, not space-separated).
	if filtersStr, ok := data[jsonkeys.SettingsFilters].(string); ok && filtersStr != "" {
		lines := splitNonEmptyLines(filtersStr)
		filters, err := parsing.ParseFilterOps(lines)
		if err != nil {
			return nil, err
		}
		settings.Filters = filters
	}
	if moveOpsStr, ok := data[jsonkeys.SettingsMoveOps].(string); ok && moveOpsStr != "" {
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
	if v, ok := data[jsonkeys.MetarrOutputExt].(string); ok {
		metarr.OutputExt = v
	}
	if v, ok := data[jsonkeys.MetarrFilenameOpsFile].(string); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		metarr.FilenameOpsFile = v
	}
	if v, ok := data[jsonkeys.MetarrFilteredFilenameOpsFile].(string); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		metarr.FilteredFilenameOpsFile = v
	}
	if v, ok := data[jsonkeys.MetarrMetaOpsFile].(string); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		metarr.MetaOpsFile = v
	}
	if v, ok := data[jsonkeys.MetarrFilteredMetaOpsFile].(string); ok {
		if _, _, err := sharedvalidation.ValidateFile(v, false, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		metarr.FilteredMetaOpsFile = v
	}
	if v, ok := data[jsonkeys.MetarrExtraFFmpegArgs].(string); ok {
		metarr.ExtraFFmpegArgs = v
	}
	if v, ok := data[jsonkeys.MetarrRenameStyle].(string); ok {
		if err := validation.ValidateRenameFlag(v); err != nil {
			return nil, err
		}
		metarr.RenameStyle = v
	}
	if v, ok := data[jsonkeys.MetarrMinFreeMem].(string); ok {
		if _, err := sharedvalidation.ValidateMinFreeMem(v); err != nil {
			return nil, err
		}
		metarr.MinFreeMem = v
	}
	if v, ok := data[jsonkeys.MetarrGPUDirectory].(string); ok {
		metarr.TranscodeGPUDirectory = v
	}
	if v, ok := data[jsonkeys.MetarrGPU].(string); ok {
		validGPU, _, err := validation.ValidateGPU(v, metarr.TranscodeGPUDirectory)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeGPU = validGPU
	}
	if v, ok := data[jsonkeys.MetarrOutputDirectory].(string); ok {
		if _, _, err := sharedvalidation.ValidateDirectory(v, true, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		metarr.OutputDir = v
	}
	if v, ok := data[jsonkeys.MetarrTranscodeVideoFilter].(string); ok {
		metarr.TranscodeVideoFilter = v
	}
	if v, ok := data[jsonkeys.MetarrVideoCodecs].([]string); ok {
		validPairs, err := validation.ValidateVideoTranscodeCodecSlice(v, metarr.TranscodeGPU)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeVideoCodecs = validPairs
	}
	if v, ok := data[jsonkeys.MetarrAudioCodecs].([]string); ok {
		validPairs, err := validation.ValidateAudioTranscodeCodecSlice(v)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeAudioCodecs = validPairs
	}
	if v, ok := data[jsonkeys.MetarrTranscodeQuality].(string); ok {
		validQuality, err := sharedvalidation.ValidateTranscodeQuality(v)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeQuality = validQuality
	}

	// Extract integer fields
	if v, ok := data[jsonkeys.MetarrConcurrency].(int); ok {
		metarr.Concurrency = sharedvalidation.ValidateConcurrencyLimit(v)
	}

	// Extract float fields
	if v, ok := data[jsonkeys.MetarrMaxCPU].(float64); ok {
		metarr.MaxCPU = v
	}

	// Parse model fields from strings (newline-separated, not space-separated)
	if filenameOpsStr, ok := data[jsonkeys.MetarrFilenameOps].(string); ok && filenameOpsStr != "" {
		lines := splitNonEmptyLines(filenameOpsStr)
		filenameOps, err := parsing.ParseFilenameOps(lines)
		if err != nil {
			return nil, err
		}
		metarr.FilenameOps = filenameOps
	}
	if filteredFilenameOpsStr, ok := data[jsonkeys.MetarrFilteredFilenameOps].(string); ok && filteredFilenameOpsStr != "" {
		lines := splitNonEmptyLines(filteredFilenameOpsStr)
		filteredFilenameOps, err := parsing.ParseFilteredFilenameOps(lines)
		if err != nil {
			return nil, err
		}
		metarr.FilteredFilenameOps = filteredFilenameOps
	}
	if metaOpsStr, ok := data[jsonkeys.MetarrMetaOps].(string); ok && metaOpsStr != "" {
		lines := splitNonEmptyLines(metaOpsStr)
		metaOps, err := parsing.ParseMetaOps(lines)
		if err != nil {
			return nil, err
		}
		metarr.MetaOps = metaOps
	}
	if filteredMetaOpsStr, ok := data[jsonkeys.MetarrFilteredMetaOps].(string); ok && filteredMetaOpsStr != "" {
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
	cookiesFromBrowser := r.FormValue(jsonkeys.SettingsCookiesFromBrowser)
	externalDownloader := r.FormValue(jsonkeys.SettingsExternalDownloader)
	externalDownloaderArgs := r.FormValue(jsonkeys.SettingsExternalDownloaderArgs)
	extraYtdlpVideoArgs := r.FormValue(jsonkeys.SettingsExtraYtdlpVideoArgs)
	extraYtdlpMetaArgs := r.FormValue(jsonkeys.SettingsExtraYtdlpMetaArgs)
	filterFile := r.FormValue(jsonkeys.SettingsFilterFile)
	moveOpFile := r.FormValue(jsonkeys.SettingsMoveOpsFile)

	// Strings needing validation
	jDir := r.FormValue(jsonkeys.SettingsJSONDirectory)
	vDir := r.FormValue(jsonkeys.SettingsVideoDirectory)
	maxFilesizeStr := r.FormValue(jsonkeys.SettingsMaxFilesize)
	ytdlpOutExt := r.FormValue(jsonkeys.SettingsYtdlpOutputExt)
	fromDateStr := r.FormValue(jsonkeys.SettingsFromDate)
	toDateStr := r.FormValue(jsonkeys.SettingsToDate)

	// Integers
	maxConcurrencyStr := r.FormValue(jsonkeys.SettingsMaxConcurrency)
	crawlFreqStr := r.FormValue(jsonkeys.SettingsCrawlFreq)
	retriesStr := r.FormValue(jsonkeys.SettingsDownloadRetries)

	// Bools
	useGlobalCookiesStr := r.FormValue(jsonkeys.SettingsUseGlobalCookies)

	// Models
	filtersStr := r.FormValue(jsonkeys.SettingsFilters)
	moveOpsStr := r.FormValue(jsonkeys.SettingsMoveOps)

	// -- Validation --
	// Strings
	if _, _, err := sharedvalidation.ValidateDirectory(vDir, true, sharedtemplates.AllTemplatesMap); err != nil {
		http.Error(w, fmt.Sprintf("video directory %q is invalid: %v", vDir, err), http.StatusBadRequest)
		return nil
	}
	if _, _, err := sharedvalidation.ValidateDirectory(jDir, true, sharedtemplates.AllTemplatesMap); err != nil {
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

	// Return Settings.
	return &models.Settings{
		JSONDir:  jDir,
		VideoDir: vDir,

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
	}
}

// getMetarrArgsStrings retrieves MetarrArgs model strings, converting where needed.
func getMetarrArgsStrings(w http.ResponseWriter, r *http.Request) *models.MetarrArgs {
	// -- Initialize --
	// Strings not needing validation.
	outExt := r.FormValue(jsonkeys.MetarrOutputExt)
	filenameOpsFile := r.FormValue(jsonkeys.MetarrFilenameOpsFile)
	filteredFilenameOpsFile := r.FormValue(jsonkeys.MetarrFilteredFilenameOpsFile)
	metaOpsFile := r.FormValue(jsonkeys.MetarrMetaOpsFile)
	filteredMetaOpsFile := r.FormValue(jsonkeys.MetarrFilteredMetaOpsFile)
	extraFFmpegArgs := r.FormValue(jsonkeys.MetarrExtraFFmpegArgs)

	// Strings requiring validation.
	renameStyle := r.FormValue(jsonkeys.MetarrRenameStyle)
	minFreeMem := r.FormValue(jsonkeys.MetarrMinFreeMem)
	transcodeGPUStr := r.FormValue(jsonkeys.MetarrGPU)
	transcodeGPUDirStr := r.FormValue(jsonkeys.MetarrGPUDirectory)
	outputDir := r.FormValue(jsonkeys.MetarrOutputDirectory)
	transcodeVideoFilterStr := r.FormValue(jsonkeys.MetarrTranscodeVideoFilter)
	transcodeCodecStr := r.FormValue(jsonkeys.MetarrVideoCodecs)
	transcodeAudioCodecStr := r.FormValue(jsonkeys.MetarrAudioCodecs)
	transcodeQualityStr := r.FormValue(jsonkeys.MetarrTranscodeQuality)

	// Ints.
	maxConcurrencyStr := r.FormValue(jsonkeys.MetarrConcurrency)
	maxCPUStr := r.FormValue(jsonkeys.MetarrMaxCPU)

	// Models.
	filenameOpsStr := r.FormValue(jsonkeys.MetarrFilenameOps)
	filteredFilenameOpsStr := r.FormValue(jsonkeys.MetarrFilteredFilenameOps)
	filteredMetaOpsStr := r.FormValue(jsonkeys.MetarrFilteredMetaOps)
	metaOpsStr := r.FormValue(jsonkeys.MetarrMetaOps)

	// -- Validation --
	//Strings.
	if err := validation.ValidateRenameFlag(renameStyle); err != nil {
		http.Error(w, fmt.Sprintf("invalid rename style %q: %v", renameStyle, err), http.StatusBadRequest)
		return nil
	}
	if _, err := sharedvalidation.ValidateMinFreeMem(minFreeMem); err != nil {
		http.Error(w, fmt.Sprintf("invalid min free mem %q: %v", minFreeMem, err), http.StatusBadRequest)
		return nil
	}
	transcodeGPU, transcodeGPUDir, err := validation.ValidateGPU(transcodeGPUStr, transcodeGPUDirStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid GPU type or device directory (%q : %q): %v", transcodeGPUStr, transcodeGPUDirStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeVideoFilter, err := validation.ValidateTranscodeVideoFilter(transcodeVideoFilterStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video filter string %q: %v", transcodeVideoFilterStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeVideoCodecs, err := validation.ValidateVideoTranscodeCodecSlice(splitNonEmptyLines(transcodeCodecStr), transcodeGPU)
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
	if _, _, err := sharedvalidation.ValidateDirectory(outputDir, false, sharedtemplates.AllTemplatesMap); err != nil {
		http.Error(w, fmt.Sprintf("cannot get output directories. Input string %q. Error: %v", outputDir, err), http.StatusBadRequest)
		return nil
	}

	// Integers & floats.
	maxConcurrency, err := strconv.Atoi(maxConcurrencyStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert max concurrency string %q: %v", maxConcurrencyStr, err), http.StatusBadRequest)
		return nil
	}
	maxConcurrency = sharedvalidation.ValidateConcurrencyLimit(maxConcurrency)

	maxCPU := 0.00
	if maxCPUStr != "" {
		maxCPU, err = strconv.ParseFloat(maxCPUStr, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert max CPU limit string %q: %v", maxCPUStr, err), http.StatusBadRequest)
			return nil
		}
	}

	// Models (newline-separated, not space-separated).
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

	// Return MetarrArgs.
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
		TranscodeGPU:            transcodeGPU,
		TranscodeGPUDirectory:   transcodeGPUDir,
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

	// Normalize lines and append non-empty ones.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
