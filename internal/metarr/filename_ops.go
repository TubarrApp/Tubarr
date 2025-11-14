package metarr

import (
	"tubarr/internal/domain/keys"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"
)

var isConflictingFilenameOp = map[string]bool{
	"date-tag": true, "delete-date-tag": true,
}

// loadAndMergeFilenameOps loads and merges filename ops: file ops override DB ops, then apply filtering
func loadAndMergeFilenameOps(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// Load file ops (highest priority)
	fileFilenameOps := loadFilenameOpsFromFile(v, cu, dirParser)

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

// loadFilenameOpsFromFile loads in file operations from the given file.
//
// File ops take precedence and will replace any matching DB ops.
func loadFilenameOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.FilenameOps {
	if cu.ChanURLMetarrArgs.FilenameOpsFile == "" {
		return nil
	}

	filenameOpsFile := cu.ChanURLMetarrArgs.FilenameOpsFile
	var err error
	if filenameOpsFile, err = dp.ParseDirectory(filenameOpsFile, v, "filename ops"); err != nil {
		logging.E("Failed to parse directory %q: %v", filenameOpsFile, err)
		return nil
	}

	logging.I("Adding filename ops from file %q...", filenameOpsFile)
	ops, err := file.ReadFileLines(filenameOpsFile)
	if err != nil {
		logging.E("Error loading filename ops from file %q: %v", filenameOpsFile, err)
		return nil
	}

	// Parse filename operations from strings
	parsedOps, err := parsing.ParseFilenameOps(ops)
	if err != nil {
		logging.E("Error parsing filename ops from file %q: %v", filenameOpsFile, err)
		return nil
	}

	// Validate the parsed operations
	if err := validation.ValidateFilenameOps(parsedOps); err != nil {
		logging.E("Error validating filename ops from file %q: %v", filenameOpsFile, err)
		return nil
	}

	logging.I("Loaded %d filename ops from file", len(parsedOps))
	return parsedOps
}

// filterConflictingFilenameOps removes DB ops that are fully identical to file ops
func filterConflictingFilenameOps(fileOps, dbOps []models.FilenameOps) []models.FilenameOps {
	fileOpKeys := make(map[string]bool)

	// Build conflict keys
	for _, op := range fileOps {
		if isConflictingFilenameOp[op.OpType] {
			fileOpKeys[op.OpType] = true
		}
	}

	// Add new operations
	result := make([]models.FilenameOps, 0, len(dbOps))
	for _, op := range dbOps {
		if !fileOpKeys[op.OpType] || !isConflictingFilenameOp[op.OpType] {
			result = append(result, op)
		} else {
			logging.D(2, "File filename op overrides DB op: %s", keys.BuildFilenameOpsKey(op))
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
		key := keys.BuildFilenameOpsKey(op)
		if !matchedFilteredKeys[key] {
			logging.D(2, "Added filename operation %q for video URL %q (Channel: %q)", op, videoURL, channelName)
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
		if includeNonConflicting || !isConflictingFilenameOp[op.OpType] {
			filenameOpsKeys[keys.BuildFilenameOpsKey(op)] = true
		}
	}
	return filenameOpsKeys
}

// filterFilenameOpsByChannel filters out meta operations not matching the current channel.
func filterFilenameOpsByChannel(ops []models.FilenameOps, cURL string) []models.FilenameOps {
	valid := make([]models.FilenameOps, 0, len(ops))
	for _, op := range ops {
		if op.ChannelURL == "" || op.ChannelURL == cURL {
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
		opStr := keys.BuildFilenameOpsKey(op)
		if !seen[opStr] {
			seen[opStr] = true
			result = append(result, opStr)
		}
	}
	return result
}
