package metarr

import (
	"fmt"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
)

// conflictingFilenameOp contains conflicting filename operations.
var conflictingFilenameOp = map[string]struct{}{
	sharedconsts.OpDateTag:       {},
	sharedconsts.OpDeleteDateTag: {},
}

// loadAndMergeFilenameOps loads and merges filename ops: file ops override DB ops, then apply filtering
func loadAndMergeFilenameOps(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// Load file ops (highest priority). An unusable file holds back Metarr for the whole batch, see app.metarrSkip.
	fileFilenameOps, err := LoadFilenameOpsFromFile(cu, dirParser)
	if err != nil {
		logger.Pl.E("Filename ops file for %q can't be used: %v", cu.URL, err)
	}

	// Ops from file override DB ops with same key
	nonConflictingDBOps := filterConflictingFilenameOps(fileFilenameOps, cu.ChanURLMetarrArgs.FilenameOps)

	// Combine
	fileAndDBOps := append(fileFilenameOps, nonConflictingDBOps...)

	// Merge with filtered filename ops (per-video, stored in Video struct)
	mergedOps := applyFilteredFilenameOps(fileAndDBOps, v.FilteredFilenameOps, v.URL, c.Name)

	// Omit operations with the wrong channel URL
	channelMatchedOps := filterFilenameOpsByChannel(mergedOps, cu.URL)

	// Convert to slice and deduplicate
	return getDedupedFilenameOpStrings(channelMatchedOps)
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

	// Parse filename operations from strings
	parsedOps, err := parsing.ParseFilenameOps(ops)
	if err != nil {
		return nil, fmt.Errorf("failed to parse filename ops in %q: %w", filenameOpsFile, err)
	}

	// Validate the parsed operations
	if err := validation.ValidateFilenameOps(parsedOps); err != nil {
		return nil, fmt.Errorf("invalid filename ops in %q: %w", filenameOpsFile, err)
	}

	logger.Pl.I("Loaded %d filename ops from file", len(parsedOps))
	return parsedOps, nil
}

// filterConflictingFilenameOps removes DB ops that are fully identical to file ops
func filterConflictingFilenameOps(fileOps, dbOps []models.FilenameOps) []models.FilenameOps {
	fileOpKeys := make(map[string]bool)

	// Build conflict keys
	for _, op := range fileOps {
		if _, conflicts := conflictingFilenameOp[op.OpType]; conflicts {
			fileOpKeys[op.OpType] = true
		}
	}

	// Add new operations
	result := make([]models.FilenameOps, 0, len(dbOps))
	for _, op := range dbOps {
		_, conflicts := conflictingFilenameOp[op.OpType]
		if !fileOpKeys[op.OpType] || !conflicts {
			result = append(result, op)
		} else {
			logger.Pl.D(2, "File filename op overrides DB op: %s", sharedparsing.FormatFilenameOp(op, false))
		}
	}
	return result
}

// applyFilteredFilenameOps applies filtering rules to filename ops
func applyFilteredFilenameOps(ops []models.FilenameOps, filteredOps []models.FilteredFilenameOps, videoURL, channelName string) []models.FilenameOps {
	matchedFilteredOps := extractMatchedFilteredFilenameOps(filteredOps)
	matchedFilteredKeys := buildFilenameOpKeySet(matchedFilteredOps, true)

	// Keep only ops that aren't being replaced by MATCHED filtered ops
	result := make([]models.FilenameOps, 0, len(ops))
	for _, op := range ops {
		key := sharedparsing.FormatFilenameOp(op, false)
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
		_, conflicts := conflictingFilenameOp[op.OpType]
		if includeNonConflicting || !conflicts {
			filenameOpsKeys[sharedparsing.FormatFilenameOp(op, false)] = true
		}
	}
	return filenameOpsKeys
}

// filterFilenameOpsByChannel filters out meta operations not matching the current channel.
func filterFilenameOpsByChannel(ops []models.FilenameOps, cURL string) []models.FilenameOps {
	valid := make([]models.FilenameOps, 0, len(ops))
	for _, op := range ops {
		if sharedparsing.AppliesToChannel(op.ChannelURL, cURL) {
			valid = append(valid, op)
		}
	}
	return valid
}

// getDedupedFilenameOpStrings converts ops to deduplicated string slice for specific URL.
func getDedupedFilenameOpStrings(ops []models.FilenameOps) []string {
	seen := make(map[string]bool, len(ops))
	result := make([]string, 0, len(ops))

	// Make deduplicated filename op slice
	for _, op := range ops {
		opStr := sharedparsing.FormatFilenameOp(op, false)
		if !seen[opStr] {
			seen[opStr] = true
			result = append(result, opStr)
		}
	}
	return result
}
