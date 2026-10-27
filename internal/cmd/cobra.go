// Package cmd handles settings command flags for interfaces like Cobra and Viper.
package cmd

import (
	"fmt"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// SetPrimaryChannelFlags sets the main flags for channels in, or intended for, the database.
func SetPrimaryChannelFlags(cmd *cobra.Command, name *string, urls *[]string, id *int) {
	if id != nil {
		cmd.Flags().IntVarP(id, keys.ChannelID, "i", 0, "Channel ID in the DB")
	}
	if name != nil {
		cmd.Flags().StringVarP(name, keys.ChannelName, "n", "", "Channel name")
	}
	if urls != nil {
		cmd.Flags().StringSliceVarP(urls, keys.ChannelURLs, "u", nil, "Channel URLs")
	}
}

// SetProfileFlag sets the flag choosing a channel's or URL's profile.
func SetProfileFlag(cmd *cobra.Command, profile *string) {
	cmd.Flags().StringVar(profile, keys.Profile, "", "Name of the profile filling in the settings left unset (empty for none)")
}

// SetAuthFlags sets flags related to channel authorization.
func SetAuthFlags(cmd *cobra.Command, username, password, loginURL *string, authDetails *[]string) {
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
		cmd.Flags().StringArrayVar(authDetails, keys.AuthDetails, nil, `Login details as JSON, one per flag: '{"channel_url":"https://...","username":"...","password":"...","login_url":"https://..."}' (channel_url can be left out for a channel with one URL)`)
	}
}

// SetDownloadFlags sets flags related to download tasks.
func SetDownloadFlags(cmd *cobra.Command, retries *int, useGlobalCookies *bool, ytdlpOutputExt, fromDate, toDate, cookiesFromBrowser, maxFilesize, dlFilterFile *string, dlFilters, ytdlpPreferredVideoCodecs, ytdlpPreferredAudioCodecs *[]string) {

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
		cmd.Flags().IntVar(retries, keys.DLRetries, 0, fmt.Sprintf("Times Tubarr runs a failed video or metadata download again before giving up (0 uses %d)", consts.DefaultDownloadRetries))
	}

	if cookiesFromBrowser != nil {
		cmd.Flags().StringVar(cookiesFromBrowser, keys.CookiesFromBrowser, "", "Browser to read cookies from for yt-dlp crawls and downloads, together with any login or global cookies (e.g. firefox). Custom crawl rules don't use it")
	}

	if maxFilesize != nil {
		cmd.Flags().StringVar(maxFilesize, keys.MaxFilesize, "", "Enter your desired yt-dlp max filesize parameter")
	}

	if dlFilters != nil {
		cmd.Flags().StringSliceVar(dlFilters, keys.FilterOpsInput, nil, "Filter in or out videos with certain metafields")
	}

	if dlFilterFile != nil {
		cmd.Flags().StringVar(dlFilterFile, keys.FilterOpsFile, "", "Path to a filter operations file (one operation per line [Format is: 'field:contains/regex-contains/omits/more-than/less-than/equals/not-equals:VALUE:must/any', with a regex-contains VALUE wrapped in [[ ]]])")
	}

	if ytdlpPreferredVideoCodecs != nil {
		cmd.Flags().StringSliceVar(ytdlpPreferredVideoCodecs, keys.YTDLPPreferredVideoCodecs, nil, "Preferred video codec(s) for downloads (comma-separated: vp9,av1,h264,hevc)")
	}

	if ytdlpPreferredAudioCodecs != nil {
		cmd.Flags().StringSliceVar(ytdlpPreferredAudioCodecs, keys.YTDLPPreferredAudioCodecs, nil, "Preferred audio codec(s) for downloads (comma-separated: opus,aac,vorbis)")
	}
}

// SetMetarrFlags sets flags for interaction with the Metarr software.
func SetMetarrFlags(cmd *cobra.Command, maxCPU *float64, metarrConcurrency *int, purgeOriginalVideo *bool,
	outputExt, extraFFmpegargs, minFreeMem, outDir, metaPurge, renameStyle, metaOpsFile,
	filteredMetaOpsFile, filenameOpsFile, filteredFilenameOpsFile *string,
	filenameOps, filteredFilenameOps, metaOps, filteredMetaOps *[]string) {

	// Numbers
	if maxCPU != nil {
		cmd.Flags().Float64Var(maxCPU, keys.MMaxCPU, 0, "Max CPU usage for Metarr")
	}
	if metarrConcurrency != nil {
		cmd.Flags().IntVar(metarrConcurrency, keys.MConcurrency, 0, "Max concurrent processes for Metarr")
	}

	// Booleans
	if purgeOriginalVideo != nil {
		cmd.Flags().BoolVar(purgeOriginalVideo, keys.MPurgeOriginalVideo, false, "Delete the original video once Metarr writes a new one from it (the default, false keeps it)")
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
	if metaPurge != nil {
		cmd.Flags().StringVar(metaPurge, keys.MMetaPurge, "", "Metadata files Metarr deletes after processing a video: all, json or nfo")
	}
	if renameStyle != nil {
		cmd.Flags().StringVar(renameStyle, keys.MRenameStyle, "", "Renaming style applied by Metarr (skip, fixes-only, underscores, spaces)")
	}
	if metaOpsFile != nil {
		cmd.Flags().StringVar(metaOpsFile, keys.MMetaOpsFile, "", "File containing meta operations (one per line)")
	}
	if filteredMetaOpsFile != nil {
		cmd.Flags().StringVar(filteredMetaOpsFile, keys.MFilteredMetaOpsFile, "", "File containing filtered meta operations (one per line, format: Filter&&Filter||Meta Op)")
	}
	if filenameOpsFile != nil {
		cmd.Flags().StringVar(filenameOpsFile, keys.MFilenameOpsFile, "", "File containing filename operations (one per line)")
	}
	if filteredFilenameOpsFile != nil {
		cmd.Flags().StringVar(filteredFilenameOpsFile, keys.MFilteredFilenameOpsFile, "", "File containing filtered filename operations (one per line, format: Filter&&Filter||Filename Op)")
	}

	// Arrays
	if filenameOps != nil {
		cmd.Flags().StringArrayVar(filenameOps, keys.MFilenameOps, nil, "Filename operation for Metarr (e.g. 'prefix:[CATEGORY] ' or 'date-tag:prefix:ymd'). Repeat the flag for each operation.")
	}
	if filteredFilenameOps != nil {
		cmd.Flags().StringArrayVar(filteredFilenameOps, keys.MFilteredFilenameOps, nil, "Filename operation to perform in Metarr when its filters all match (filter&&filter||operation&&operation). Repeat the flag for each operation.")
	}
	if metaOps != nil {
		cmd.Flags().StringArrayVar(metaOps, keys.MMetaOps, nil, "Meta operation to perform in Metarr. Repeat the flag for each operation.")
	}
	if filteredMetaOps != nil {
		cmd.Flags().StringArrayVar(filteredMetaOps, keys.MFilteredMetaOps, nil, "Meta operation to perform in Metarr when its filters all match (filter&&filter||operation&&operation). Repeat the flag for each operation.")
	}
}

// SetNotifyFlags sets flags related to notification URLs (e.g. Plex library URL to ping for an update).
func SetNotifyFlags(cmd *cobra.Command, notification *[]string) {
	if notification != nil {
		cmd.Flags().StringSliceVar(notification, keys.Notify, nil, "URL to notify when new videos are downloaded (e.g. a Plex library refresh URL)")
	}
}

// SetTranscodeFlags sets flags related to video transcoding.
func SetTranscodeFlags(cmd *cobra.Command, gpu, gpuNode, quality *string, videoCodec, audioCodec *[]string) {
	if gpu != nil {
		cmd.Flags().StringVar(gpu, keys.TranscodeGPU, "", "GPU for transcoding.")
	}

	if gpuNode != nil {
		cmd.Flags().StringVar(gpuNode, keys.TranscodeGPUNode, "", "GPU node path for transcoding (e.g. '/dev/dri/renderD128')")
	}

	if quality != nil {
		cmd.Flags().StringVar(quality, keys.TranscodeQuality, "", "Transcode quality from 0 to 51, lower is better (e.g. 23).")
	}

	if audioCodec != nil {
		cmd.Flags().StringSliceVar(audioCodec, keys.TranscodeAudioCodec, nil, "Codec for transcoding audio (e.g. 'aac' or 'copy').")
	}

	if videoCodec != nil {
		cmd.Flags().StringSliceVar(videoCodec, keys.TranscodeCodec, nil, "Codec for transcoding (h264/hevc).")
	}
}

// SetFileDirFlags sets the primary video and JSON directories.
func SetFileDirFlags(cmd *cobra.Command, configFile, jsonDir, videoDir *string) {
	if configFile != nil {
		cmd.Flags().StringVar(configFile, keys.ChannelConfigFile, "", "This is where the channel config file is stored (do not use templating). NOTE: Persists across runs! Channel will always load in this file if it's set!")
	}
	if videoDir != nil {
		cmd.Flags().StringVar(videoDir, keys.VideoDir, "", "This is where videos will be saved (some {{}} templating commands available)")
	}
	if jsonDir != nil {
		cmd.Flags().StringVar(jsonDir, keys.JSONDir, "", "This is where metadata files will be saved, defaulting to the video directory (some {{}} templating commands available)")
	}
}

// SetProgramRelatedFlags sets flags for the Tubarr instance.
func SetProgramRelatedFlags(cmd *cobra.Command, concurrency, crawlFreq *int, downloadArgs, downloadCmd, moveOpsFile *string, moveOps *[]string, pause *bool) {
	if concurrency != nil {
		cmd.Flags().IntVar(concurrency, keys.DownloadConcurrency, 0, "Maximum videos this channel downloads at once (0 = 1)")
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
		cmd.Flags().StringVar(moveOpsFile, keys.MoveOpsFile, "", "File containing move filter operations, one per line (format is 'metafield:filter_type:value||output directory', joining filters with '&&' to require all of them, e.g. 'title:contains:dogs||/home/dogs' or 'title:contains:dogs&&duration:more-than:3600||/home/long dogs')")
	}

	if moveOps != nil {
		cmd.Flags().StringSliceVar(moveOps, keys.MoveOps, nil, "Move to an output directory in Metarr based on metadata (format is 'metafield:filter_type:value||output directory', joining filters with '&&' to require all of them, e.g. 'title:contains:dogs||/home/dogs' or 'title:contains:dogs&&duration:more-than:3600||/home/long dogs')")
	}

	if pause != nil {
		cmd.Flags().BoolVar(pause, keys.Pause, false, "Pause/unpause this channel")
	}
}

// SetCustomYDLPArgFlags sets flags for custom additional YTDLP download arguments.
func SetCustomYDLPArgFlags(cmd *cobra.Command, extraVideoArgs, extraMetaArgs, extraCrawlArgs *string) {
	if extraVideoArgs != nil {
		cmd.Flags().StringVar(extraVideoArgs, keys.ExtraYTDLPVideoArgs, "", "Additional commands to pass to yt-dlp when downloading videos")
	}
	if extraMetaArgs != nil {
		cmd.Flags().StringVar(extraMetaArgs, keys.ExtraYTDLPMetaArgs, "", "Additional commands to pass to yt-dlp when downloading metadata")
	}
	if extraCrawlArgs != nil {
		cmd.Flags().StringVar(extraCrawlArgs, keys.ExtraYTDLPCrawlArgs, "", "Additional commands to pass to yt-dlp when crawling for videos")
	}
}
