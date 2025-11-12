package cfg

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// Channel:
// setPrimaryChannelFlags sets the main flags for channels in, or intended for, the database.
func setPrimaryChannelFlags(cmd *cobra.Command, name *string, urls *[]string, id *int) {
	if id != nil {
		cmd.Flags().IntVarP(id, keys.ID, "i", 0, "Channel ID in the DB")
	}
	if name != nil {
		cmd.Flags().StringVarP(name, keys.Name, "n", "", "Channel name")
	}
	if urls != nil {
		cmd.Flags().StringSliceVarP(urls, keys.URL, "u", nil, "Channel URL")
	}
}

// Authorization:
// setAuthFlags sets flags related to channel authorization.
func setAuthFlags(cmd *cobra.Command, username, password, loginURL *string, authDetails *[]string) {
	if username != nil {
		cmd.Flags().StringVar(username, keys.AuthUsername, "", "Username for authentication.")
	}

	if password != nil {
		cmd.Flags().StringVar(password, keys.AuthPassword, "", "Password for authentication.")
	}

	if loginURL != nil {
		cmd.Flags().StringVar(loginURL, keys.AuthURL, "", "Login URL for authentication.")
	}

	if authDetails != nil {
		cmd.Flags().StringSliceVar(authDetails, keys.AuthDetails, nil, "Fully qualified authentication details: 'channel URL|username|password|login URL'")
	}
}

// Downloads:
// setDownloadFlags sets flags related to download tasks.
func setDownloadFlags(cmd *cobra.Command, retries *int, useGlobalCookies *bool, ytdlpOutputExt, fromDate, toDate, cookiesFromBrowser, maxFilesize, dlFilterFile *string, dlFilters *[]string) {

	if fromDate != nil {
		cmd.Flags().StringVar(fromDate, keys.FromDate, "", "Only grab videos uploaded on or after this date")
	}

	if toDate != nil {
		cmd.Flags().StringVar(toDate, keys.ToDate, "", "Only grab videos uploaded up to this date")
	}

	if useGlobalCookies != nil {
		cmd.Flags().BoolVar(useGlobalCookies, keys.UseGlobalCookies, false, "Attempt to grab cookies globally (Kooky searches common browser locations)")
	}

	if ytdlpOutputExt != nil {
		cmd.Flags().StringVar(ytdlpOutputExt, keys.YtdlpOutputExt, "", "The preferred downloaded output format for videos")
	}

	if retries != nil {
		cmd.Flags().IntVar(retries, keys.DLRetries, 0, "Number of retries to attempt a download before failure")
	}

	if cookiesFromBrowser != nil {
		cmd.Flags().StringVar(cookiesFromBrowser, keys.CookiesFromBrowser, "", "Cookie source to use for downloading videos (e.g. firefox)")
	}

	if maxFilesize != nil {
		cmd.Flags().StringVar(maxFilesize, keys.MaxFilesize, "", "Enter your desired yt-dlp max filesize parameter")
	}

	if dlFilters != nil {
		cmd.Flags().StringSliceVar(dlFilters, keys.FilterOpsInput, nil, "Filter in or out videos with certain metafields")
	}

	if dlFilterFile != nil {
		cmd.Flags().StringVar(dlFilterFile, keys.FilterOpsFile, "", "Path to a filter operations file (one operation per line [Format is: 'field:contains/omits:VALUE:must/any'])")
	}
}

// External programs
// setMetarrFlags sets flags for interaction with the Metarr software.
func setMetarrFlags(cmd *cobra.Command, maxCPU *float64, metarrConcurrency *int,
	outputExt, extraFFmpegargs, minFreeMem, outDir, renameStyle, metaOpsFile,
	filteredMetaOpsFile, filenameOpsFile, filteredFilenameOpsFile *string,
	urlOutDirs, filenameOps, filteredFilenameOps, metaOps, filteredMetaOps *[]string) {

	// Numbers
	if maxCPU != nil {
		cmd.Flags().Float64Var(maxCPU, keys.MMaxCPU, 0, "Max CPU usage for Metarr")
	}
	if metarrConcurrency != nil {
		cmd.Flags().IntVar(metarrConcurrency, keys.MConcurrency, 0, "Max concurrent processes for Metarr")
	}

	// String
	if outputExt != nil {
		cmd.Flags().StringVar(outputExt, keys.MOutputExt, "", "Output filetype for videos passed into Metarr")
	}
	if extraFFmpegargs != nil {
		cmd.Flags().StringVar(extraFFmpegargs, keys.MExtraFFmpegArgs, "", "Arguments to add on to FFmpeg commands")
	}
	if minFreeMem != nil {
		cmd.Flags().StringVar(minFreeMem, keys.MMinFreeMem, "", "Min free mem for Metarr process")
	}
	if outDir != nil {
		cmd.Flags().StringVar(outDir, keys.MOutputDir, "", "Metarr will move files to this location on completion (some {{}} templating commands available)")
	}
	if renameStyle != nil {
		cmd.Flags().StringVar(renameStyle, keys.MRenameStyle, "", "Renaming style applied by Metarr (skip, fixes-only, underscores, spaces)")
	}
	if metaOpsFile != nil {
		cmd.Flags().StringVar(metaOpsFile, keys.MMetaOpsFile, "", "File containing meta operations (one per line)")
	}
	if filteredMetaOpsFile != nil {
		cmd.Flags().StringVar(filteredMetaOpsFile, keys.MFilteredMetaOpsFile, "", "File containing filtered meta operations (one per line, format: Filter Rules|Meta Ops)")
	}
	if filenameOpsFile != nil {
		cmd.Flags().StringVar(filenameOpsFile, keys.MFilenameOpsFile, "", "File containing filename operations (one per line)")
	}
	if filteredFilenameOpsFile != nil {
		cmd.Flags().StringVar(filteredFilenameOpsFile, keys.MFilteredFilenameOpsFile, "", "File containing filtered filename operations (one per line, format: Filter Rules|Filename Ops)")
	}

	// Arrays
	if urlOutDirs != nil {
		cmd.Flags().StringSliceVar(urlOutDirs, keys.MURLOutputDirs, nil, "Metarr will move a channel URL's files to this location on completion (some {{}} templating commands available)")
	}
	if filenameOps != nil {
		cmd.Flags().StringSliceVar(filenameOps, keys.MFilenameOps, nil, "Filename operations for Metarr (e.g. 'prefix:[CATEGORY] ' or 'date-tag:prefix:ymd')")
	}
	if filteredFilenameOps != nil {
		cmd.Flags().StringSliceVar(filteredFilenameOps, keys.MFilteredFilenameOps, nil, "Filename operations to perform in Metarr, based on filters matched in metadata")
	}
	if metaOps != nil {
		cmd.Flags().StringSliceVar(metaOps, keys.MMetaOps, nil, "Meta operations to perform in Metarr")
	}
	if filteredMetaOps != nil {
		cmd.Flags().StringSliceVar(filteredMetaOps, keys.MFilteredMetaOps, nil, "Meta operations to perform in Metarr, based on filters matched in metadata")
	}
}

// setNotifyFlags sets flags related to notification URLs (e.g. Plex library URL to ping for an update).
func setNotifyFlags(cmd *cobra.Command, notification *[]string) {
	if notification != nil {
		cmd.Flags().StringSliceVar(notification, keys.NotifyPair, nil, "URL to notify on completion (format is 'URL|Name', 'Name' can be empty)")
	}
}

// setTranscodeFlags sets flags related to video transcoding.
func setTranscodeFlags(cmd *cobra.Command, gpu, gpuDir, videoFilter, quality *string, videoCodec, audioCodec *[]string) {
	if gpu != nil {
		cmd.Flags().StringVar(gpu, keys.TranscodeGPU, "", "GPU for transcoding.")
	}

	if gpuDir != nil {
		cmd.Flags().StringVar(gpuDir, keys.TranscodeGPUDir, "", "Directory of the GPU for transcoding (e.g. '/dev/dri/renderD128')")
	}

	if videoFilter != nil {
		cmd.Flags().StringVar(videoFilter, keys.TranscodeVideoFilter, "", "Video filter")
	}

	if quality != nil {
		cmd.Flags().StringVar(quality, keys.TranscodeQuality, "", "Transcode quality profile from p1 (worst) to p7 (best).")
	}

	if audioCodec != nil {
		cmd.Flags().StringSliceVar(audioCodec, keys.TranscodeAudioCodec, nil, "Codec for transcoding audio (e.g. 'aac' or 'copy').")
	}

	if videoCodec != nil {
		cmd.Flags().StringSliceVar(videoCodec, keys.TranscodeCodec, nil, "Codec for transcoding (h264/hevc).")
	}
}

// Files & Directories
// setFileDirFlags sets the primary video and JSON directories.
func setFileDirFlags(cmd *cobra.Command, configFile, jsonDir, videoDir *string) {
	if configFile != nil {
		cmd.Flags().StringVar(configFile, keys.ChannelConfigFile, "", "This is where the channel config file is stored (do not use templating)")
	}
	if videoDir != nil {
		cmd.Flags().StringVar(videoDir, keys.VideoDir, "", "This is where videos will be saved (some {{}} templating commands available)")
	}
	if jsonDir != nil {
		cmd.Flags().StringVar(jsonDir, keys.JSONDir, "", "This is where metadata files will be saved (some {{}} templating commands available)")
	}
}

// Program
// setProgramRelatedFlags sets flags for the Tubarr instance.
func setProgramRelatedFlags(cmd *cobra.Command, concurrency, crawlFreq *int, downloadArgs, downloadCmd, moveOpsFile *string, moveOps *[]string, pause *bool) {

	if concurrency != nil {
		cmd.Flags().IntVarP(concurrency, keys.Concurrency, "l", 0, "Maximum concurrent videos to download/process for this instance")
	}

	if crawlFreq != nil {
		cmd.Flags().IntVar(crawlFreq, keys.CrawlFreq, -1, "New crawl frequency in minutes")
	}

	if downloadCmd != nil {
		cmd.Flags().StringVar(downloadCmd, keys.ExternalDownloader, "", "External downloader command")
	}

	if downloadArgs != nil {
		cmd.Flags().StringVar(downloadArgs, keys.ExternalDownloaderArgs, "", "External downloader arguments")
	}

	if moveOpsFile != nil {
		cmd.Flags().StringVar(moveOpsFile, keys.MoveOpsFile, "", "File containing move filter operations, one per line (format is 'metafield:value:output directory')")
	}

	if moveOps != nil {
		cmd.Flags().StringSliceVar(moveOps, keys.MoveOps, nil, "Move to an output directory in Metarr based on metadata (format is 'metafield:value:output directory')")
	}

	if pause != nil {
		cmd.Flags().BoolVar(pause, keys.Pause, false, "Pause/unpause this channel")
	}
}

// setCustomYDLPArgFlags sets flags for custom additional YTDLP download arguments.
func setCustomYDLPArgFlags(cmd *cobra.Command, extraVideoArgs, extraMetaArgs *string) {
	if extraVideoArgs != nil {
		cmd.Flags().StringVar(extraVideoArgs, keys.ExtraYTDLPVideoArgs, "", "Additional commands to pass to yt-dlp when downloading videos")
	}
	if extraMetaArgs != nil {
		cmd.Flags().StringVar(extraMetaArgs, keys.ExtraYTDLPMetaArgs, "", "Additional commands to pass to yt-dlp when downloading metadata")
	}
}
