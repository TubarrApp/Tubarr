package metarr

import (
	"fmt"
	"slices"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
)

// loadAndMergeFilenameOps loads and merges filename ops: file ops override DB ops, then apply filtering
func loadAndMergeFilenameOps(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// Load file ops (highest priority). An unusable file holds back Metarr for the whole batch, see app.metarrSkip.
	fileFilenameOps, err := LoadFilenameOpsFromFile(cu, dirParser)
	if err != nil {
		logger.Pl.E("Filename ops file for %q can't be used: %v", cu.URL, err)
	}

	// Ops from file override DB ops with same key
	nonConflictingDBOps := models.FilterConflictingFilenameOps(fileFilenameOps, cu.ChanURLMetarrArgs.FilenameOps)

	// Combine
	fileAndDBOps := slices.Concat(fileFilenameOps, nonConflictingDBOps)

	// Merge with filtered filename ops (per-video, stored in Video struct)
	mergedOps := applyFilteredFilenameOps(fileAndDBOps, v.FilteredFilenameOps, v.URL, c.Name)

	// Convert to slice and deduplicate
	return getDedupedFilenameOpStrings(mergedOps)
}

// overrideFilenameOps returns the video's Download As filename ops, with its matched filtered filename ops and any
// chosen filename. Metarr keeps only one set op, so a chosen filename replaces the others.
func overrideFilenameOps(v *models.Video, c *models.Channel) []string {
	ops := applyFilteredFilenameOps(v.Overrides.FilenameOps, v.FilteredFilenameOps, v.URL, c.Name)
	if v.Overrides.Filename != "" {
		ops = slices.DeleteFunc(ops, func(op models.FilenameOps) bool { return op.OpType == sharedconsts.OpSet })
		ops = append(ops, models.FilenameOps{OpType: sharedconsts.OpSet, OpValue: v.Overrides.Filename})
	}
	return getDedupedFilenameOpStrings(ops)
}

// LoadFilenameOpsFromFile loads in file operations from the given file.
//
// File ops take precedence and will replace any matching DB ops. Returns an error if the file is set but can't be read
// or parsed.
func LoadFilenameOpsFromFile(cu *models.ChannelURL, dp *parsing.DirectoryParser) ([]models.FilenameOps, error) {
	if cu.ChanURLMetarrArgs.FilenameOpsFile == "" {
		return nil, nil
	}

	filenameOpsFile, err := dp.ParseDirectory(cu.ChanURLMetarrArgs.FilenameOpsFile, "filename ops")
	if err != nil {
		return nil, fmt.Errorf("failed to parse filename ops file path %q: %w", cu.ChanURLMetarrArgs.FilenameOpsFile, err)
	}

	logger.Pl.I("Adding filename ops from file %q...", filenameOpsFile)
	ops, err := file.ReadOpLines(filenameOpsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read filename ops file: %w", err)
	}

	// Parse and validate filename operations from strings
	parsedOps, err := parsing.ParseFilenameOps(ops)
	if err != nil {
		return nil, fmt.Errorf("invalid filename ops in %q: %w", filenameOpsFile, err)
	}

	logger.Pl.I("Loaded %d filename ops from file", len(parsedOps))
	return parsedOps, nil
}

// applyFilteredFilenameOps applies filtering rules to filename ops
func applyFilteredFilenameOps(ops []models.FilenameOps, filteredOps []models.FilteredFilenameOps, videoURL, channelName string) []models.FilenameOps {
	matchedFilteredOps := extractMatchedFilteredFilenameOps(filteredOps)
	matchedFilteredKeys := buildFilenameOpKeySet(matchedFilteredOps, true)

	// Keep only ops that aren't being replaced by MATCHED filtered ops
	result := make([]models.FilenameOps, 0, len(ops))
	for _, op := range ops {
		key := sharedparsing.FormatFilenameOp(op)
		if !matchedFilteredKeys[key] {
			logger.Pl.D(2, "Added filename operation %q for video URL %q (Channel: %q)", op, videoURL, channelName)
			result = append(result, op)
		}
	}

	// Add the matched filtered ops
	result = append(result, matchedFilteredOps...)

	return result
}

// extractMatchedFilteredFilenameOps gets only the filtered ops where filters matched
func extractMatchedFilteredFilenameOps(filteredOps []models.FilteredFilenameOps) []models.FilenameOps {
	result := make([]models.FilenameOps, 0)
	for _, ffo := range filteredOps {
		if ffo.FiltersMatched {
			result = append(result, ffo.FilenameOps...)
		}
	}
	return result
}

// buildFilenameOpKeySet creates a set of keys from filename ops
func buildFilenameOpKeySet(ops []models.FilenameOps, includeNonConflicting bool) map[string]bool {
	filenameOpsKeys := make(map[string]bool, len(ops))
	for _, op := range ops {
		_, conflicts := models.ConflictingFilenameOps[op.OpType]
		if includeNonConflicting || !conflicts {
			filenameOpsKeys[sharedparsing.FormatFilenameOp(op)] = true
		}
	}
	return filenameOpsKeys
}

// getDedupedFilenameOpStrings converts ops to deduplicated string slice.
func getDedupedFilenameOpStrings(ops []models.FilenameOps) []string {
	seen := make(map[string]bool, len(ops))
	result := make([]string, 0, len(ops))

	// Make deduplicated filename op slice
	for _, op := range ops {
		opStr := sharedparsing.FormatFilenameOp(op)
		if !seen[opStr] {
			seen[opStr] = true
			result = append(result, opStr)
		}
	}
	return result
}
