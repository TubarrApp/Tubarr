package cfgflags

import (
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"

	"github.com/spf13/cobra"
)

// SetMetarrFlags sets flags for interaction with the Metarr software.
func SetMetarrFlags(cmd *cobra.Command, maxCPUPtr *float64, metarrConcurrencyPtr *int, extPtr, filenameDateTagPtr, minFreeMemPtr, outDirPtr, renameStylePtr *string, fileSfxReplacePtr, metaOpsPtr *[]string) models.MetarrArgs {
	var (
		maxCPU                                                float64
		metarrConcurrency                                     int
		ext, filenameDateTag, minFreeMem, outDir, renameStyle string
		fileSfxReplace, metaOps                               []string
	)

	// Numbers
	if maxCPUPtr != nil {
		cmd.Flags().Float64Var(maxCPUPtr, keys.MMaxCPU, 0, "Max CPU usage for Metarr")
	}
	if metarrConcurrencyPtr != nil {
		cmd.Flags().IntVar(metarrConcurrencyPtr, keys.MConcurrency, 0, "Max concurrent processes for Metarr")
	}

	// String
	if extPtr != nil {
		cmd.Flags().StringVar(extPtr, keys.MExt, "", "Output filetype for videos passed into Metarr")
	}
	if filenameDateTagPtr != nil {
		cmd.Flags().StringVar(filenameDateTagPtr, keys.MInputFileDatePfx, "", "Prefix a filename with a particular date tag (ymd format where Y means yyyy and y means yy)")
	}
	if minFreeMemPtr != nil {
		cmd.Flags().StringVar(minFreeMemPtr, keys.MMinFreeMem, "", "Min free mem for Metarr process")
	}
	if outDirPtr != nil {
		cmd.Flags().StringVar(outDirPtr, keys.MOutputDir, "", "Metarr will move files to this location on completion (some {{}} templating commands available)")
	}
	if renameStylePtr != nil {
		cmd.Flags().StringVar(renameStylePtr, keys.MRenameStyle, "", "Renaming style applied by Metarr (skip, fixes-only, underscores, spaces)")
	}

	// Arrays
	if fileSfxReplacePtr != nil {
		cmd.Flags().StringSliceVar(fileSfxReplacePtr, keys.MFilenameReplaceSuffix, nil, "Replace a filename suffix element in Metarr")
	}
	if metaOpsPtr != nil {
		cmd.Flags().StringSliceVar(metaOpsPtr, keys.MMetaOps, nil, "Meta operations to perform in Metarr")
	}

	return models.MetarrArgs{
		Ext:                ext,
		FilenameReplaceSfx: fileSfxReplace,
		RenameStyle:        renameStyle,
		FileDatePfx:        filenameDateTag,
		MetaOps:            metaOps,
		OutputDir:          outDir,
		Concurrency:        metarrConcurrency,
		MaxCPU:             maxCPU,
		MinFreeMem:         minFreeMem,
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
