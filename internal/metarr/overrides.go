package metarr

import (
	"slices"
	"strings"
	"tubarr/internal/domain/keys"
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedparsing"
)

// ChannelURLOverrideLines returns the directories and ops a channel URL's videos use, ops without their channel URL
// prefix, as the defaults for a video's Download As overrides. Filtered ops are listed with their filters, as matching
// needs metadata, and the output directory leaves out move ops for the same reason.
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

	// Channel URL ops, else the global ones, as Metarr is given them.
	metaOps := slices.Concat(fileMetaOps, filterConflictingMetaOps(fileMetaOps, cu.ChanURLMetarrArgs.MetaOps))
	metaOpLines := getDedupedMetaOpStrings(filterMetaOpsByChannel(metaOps, cu.URL))
	if len(metaOpLines) == 0 {
		metaOpLines = globalOpLines(keys.MMetaOps, cu.URL)
	}
	filenameOps := slices.Concat(fileFilenameOps, filterConflictingFilenameOps(fileFilenameOps, cu.ChanURLMetarrArgs.FilenameOps))
	filenameOpLines := getDedupedFilenameOpStrings(filterFilenameOpsByChannel(filenameOps, cu.URL))
	if len(filenameOpLines) == 0 {
		filenameOpLines = globalOpLines(keys.MFilenameOps, cu.URL)
	}

	outputDir := cu.ChanURLMetarrArgs.OutputDir
	if abstractions.IsSet(keys.MoveOnComplete) {
		outputDir = abstractions.GetString(keys.MoveOnComplete)
	}

	return models.OverrideLines{
		VideoDir:            cu.ChanURLSettings.VideoDir,
		JSONDir:             cu.ChanURLSettings.JSONDir,
		OutputDir:           outputDir,
		MetaOps:             strings.Join(metaOpLines, "\n"),
		FilteredMetaOps:     strings.Join(filteredMetaOpLines(slices.Concat(cu.ChanURLMetarrArgs.FilteredMetaOps, fileFilteredMetaOps), cu.URL), "\n"),
		FilenameOps:         strings.Join(filenameOpLines, "\n"),
		FilteredFilenameOps: strings.Join(filteredFilenameOpLines(slices.Concat(cu.ChanURLMetarrArgs.FilteredFilenameOps, fileFilteredFilenameOps), cu.URL), "\n"),
	}, nil
}

// VideoOverrideLines returns a video's stored Download As overrides as lines, to fill the dialog with them again.
func VideoOverrideLines(o *models.VideoOverrides, chanURL string) models.OverrideLines {
	return models.OverrideLines{
		Filename:            o.Filename,
		VideoDir:            o.VideoDir,
		JSONDir:             o.JSONDir,
		OutputDir:           o.OutputDir,
		MetaOps:             strings.Join(getDedupedMetaOpStrings(o.MetaOps), "\n"),
		FilteredMetaOps:     strings.Join(filteredMetaOpLines(o.FilteredMetaOps, chanURL), "\n"),
		FilenameOps:         strings.Join(getDedupedFilenameOpStrings(o.FilenameOps), "\n"),
		FilteredFilenameOps: strings.Join(filteredFilenameOpLines(o.FilteredFilenameOps, chanURL), "\n"),
	}
}

// globalOpLines returns the global ops under key which apply to chanURL, without their channel URL prefix.
func globalOpLines(key, chanURL string) []string {
	if !abstractions.IsSet(key) {
		return nil
	}
	var lines []string
	for _, l := range abstractions.GetStringSlice(key) {
		if u, rest, err := sharedparsing.SplitOpURL(l); err == nil && sharedparsing.AppliesToChannel(u, chanURL) {
			lines = append(lines, rest)
		}
	}
	return lines
}

// filteredMetaOpLines returns the filtered meta ops which apply to chanURL, without their channel URL prefix.
func filteredMetaOpLines(ops []models.FilteredMetaOps, chanURL string) []string {
	lines := make([]string, 0, len(ops))
	for _, fmo := range ops {
		for i := range min(len(fmo.Filters), len(fmo.MetaOps)) {
			f, op := fmo.Filters[i], fmo.MetaOps[i]
			if !sharedparsing.AppliesToChannel(f.ChannelURL, chanURL) || !sharedparsing.AppliesToChannel(op.ChannelURL, chanURL) {
				continue
			}
			f.ChannelURL = ""
			lines = append(lines, models.FiltersToString(f, false)+"|"+sharedparsing.FormatMetaOp(op, false))
		}
	}
	return lines
}

// filteredFilenameOpLines returns the filtered filename ops which apply to chanURL, without their channel URL prefix.
func filteredFilenameOpLines(ops []models.FilteredFilenameOps, chanURL string) []string {
	lines := make([]string, 0, len(ops))
	for _, ffo := range ops {
		for i := range min(len(ffo.Filters), len(ffo.FilenameOps)) {
			f, op := ffo.Filters[i], ffo.FilenameOps[i]
			if !sharedparsing.AppliesToChannel(f.ChannelURL, chanURL) || !sharedparsing.AppliesToChannel(op.ChannelURL, chanURL) {
				continue
			}
			f.ChannelURL = ""
			lines = append(lines, models.FiltersToString(f, false)+"|"+sharedparsing.FormatFilenameOp(op, false))
		}
	}
	return lines
}
