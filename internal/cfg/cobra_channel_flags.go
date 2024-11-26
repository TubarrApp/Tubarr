package cfg

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

func setPrimaryChannelFlags(cmd *cobra.Command, name, url *string, id *int) {
	if id != nil {
		cmd.Flags().IntVarP(id, keys.ID, "i", 0, "Channel ID in the DB")
	}

	if name != nil {
		cmd.Flags().StringVarP(name, keys.Name, "n", "", "Channel name")
	}
	if url != nil {
		cmd.Flags().StringVarP(url, keys.URL, "u", "", "Channel URL")
	}
}

func setFileDirFlags(cmd *cobra.Command, jDir, vDir *string) {
	if vDir != nil {
		cmd.Flags().StringVar(vDir, keys.VideoDir, "", "This is where videos for this channel will be saved (some {{}} templating commands available)")
	}
	if jDir != nil {
		cmd.Flags().StringVar(jDir, keys.JSONDir, "", "This is where JSON files for this channel will be saved (some {{}} templating commands available)")
	}
}

func setProgramRelatedFlags(cmd *cobra.Command, concurrency, crawlFreq *int, downloadArgs, downloadCmd *string) {
	if concurrency != nil {
		cmd.Flags().IntVarP(concurrency, keys.Concurrency, "l", 0, "Maximum concurrent videos to download/process for this channel")
	}
	if crawlFreq != nil {
		cmd.Flags().IntVar(crawlFreq, "crawl-freq", 30, "New crawl frequency in minutes")
	}
	if downloadCmd != nil {
		cmd.Flags().StringVar(downloadCmd, "downloader", "", "External downloader command")
	}
	if downloadArgs != nil {
		cmd.Flags().StringVar(downloadArgs, "downloader-args", "", "External downloader arguments")
	}
}

func setDownloadFlags(cmd *cobra.Command, retries *int, cookieSource, maxFilesize *string, dlFilters *[]string) {
	if retries != nil {
		cmd.Flags().IntVar(retries, keys.DLRetries, 0, "Number of retries to attempt a download before failure")
	}

	if cookieSource != nil {
		cmd.Flags().StringVar(cookieSource, keys.CookieSource, "", "Cookie source to use for downloading videos (e.g. firefox)")
	}
	if maxFilesize != nil {
		cmd.Flags().StringVar(maxFilesize, keys.MaxFilesize, "", "Enter your desired yt-dlp max filesize parameter")
	}

	if dlFilters != nil {
		cmd.Flags().StringSliceVar(dlFilters, keys.FilterOpsInput, nil, "Filter in or out videos with certain metafields")
	}
}

func setMetarrFlags(cmd *cobra.Command, maxCPU *float64, metarrConcurrency *int, ext, filenameDateTag, minFreeMem, outDir, renameStyle *string, fileSfxReplace, metaOps *[]string) {
	// Numbers
	if maxCPU != nil {
		cmd.Flags().Float64Var(maxCPU, keys.MaxCPU, 0, "Max CPU usage for Metarr")
	}
	if metarrConcurrency != nil {
		cmd.Flags().IntVar(metarrConcurrency, keys.MetarrConcurrency, 0, "Max concurrent processes for Metarr")
	}

	// String
	if ext != nil {
		cmd.Flags().StringVar(ext, keys.OutputFiletype, "", "Output filetype for videos passed into Metarr")
	}
	if filenameDateTag != nil {
		cmd.Flags().StringVar(filenameDateTag, keys.InputFileDatePfx, "", "Prefix a filename with a particular date tag (ymd format where Y means yyyy and y means yy)")
	}
	if minFreeMem != nil {
		cmd.Flags().StringVar(minFreeMem, keys.MinFreeMem, "", "Min free mem for Metarr process")
	}
	if outDir != nil {
		cmd.Flags().StringVar(outDir, keys.MetarrOutputDir, "", "Metarr will move files to this location on completion (some {{}} templating commands available)")
	}
	if renameStyle != nil {
		cmd.Flags().StringVar(renameStyle, keys.RenameStyle, "", "Renaming style applied by Metarr (skip, fixes-only, underscores, spaces)")
	}

	// Arrays
	if fileSfxReplace != nil {
		cmd.Flags().StringSliceVar(fileSfxReplace, keys.InputFilenameReplaceSfx, nil, "Replace a filename suffix element in Metarr")
	}
	if metaOps != nil {
		cmd.Flags().StringSliceVar(metaOps, keys.MetaOps, nil, "Meta operations to perform in Metarr")
	}

}
