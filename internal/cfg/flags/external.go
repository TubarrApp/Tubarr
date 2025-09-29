package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// SetMetarrFlags sets flags for interaction with the Metarr software.
func SetMetarrFlags(cmd *cobra.Command, maxCPU *float64, metarrConcurrency *int, ext, extraFFmpegargs, filenameDateTag, minFreeMem, outDir, renameStyle *string, urlOutDirs, fileSfxReplace, metaOps *[]string) {
	// Numbers
	if maxCPU != nil {
		cmd.Flags().Float64Var(maxCPU, keys.MMaxCPU, 0, "Max CPU usage for Metarr")
	}
	if metarrConcurrency != nil {
		cmd.Flags().IntVar(metarrConcurrency, keys.MConcurrency, 0, "Max concurrent processes for Metarr")
	}

	// String
	if ext != nil {
		cmd.Flags().StringVar(ext, keys.MExt, "", "Output filetype for videos passed into Metarr")
	}
	if extraFFmpegargs != nil {
		cmd.Flags().StringVar(extraFFmpegargs, keys.MExtraFFmpegArgs, "", "Arguments to add on to FFmpeg commands")
	}
	if filenameDateTag != nil {
		cmd.Flags().StringVar(filenameDateTag, keys.MFilenameDateTag, "", "Prefix a filename with a particular date tag (ymd format where Y means yyyy and y means yy)")
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

	// Arrays
	if fileSfxReplace != nil {
		cmd.Flags().StringSliceVar(fileSfxReplace, keys.MFilenameReplaceSuffix, nil, "Replace a filename suffix element in Metarr")
	}
	if metaOps != nil {
		cmd.Flags().StringSliceVar(metaOps, keys.MMetaOps, nil, "Meta operations to perform in Metarr")
	}
	if urlOutDirs != nil {
		cmd.Flags().StringSliceVar(urlOutDirs, keys.MURLOutputDirs, nil, "Metarr will move a channel URL's files to this location on completion (some {{}} templating commands available)")
	}
}

// SetNotifyFlags sets flags related to notification URLs (e.g. Plex library URL to ping for an update).
func SetNotifyFlags(cmd *cobra.Command, notification *[]string) {
	if notification != nil {
		cmd.Flags().StringSliceVar(notification, keys.NotifyPair, nil, "URL to notify on completion (format is 'URL|Name', 'Name' can be empty)")
	}
}

// SetTranscodeFlags sets flags related to video transcoding.
func SetTranscodeFlags(cmd *cobra.Command, gpu, gpuDir, videoFilter, codec, audioCodec, quality *string) {
	if gpu != nil {
		cmd.Flags().StringVar(gpu, keys.TranscodeGPU, "", "GPU for transcoding.")
	}

	if gpuDir != nil {
		cmd.Flags().StringVar(gpuDir, keys.TranscodeGPUDir, "", "Directory of the GPU for transcoding (e.g. '/dev/dri/renderD128')")
	}

	if videoFilter != nil {
		cmd.Flags().StringVar(videoFilter, keys.TranscodeVideoFilter, "", "Video filter")
	}

	if audioCodec != nil {
		cmd.Flags().StringVar(audioCodec, keys.TranscodeAudioCodec, "", "Codec for transcoding audio (e.g. 'aac' or 'copy').")
	}

	if codec != nil {
		cmd.Flags().StringVar(codec, keys.TranscodeCodec, "", "Codec for transcoding (h264/hevc).")
	}

	if quality != nil {
		cmd.Flags().StringVar(quality, keys.TranscodeQuality, "", "Transcode quality profile from p1 (worst) to p7 (best).")
	}
}
