package metarr

import (
	"errors"
	"fmt"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// loadAndMergeMetaOps returns a video's meta ops: its channel URL's from every level, then its matched filtered ones.
func loadAndMergeMetaOps(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) []string {
	// An unusable file holds back Metarr for the whole batch, see SkipReason.
	ops, err := channelURLMetaOps(cu, dirParser)
	if err != nil {
		cu.Log().E("Meta ops file for %q can't be used: %v", cu.URL, err)
	}
	return getDedupedMetaOpStrings(applyFilteredMetaOps(ops, v.FilteredMetaOps, v.URL, v.ChannelName))
}

// channelURLMetaOps returns a channel URL's meta ops from every level, with each level's ops file. Returns the errors of
// files it can't use, whose ops are left out.
func channelURLMetaOps(cu *models.ChannelURL, dp *parsing.DirectoryParser) ([]models.MetaOps, error) {
	fileOps := make([][]models.MetaOps, len(cu.Levels))
	var errs []error
	for i, l := range cu.Levels {
		if l.MetarrArgs == nil {
			continue
		}
		var err error
		fileOps[i], err = LoadMetaOpsFromFile(l.MetarrArgs.MetaOpsFile, dp)
		errs = append(errs, err)
	}
	return models.StackMetaOps(cu.Levels, fileOps), errors.Join(errs...)
}

// overrideMetaOps returns the video's Download As meta ops, with its matched filtered meta ops.
func overrideMetaOps(v *models.Video) []string {
	return getDedupedMetaOpStrings(applyFilteredMetaOps(v.Overrides.MetaOps, v.FilteredMetaOps, v.URL, v.ChannelName))
}

// LoadMetaOpsFromFile loads in meta operations from the file at path, if set.
//
// File ops take precedence and will replace any matching DB ops. Returns an error if the file is set but can't be read
// or parsed.
func LoadMetaOpsFromFile(path string, dp *parsing.DirectoryParser) ([]models.MetaOps, error) {
	if path == "" {
		return nil, nil
	}

	metaOpsFile, err := dp.ParseDirectory(path, "meta ops file")
	if err != nil {
		return nil, fmt.Errorf("failed to parse meta ops file path %q: %w", path, err)
	}

	dp.C.Log().D(1, "Adding meta ops from file %q...", metaOpsFile)
	ops, err := file.ReadOpLines(metaOpsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read meta ops file: %w", err)
	}

	// Parse meta operations from strings
	parsedOps, err := parsing.ParseMetaOps(ops)
	if err != nil {
		return nil, fmt.Errorf("invalid meta ops in %q: %w", metaOpsFile, err)
	}

	dp.C.Log().D(1, "Loaded %d meta ops from file", len(parsedOps))
	return parsedOps, nil
}

// applyFilteredMetaOps adds the matched filtered meta ops to ops, each beating any of ops it conflicts with.
func applyFilteredMetaOps(ops []models.MetaOps, filteredOps []models.FilteredMetaOps, videoURL, channelName string) []models.MetaOps {
	matchedFilteredOps := extractMatchedFilteredMetaOps(filteredOps)
	result := models.FilterConflictingMetaOps(matchedFilteredOps, ops)
	for _, op := range result {
		logger.Pl.Channel(channelName).D(2, "Added operation %q for video URL %q (Channel: %q)", sharedparsing.FormatMetaOp(op), videoURL, channelName)
	}
	return append(result, matchedFilteredOps...)
}

// extractMatchedFilteredMetaOps gets only the filtered ops where filters matched
func extractMatchedFilteredMetaOps(filteredOps []models.FilteredMetaOps) []models.MetaOps {
	result := make([]models.MetaOps, 0)
	for _, fmo := range filteredOps {
		if fmo.FiltersMatched {
			result = append(result, fmo.MetaOps...)
		}
	}
	return result
}

// getDedupedMetaOpStrings converts ops to deduplicated string slice.
func getDedupedMetaOpStrings(ops []models.MetaOps) []string {
	seen := make(map[string]bool, len(ops))
	result := make([]string, 0, len(ops))

	for _, op := range ops {
		opStr := sharedparsing.FormatMetaOp(op)
		if !seen[opStr] {
			seen[opStr] = true
			result = append(result, opStr)
		}
	}
	return result
}
