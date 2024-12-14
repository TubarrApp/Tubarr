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
		cmd.Flags().Float64Var(maxCPUPtr, keys.MaxCPU, 0, "Max CPU usage for Metarr")
	}
	if metarrConcurrencyPtr != nil {
		cmd.Flags().IntVar(metarrConcurrencyPtr, keys.MetarrConcurrency, 0, "Max concurrent processes for Metarr")
	}

	// String
	if extPtr != nil {
		cmd.Flags().StringVar(extPtr, keys.OutputFiletype, "", "Output filetype for videos passed into Metarr")
	}
	if filenameDateTagPtr != nil {
		cmd.Flags().StringVar(filenameDateTagPtr, keys.InputFileDatePfx, "", "Prefix a filename with a particular date tag (ymd format where Y means yyyy and y means yy)")
	}
	if minFreeMemPtr != nil {
		cmd.Flags().StringVar(minFreeMemPtr, keys.MinFreeMem, "", "Min free mem for Metarr process")
	}
	if outDirPtr != nil {
		cmd.Flags().StringVar(outDirPtr, keys.MetarrOutputDir, "", "Metarr will move files to this location on completion (some {{}} templating commands available)")
	}
	if renameStylePtr != nil {
		cmd.Flags().StringVar(renameStylePtr, keys.RenameStyle, "", "Renaming style applied by Metarr (skip, fixes-only, underscores, spaces)")
	}

	// Arrays
	if fileSfxReplacePtr != nil {
		cmd.Flags().StringSliceVar(fileSfxReplacePtr, keys.FilenameReplaceSuffix, nil, "Replace a filename suffix element in Metarr")
	}
	if metaOpsPtr != nil {
		cmd.Flags().StringSliceVar(metaOpsPtr, keys.MetaOps, nil, "Meta operations to perform in Metarr")
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
