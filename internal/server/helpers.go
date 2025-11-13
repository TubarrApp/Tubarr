package server

import (
	"net/http"
	"strings"
	"time"
	"tubarr/internal/auth"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
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
	for _, u := range *input.URLs {
		if u == "" {
			continue
		}

		var parsedUsername, parsedPassword, parsedLoginURL string
		if a, exists := authMap[u]; exists {
			parsedUsername = a.Username
			parsedPassword = a.Password
			parsedLoginURL = a.LoginURL
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

	c := &models.Channel{
		URLModels: chanURLs,
		Name:      *input.Name,

		ChanSettings: &models.Settings{
			ChannelConfigFile:      parsing.NilOrZeroValue(input.ChannelConfigFile),
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
	if settings.ChannelConfigFile != "" {
		settingsMap["channel_config_file"] = settings.ChannelConfigFile
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
