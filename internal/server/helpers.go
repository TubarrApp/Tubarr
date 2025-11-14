package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"tubarr/internal/auth"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"
)

// fillChannelFromConfigFile returns a channel from Viper variables.
func fillChannelFromConfigFile(w http.ResponseWriter, input models.ChannelInputPtrs) (channel *models.Channel, authenticationMap map[string]*models.ChannelAccessDetails) {
	// Check validity
	if input.VideoDir == nil || *input.VideoDir == "" ||
		input.Name == nil || *input.Name == "" ||
		input.URLs == nil || len(*input.URLs) == 0 {
		http.Error(w, "new channels require a video directory, name, and at least one channel URL", http.StatusBadRequest)
		return nil, make(map[string]*models.ChannelAccessDetails)
	}

	// Set defaults
	if input.JSONDir == nil {
		input.JSONDir = input.VideoDir
	}

	// Validate entries
	if _, err := validation.ValidateDirectory(*input.VideoDir, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, make(map[string]*models.ChannelAccessDetails)
	}

	var dlFilterModels []models.Filters
	if input.DLFilters != nil {
		m, err := validation.ValidateFilterOps(*input.DLFilters)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		dlFilterModels = m
	}

	moveOpsModels, err := validation.ValidateMoveOps(parsing.NilOrZeroValue(input.MoveOps))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, make(map[string]*models.ChannelAccessDetails)
	}

	var metaOpsModels []models.MetaOps
	if input.MetaOps != nil {
		m, err := validation.ValidateMetaOps(*input.MetaOps)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		metaOpsModels = m
	}

	var filenameOpsModels []models.FilenameOps
	if input.FilenameOps != nil {
		m, err := validation.ValidateFilenameOps(*input.FilenameOps)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		filenameOpsModels = m
	}

	var filteredMetaOpsModels []models.FilteredMetaOps
	if input.FilteredMetaOps != nil {
		m, err := validation.ValidateFilteredMetaOps(*input.FilteredMetaOps)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		filteredMetaOpsModels = m
	}

	var filteredFilenameOpsModels []models.FilteredFilenameOps
	if input.FilteredFilenameOps != nil {
		m, err := validation.ValidateFilteredFilenameOps(*input.FilteredFilenameOps)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		filteredFilenameOpsModels = m
	}

	if input.RenameStyle != nil && *input.RenameStyle != "" {
		if err := validation.ValidateRenameFlag(*input.RenameStyle); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
	}

	if input.MinFreeMem != nil && *input.MinFreeMem != "" {
		if err := validation.ValidateMinFreeMem(*input.MinFreeMem); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
	}

	if input.FromDate != nil && *input.FromDate != "" {
		v, err := validation.ValidateToFromDate(*input.FromDate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		input.FromDate = &v
	}

	if input.ToDate != nil && *input.ToDate != "" {
		v, err := validation.ValidateToFromDate(*input.ToDate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		input.ToDate = &v
	}

	if input.TranscodeGPU != nil && *input.TranscodeGPU != "" {
		g, d, err := validation.ValidateGPU(*input.TranscodeGPU, parsing.NilOrZeroValue(input.GPUDir))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		input.TranscodeGPU = &g
		input.GPUDir = &d
	}

	if input.VideoCodec != nil && len(*input.VideoCodec) != 0 {
		c, err := validation.ValidateVideoTranscodeCodecSlice(*input.VideoCodec, parsing.NilOrZeroValue(input.TranscodeGPU))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		input.VideoCodec = &c
	}

	if input.AudioCodec != nil && len(*input.AudioCodec) != 0 {
		c, err := validation.ValidateAudioTranscodeCodecSlice(*input.AudioCodec)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		input.AudioCodec = &c
	}

	if input.TranscodeQuality != nil && *input.TranscodeQuality != "" {
		q, err := validation.ValidateTranscodeQuality(*input.TranscodeQuality)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		input.TranscodeQuality = &q
	}

	if input.YTDLPOutputExt != nil && *input.YTDLPOutputExt != "" {
		v := strings.ToLower(*input.YTDLPOutputExt)
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, make(map[string]*models.ChannelAccessDetails)
		}
		input.YTDLPOutputExt = &v
	}

	authMap, err := auth.ParseAuthDetails(
		parsing.NilOrZeroValue(input.Username),
		parsing.NilOrZeroValue(input.Password),
		parsing.NilOrZeroValue(input.LoginURL),
		parsing.NilOrZeroValue(input.AuthDetails),
		parsing.NilOrZeroValue(input.URLs),
		false,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, make(map[string]*models.ChannelAccessDetails)
	}

	now := time.Now()
	var chanURLs []*models.ChannelURL
	logging.I("Channel URLs from config: %v", *input.URLs)
	for _, u := range *input.URLs {
		if u == "" {
			continue
		}
		logging.I("Processing URL from channel: %q", u)

		var parsedUsername, parsedPassword, parsedLoginURL string
		if a, exists := authMap[u]; exists {
			parsedUsername = a.Username
			parsedPassword = a.Password
			parsedLoginURL = a.LoginURL
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

		// Apply per-URL settings if present
		if input.URLSettings != nil {
			// Normalize URL to lowercase for case-insensitive matching
			normalizedURL := strings.ToLower(u)

			if override, hasOverride := input.URLSettings[normalizedURL]; hasOverride {
				logging.I("Found per-URL settings override for URL: %s", u)
				// Apply settings overrides
				if override.Settings != nil {
					if chanURL.ChanURLSettings, err = buildSettingsFromInput(override.Settings); err != nil {
						http.Error(w, fmt.Sprintf("failed to build settings from input: %q", err.Error()), http.StatusBadRequest)
						return nil, make(map[string]*models.ChannelAccessDetails)
					}
				}
				// Apply metarr overrides
				if override.Metarr != nil {
					if chanURL.ChanURLMetarrArgs, err = buildMetarrArgsFromInput(override.Metarr); err != nil {
						http.Error(w, fmt.Sprintf("failed to build Metarr args from input: %q", err.Error()), http.StatusBadRequest)
						return nil, make(map[string]*models.ChannelAccessDetails)
					}
				}
			} else {
				logging.I("No per-URL settings override found for URL: %s", u)
			}
		} else {
			logging.I("No URL settings map provided in input")
		}

		chanURLs = append(chanURLs, chanURL)
	}

	c := &models.Channel{
		URLModels: chanURLs,
		Name:      *input.Name,

		ChanSettings: &models.Settings{
			ConfigFile:             parsing.NilOrZeroValue(input.ConfigFile),
			Concurrency:            parsing.NilOrZeroValue(input.Concurrency),
			CookiesFromBrowser:     parsing.NilOrZeroValue(input.CookiesFromBrowser),
			CrawlFreq:              parsing.NilOrZeroValue(input.CrawlFreq),
			ExternalDownloader:     parsing.NilOrZeroValue(input.ExternalDownloader),
			ExternalDownloaderArgs: parsing.NilOrZeroValue(input.ExternalDownloaderArgs),
			Filters:                dlFilterModels,
			FilterFile:             parsing.NilOrZeroValue(input.DLFilterFile),
			FromDate:               parsing.NilOrZeroValue(input.FromDate),
			JSONDir:                *input.JSONDir,
			MaxFilesize:            parsing.NilOrZeroValue(input.MaxFilesize),
			MoveOps:                moveOpsModels,
			MoveOpFile:             parsing.NilOrZeroValue(input.MoveOpFile),
			Paused:                 parsing.NilOrZeroValue(input.Pause),
			Retries:                parsing.NilOrZeroValue(input.Retries),
			ToDate:                 parsing.NilOrZeroValue(input.ToDate),
			UseGlobalCookies:       parsing.NilOrZeroValue(input.UseGlobalCookies),
			VideoDir:               *input.VideoDir,
			YtdlpOutputExt:         parsing.NilOrZeroValue(input.YTDLPOutputExt),
			ExtraYTDLPVideoArgs:    parsing.NilOrZeroValue(input.ExtraYTDLPVideoArgs),
			ExtraYTDLPMetaArgs:     parsing.NilOrZeroValue(input.ExtraYTDLPMetaArgs),
		},

		ChanMetarrArgs: &models.MetarrArgs{
			OutputExt:               parsing.NilOrZeroValue(input.MetarrExt),
			ExtraFFmpegArgs:         parsing.NilOrZeroValue(input.ExtraFFmpegArgs),
			MetaOps:                 metaOpsModels,
			MetaOpsFile:             parsing.NilOrZeroValue(input.MetaOpsFile),
			FilteredMetaOps:         filteredMetaOpsModels,
			FilteredMetaOpsFile:     parsing.NilOrZeroValue(input.FilteredMetaOpsFile),
			FilenameOps:             filenameOpsModels,
			FilenameOpsFile:         parsing.NilOrZeroValue(input.FilenameOpsFile),
			FilteredFilenameOps:     filteredFilenameOpsModels,
			FilteredFilenameOpsFile: parsing.NilOrZeroValue(input.FilteredFilenameOpsFile),
			RenameStyle:             parsing.NilOrZeroValue(input.RenameStyle),
			MaxCPU:                  parsing.NilOrZeroValue(input.MaxCPU),
			MinFreeMem:              parsing.NilOrZeroValue(input.MinFreeMem),
			OutputDir:               parsing.NilOrZeroValue(input.OutDir),
			URLOutputDirs:           parsing.NilOrZeroValue(input.URLs),
			Concurrency:             parsing.NilOrZeroValue(input.MetarrConcurrency),
			UseGPU:                  parsing.NilOrZeroValue(input.TranscodeGPU),
			GPUDir:                  parsing.NilOrZeroValue(input.GPUDir),
			TranscodeVideoCodecs:    parsing.NilOrZeroValue(input.VideoCodec),
			TranscodeAudioCodecs:    parsing.NilOrZeroValue(input.AudioCodec),
			TranscodeQuality:        parsing.NilOrZeroValue(input.TranscodeQuality),
			TranscodeVideoFilter:    parsing.NilOrZeroValue(input.TranscodeVideoFilter),
		},

		LastScan:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return c, authMap
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
	if settings.ConfigFile != "" {
		settingsMap["config_file"] = settings.ConfigFile
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
	if settings.MoveOpFile != "" {
		settingsMap["move_ops_file"] = settings.MoveOpFile
	}
	if len(settings.MoveOps) > 0 {
		moveOpStrings := make([]string, len(settings.MoveOps))
		for i := range settings.MoveOps {
			moveOpStrings[i] = models.MetaFilterMoveOpsToSlice(settings.MoveOps[i])
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

// buildSettingsFromInput constructs a Settings object from ChannelInputPtrs (for per-URL overrides).
//
// Only sets fields that were explicitly provided (non-nil pointers with non-empty values).
// Returns an error if any validation fails.
func buildSettingsFromInput(input *models.ChannelInputPtrs) (*models.Settings, error) {
	settings := &models.Settings{}

	// Validate and set ConfigFile if provided
	if input.ConfigFile != nil && *input.ConfigFile != "" {
		if _, err := validation.ValidateFile(*input.ConfigFile, false); err != nil {
			return nil, fmt.Errorf("invalid config file for per-URL settings: %w", err)
		}
		settings.ConfigFile = *input.ConfigFile
	}

	// Set CookiesFromBrowser (no validation available)
	if input.CookiesFromBrowser != nil {
		settings.CookiesFromBrowser = *input.CookiesFromBrowser
	}

	// Set ExternalDownloader (no validation available)
	if input.ExternalDownloader != nil {
		settings.ExternalDownloader = *input.ExternalDownloader
	}

	// Set ExternalDownloaderArgs (no validation available)
	if input.ExternalDownloaderArgs != nil {
		settings.ExternalDownloaderArgs = *input.ExternalDownloaderArgs
	}

	// Validate and set FilterFile if provided
	if input.DLFilterFile != nil && *input.DLFilterFile != "" {
		if _, err := validation.ValidateFile(*input.DLFilterFile, false); err != nil {
			return nil, fmt.Errorf("invalid filter file for per-URL settings: %w", err)
		}
		settings.FilterFile = *input.DLFilterFile
	}

	// Validate and set JSONDir if provided
	if input.JSONDir != nil && *input.JSONDir != "" {
		if _, err := validation.ValidateDirectory(*input.JSONDir, true); err != nil {
			return nil, fmt.Errorf("invalid JSON directory for per-URL settings: %w", err)
		}
		settings.JSONDir = *input.JSONDir
	}

	// Validate and set MaxFilesize if provided
	if input.MaxFilesize != nil && *input.MaxFilesize != "" {
		v, err := validation.ValidateMaxFilesize(*input.MaxFilesize)
		if err != nil {
			return nil, fmt.Errorf("invalid max filesize for per-URL settings: %w", err)
		}
		settings.MaxFilesize = v
	}

	// Validate and set MoveOpFile if provided
	if input.MoveOpFile != nil && *input.MoveOpFile != "" {
		if _, err := validation.ValidateFile(*input.MoveOpFile, false); err != nil {
			return nil, fmt.Errorf("invalid move ops file for per-URL settings: %w", err)
		}
		settings.MoveOpFile = *input.MoveOpFile
	}

	// Validate and set VideoDir if provided
	if input.VideoDir != nil && *input.VideoDir != "" {
		if _, err := validation.ValidateDirectory(*input.VideoDir, true); err != nil {
			return nil, fmt.Errorf("invalid video directory for per-URL settings: %w", err)
		}
		settings.VideoDir = *input.VideoDir
	}

	// Set ExtraYTDLPVideoArgs (no validation available)
	if input.ExtraYTDLPVideoArgs != nil {
		settings.ExtraYTDLPVideoArgs = *input.ExtraYTDLPVideoArgs
	}

	// Set ExtraYTDLPMetaArgs (no validation available)
	if input.ExtraYTDLPMetaArgs != nil {
		settings.ExtraYTDLPMetaArgs = *input.ExtraYTDLPMetaArgs
	}

	// Validate and set Concurrency if provided
	if input.Concurrency != nil {
		settings.Concurrency = validation.ValidateConcurrencyLimit(*input.Concurrency)
	}

	// Set CrawlFreq (no validation available)
	if input.CrawlFreq != nil {
		settings.CrawlFreq = max(*input.CrawlFreq, 0)
	}

	// Set Retries (no validation available)
	if input.Retries != nil {
		settings.Retries = max(*input.Retries, 0)
	}

	// Set bools
	if input.Pause != nil {
		settings.Paused = *input.Pause
	}
	if input.UseGlobalCookies != nil {
		settings.UseGlobalCookies = *input.UseGlobalCookies
	}

	// Validate and set FromDate if provided
	if input.FromDate != nil && *input.FromDate != "" {
		v, err := validation.ValidateToFromDate(*input.FromDate)
		if err != nil {
			return nil, fmt.Errorf("invalid from date for per-URL settings: %w", err)
		}
		settings.FromDate = v
	}

	// Validate and set ToDate if provided
	if input.ToDate != nil && *input.ToDate != "" {
		v, err := validation.ValidateToFromDate(*input.ToDate)
		if err != nil {
			return nil, fmt.Errorf("invalid to date for per-URL settings: %w", err)
		}
		settings.ToDate = v
	}

	// Validate and set YTDLP output extension if provided
	if input.YTDLPOutputExt != nil && *input.YTDLPOutputExt != "" {
		v := strings.ToLower(*input.YTDLPOutputExt)
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return nil, fmt.Errorf("invalid ytdlp output extension for per-URL settings: %w", err)
		}
		settings.YtdlpOutputExt = v
	}

	// Validate and set filter ops if provided
	if input.DLFilters != nil {
		m, err := validation.ValidateFilterOps(*input.DLFilters)
		if err != nil {
			return nil, fmt.Errorf("invalid filter ops for per-URL settings: %w", err)
		}
		settings.Filters = m
	}

	// Validate and set move ops if provided
	if input.MoveOps != nil {
		m, err := validation.ValidateMoveOps(*input.MoveOps)
		if err != nil {
			return nil, fmt.Errorf("invalid move ops for per-URL settings: %w", err)
		}
		settings.MoveOps = m
	}

	return settings, nil
}

// buildMetarrArgsFromInput constructs a MetarrArgs object from ChannelInputPtrs (for per-URL overrides).
//
// Only sets fields that were explicitly provided (non-nil pointers).
func buildMetarrArgsFromInput(input *models.ChannelInputPtrs) (*models.MetarrArgs, error) {
	metarr := &models.MetarrArgs{}

	// Only set string fields if explicitly provided
	if input.MetarrExt != nil {
		metarr.OutputExt = *input.MetarrExt
	}
	if input.ExtraFFmpegArgs != nil {
		metarr.ExtraFFmpegArgs = *input.ExtraFFmpegArgs
	}
	if input.MetaOpsFile != nil {
		if _, err := validation.ValidateFile(*input.MetaOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid meta ops file for per-URL settings: %w", err)
		}
		metarr.MetaOpsFile = *input.MetaOpsFile
	}
	if input.FilteredMetaOpsFile != nil {
		if _, err := validation.ValidateFile(*input.FilteredMetaOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid filtered meta ops file for per-URL settings: %w", err)
		}
		metarr.FilteredMetaOpsFile = *input.FilteredMetaOpsFile
	}
	if input.FilenameOpsFile != nil {
		if _, err := validation.ValidateFile(*input.FilenameOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid filename ops file for per-URL settings: %w", err)
		}
		metarr.FilenameOpsFile = *input.FilenameOpsFile
	}
	if input.FilteredFilenameOpsFile != nil {
		if _, err := validation.ValidateFile(*input.FilteredFilenameOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid filtered filename ops file for per-URL settings: %w", err)
		}
		metarr.FilteredFilenameOpsFile = *input.FilteredFilenameOpsFile
	}
	if input.OutDir != nil {
		if _, err := validation.ValidateDirectory(*input.OutDir, true); err != nil {
			return nil, fmt.Errorf("invalid Metarr output directory for per-URL settings: %w", err)
		}
		metarr.OutputDir = *input.OutDir
	}
	if input.TranscodeVideoFilter != nil {
		metarr.TranscodeVideoFilter = *input.TranscodeVideoFilter
	}

	// Only set int fields if explicitly provided
	if input.MetarrConcurrency != nil {
		metarr.Concurrency = max(*input.MetarrConcurrency, 0)
	}

	// Only set float64 fields if explicitly provided
	if input.MaxCPU != nil {
		metarr.MaxCPU = min(max(*input.MaxCPU, 0.0), 100.0)
	}

	// Validate and set rename style if provided
	if input.RenameStyle != nil {
		if err := validation.ValidateRenameFlag(*input.RenameStyle); err != nil {
			return nil, fmt.Errorf("failed to validate rename style for per-URL settings: %w", err)
		}
		metarr.RenameStyle = *input.RenameStyle
	}

	// Validate and set min free mem if provided
	if input.MinFreeMem != nil {
		if err := validation.ValidateMinFreeMem(*input.MinFreeMem); err != nil {
			return nil, fmt.Errorf("failed to validate min free mem for per-URL settings: %w", err)
		}
		metarr.MinFreeMem = *input.MinFreeMem
	}

	// Validate and set GPU settings if provided
	if input.TranscodeGPU != nil {
		g, d, err := validation.ValidateGPU(*input.TranscodeGPU, parsing.NilOrZeroValue(input.GPUDir))
		if err != nil {
			return nil, fmt.Errorf("failed to validate GPU settings for per-URL settings: %w", err)
		}
		metarr.UseGPU = g
		metarr.GPUDir = d
	}

	// Validate and set video codecs if provided
	if input.VideoCodec != nil {
		c, err := validation.ValidateVideoTranscodeCodecSlice(*input.VideoCodec, metarr.UseGPU)
		if err != nil {
			return nil, fmt.Errorf("failed to validate video codecs for per-URL settings: %w", err)
		}
		metarr.TranscodeVideoCodecs = c
	}

	// Validate and set audio codecs if provided
	if input.AudioCodec != nil {
		logging.I("Found audio codecs in per-URL override: %v", *input.AudioCodec)
		c, err := validation.ValidateAudioTranscodeCodecSlice(*input.AudioCodec)
		if err != nil {
			return nil, fmt.Errorf("failed to validate audio codecs for per-URL settings: %w", err)
		}
		metarr.TranscodeAudioCodecs = c
	}

	// Validate and set transcode quality if provided
	if input.TranscodeQuality != nil {
		q, err := validation.ValidateTranscodeQuality(*input.TranscodeQuality)
		if err != nil {
			return nil, fmt.Errorf("failed to validate transcode quality for per-URL settings: %w", err)
		}
		metarr.TranscodeQuality = q
	}

	// Validate and set meta ops if provided
	if input.MetaOps != nil {
		m, err := validation.ValidateMetaOps(*input.MetaOps)
		if err != nil {
			return nil, fmt.Errorf("failed to validate meta ops for per-URL settings: %w", err)
		}
		metarr.MetaOps = m
	}

	// Validate and set filename ops if provided
	if input.FilenameOps != nil {
		m, err := validation.ValidateFilenameOps(*input.FilenameOps)
		if err != nil {
			return nil, fmt.Errorf("failed to validate filename ops for per-URL settings: %w", err)
		}
		metarr.FilenameOps = m
	}

	// Validate and set filtered meta ops if provided
	if input.FilteredMetaOps != nil {
		m, err := validation.ValidateFilteredMetaOps(*input.FilteredMetaOps)
		if err != nil {
			return nil, fmt.Errorf("failed to validate filtered meta ops for per-URL settings: %w", err)
		}
		metarr.FilteredMetaOps = m
	}

	// Validate and set filtered filename ops if provided
	if input.FilteredFilenameOps != nil {
		m, err := validation.ValidateFilteredFilenameOps(*input.FilteredFilenameOps)
		if err != nil {
			return nil, fmt.Errorf("failed to validate filtered filename ops for per-URL settings: %w", err)
		}
		metarr.FilteredFilenameOps = m
	}

	return metarr, nil
}
