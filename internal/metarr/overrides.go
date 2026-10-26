package metarr

import (
	"fmt"
	"slices"
	"strings"
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// ChannelURLOverrideLines returns the directories and ops a channel URL's videos use, as the defaults for a video's
// Download As overrides. Filtered ops are listed with their filters, as matching needs metadata, and the output
// directory leaves out move ops for the same reason.
func ChannelURLOverrideLines(cu *models.ChannelURL, dirParser *parsing.DirectoryParser) (lines models.OverrideLines, err error) {
	fileMetaOps, err := LoadMetaOpsFromFile(cu, dirParser)
	if err != nil {
		return lines, err
	}
	fileFilenameOps, err := LoadFilenameOpsFromFile(cu, dirParser)
	if err != nil {
		return lines, err
	}
	fileFilteredMetaOps, err := metadata.LoadFilteredMetaOpsFromFile(cu, dirParser)
	if err != nil {
		return lines, err
	}
	fileFilteredFilenameOps, err := metadata.LoadFilteredFilenameOpsFromFile(cu, dirParser)
	if err != nil {
		return lines, err
	}

	// Channel URL ops, as Metarr is given them.
	metaOps := slices.Concat(fileMetaOps, models.FilterConflictingMetaOps(fileMetaOps, cu.ChanURLMetarrArgs.MetaOps))
	filenameOps := slices.Concat(fileFilenameOps, models.FilterConflictingFilenameOps(fileFilenameOps, cu.ChanURLMetarrArgs.FilenameOps))

	return models.OverrideLines{
		VideoDir:            cu.ChanURLSettings.VideoDir,
		JSONDir:             cu.ChanURLSettings.JSONDir,
		OutputDir:           cu.ChanURLMetarrArgs.OutputDir,
		MetaOps:             strings.Join(getDedupedMetaOpStrings(metaOps), "\n"),
		FilteredMetaOps:     strings.Join(filteredMetaOpLines(slices.Concat(cu.ChanURLMetarrArgs.FilteredMetaOps, fileFilteredMetaOps)), "\n"),
		FilenameOps:         strings.Join(getDedupedFilenameOpStrings(filenameOps), "\n"),
		FilteredFilenameOps: strings.Join(filteredFilenameOpLines(slices.Concat(cu.ChanURLMetarrArgs.FilteredFilenameOps, fileFilteredFilenameOps)), "\n"),
	}, nil
}

// FileOverrideLines returns the ops in the file at path for the Download As dialog's box (e.g. "meta_ops").
func FileOverrideLines(box, path string, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) ([]string, error) {
	args := &models.MetarrArgs{}
	fileCU := &models.ChannelURL{URL: cu.URL, ChanURLMetarrArgs: args} // The loaders read the file path from a channel URL.
	switch box {
	case "meta_ops":
		args.MetaOpsFile = path
		ops, err := LoadMetaOpsFromFile(fileCU, dirParser)
		return getDedupedMetaOpStrings(ops), err
	case "filtered_meta_ops":
		args.FilteredMetaOpsFile = path
		ops, err := metadata.LoadFilteredMetaOpsFromFile(fileCU, dirParser)
		return filteredMetaOpLines(ops), err
	case "filename_ops":
		args.FilenameOpsFile = path
		ops, err := LoadFilenameOpsFromFile(fileCU, dirParser)
		return getDedupedFilenameOpStrings(ops), err
	case "filtered_filename_ops":
		args.FilteredFilenameOpsFile = path
		ops, err := metadata.LoadFilteredFilenameOpsFromFile(fileCU, dirParser)
		return filteredFilenameOpLines(ops), err
	}
	return nil, fmt.Errorf("unknown ops box %q", box)
}

// VideoOverrideLines returns a video's stored Download As overrides as lines, to fill the dialog with them again.
func VideoOverrideLines(o *models.VideoOverrides) models.OverrideLines {
	return models.OverrideLines{
		Filename:                  o.Filename,
		VideoDir:                  o.VideoDir,
		JSONDir:                   o.JSONDir,
		OutputDir:                 o.OutputDir,
		MetaOps:                   strings.Join(getDedupedMetaOpStrings(o.MetaOps), "\n"),
		FilteredMetaOps:           strings.Join(filteredMetaOpLines(o.FilteredMetaOps), "\n"),
		FilenameOps:               strings.Join(getDedupedFilenameOpStrings(o.FilenameOps), "\n"),
		FilteredFilenameOps:       strings.Join(filteredFilenameOpLines(o.FilteredFilenameOps), "\n"),
		Thumbnail:                 o.Thumbnail,
		StripThumbnail:            o.StripThumbnail,
		YtdlpOutputExt:            o.YtdlpOutputExt,
		MaxFilesize:               o.MaxFilesize,
		ExtraYTDLPVideoArgs:       o.ExtraYTDLPVideoArgs,
		YTDLPPreferredVideoCodecs: strings.Join(o.YTDLPPreferredVideoCodecs, "\n"),
		YTDLPPreferredAudioCodecs: strings.Join(o.YTDLPPreferredAudioCodecs, "\n"),
	}
}

// filteredMetaOpLines returns the filtered meta ops as lines, each filter with its op.
func filteredMetaOpLines(ops []models.FilteredMetaOps) []string {
	lines := make([]string, 0, len(ops))
	for _, fmo := range ops {
		for i := range min(len(fmo.Filters), len(fmo.MetaOps)) {
			lines = append(lines, sharedparsing.FormatFilter(fmo.Filters[i], false)+sharedparsing.FilterSeparator+sharedparsing.FormatMetaOp(fmo.MetaOps[i]))
		}
	}
	return lines
}

// filteredFilenameOpLines returns the filtered filename ops as lines, each filter with its op.
func filteredFilenameOpLines(ops []models.FilteredFilenameOps) []string {
	lines := make([]string, 0, len(ops))
	for _, ffo := range ops {
		for i := range min(len(ffo.Filters), len(ffo.FilenameOps)) {
			lines = append(lines, sharedparsing.FormatFilter(ffo.Filters[i], false)+sharedparsing.FilterSeparator+sharedparsing.FormatFilenameOp(ffo.FilenameOps[i]))
		}
	}
	return lines
}
