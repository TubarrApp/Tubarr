package metarr

import (
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"
)

var isNonConflictingMetaOp = map[string]bool{
	"append": true, "copy-to": true, "prefix": true, "replace": true,
}

// loadAndMergeMetaOps loads and merges meta ops: file ops override DB ops, then apply filtering
func loadAndMergeMetaOps(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// Load file ops (highest priority)
	fileMetaOps := loadMetaOpsFromFile(v, cu, dirParser)

	// File ops override DB ops with same key
	nonConflictingDBOps := filterConflictingMetaOps(fileMetaOps, cu.ChanURLMetarrArgs.MetaOps)

	// Combine
	fileAndDBOps := append(fileMetaOps, nonConflictingDBOps...)

	// Merge with filtered meta ops (per-video, stored in Video struct)
	mergedOps := applyFilteredMetaOps(fileAndDBOps, v.FilteredMetaOps, v.URL, c.Name)

	// Omit operations with the wrong channel URL
	channelMatchedOps := filterMetaOpsByChannel(mergedOps, cu.URL)

	// Convert to strings, filtering by URL and deduplicating
	return getDedupedMetaOpStrings(channelMatchedOps)
}

// loadMetaOpsFromFile loads in meta operations from the given file.
//
// File ops take precedence and will replace any matching DB ops.
func loadMetaOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.MetaOps {
	if cu.ChanURLMetarrArgs.MetaOpsFile == "" {
		return nil
	}

	metaOpsFile := cu.ChanURLMetarrArgs.MetaOpsFile
	var err error
	if metaOpsFile, err = dp.ParseDirectory(metaOpsFile, v, "meta ops"); err != nil {
		logging.E("Failed to parse directory %q: %v", metaOpsFile, err)
		return nil
	}

	logging.I("Adding meta ops from file %q...", metaOpsFile)
	ops, err := file.ReadFileLines(metaOpsFile)
	if err != nil {
		logging.E("Error loading meta ops from file %q: %v", metaOpsFile, err)
		return nil
	}

	validOps, err := validation.ValidateMetaOps(ops)
	if err != nil {
		logging.E("Error validating meta ops from file %q: %v", metaOpsFile, err)
		return nil
	}

	logging.I("Loaded %d meta ops from file", len(validOps))
	return validOps
}

// filterConflictingMetaOps removes DB ops that conflict with file ops on the same field
func filterConflictingMetaOps(fileOps, dbOps []models.MetaOps) []models.MetaOps {
	fileOpKeys := make(map[string]bool)

	// Build conflicting key map (Field:OpType only, NOT value [otherwise conflicting keys may not match in map])
	for _, op := range fileOps {
		if !isNonConflictingMetaOp[op.OpType] {
			fileOpKeys[op.Field+":"+op.OpType] = true
		}
	}

	result := make([]models.MetaOps, 0, len(dbOps))
	for _, op := range dbOps {
		if !fileOpKeys[op.Field+":"+op.OpType] || isNonConflictingMetaOp[op.OpType] {
			result = append(result, op)
		} else {
			logging.D(2, "File meta op overrides DB op: %s", parsing.BuildMetaOpsKey(op))
		}
	}
	return result
}

// applyFilteredMetaOps applies filtering rules to meta ops
func applyFilteredMetaOps(ops []models.MetaOps, filteredOps []models.FilteredMetaOps, videoURL, channelName string) []models.MetaOps {
	matchedFilteredOps := extractMatchedFilteredMetaOps(filteredOps)
	matchedFilteredKeys := buildMetaOpKeySet(matchedFilteredOps, true)

	// Keep only ops that aren't being replaced by MATCHED filtered ops
	result := make([]models.MetaOps, 0, len(ops))
	for _, op := range ops {
		key := parsing.BuildMetaOpsKey(op)
		if !matchedFilteredKeys[key] {
			logging.D(2, "Added operation %q for video URL %q (Channel: %q)", op, videoURL, channelName)
			result = append(result, op)
		}
	}

	// Add the matched filtered ops
	result = append(result, matchedFilteredOps...)

	return result
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

// buildKeySet creates a set of keys from meta ops
func buildMetaOpKeySet(ops []models.MetaOps, includeNonConflicting bool) map[string]bool {
	keys := make(map[string]bool, len(ops))
	for _, op := range ops {
		if includeNonConflicting || !isNonConflictingMetaOp[op.OpType] {
			keys[parsing.BuildMetaOpsKey(op)] = true
		}
	}
	return keys
}

// filterMetaOpsByChannel filters out meta operations not matching the current channel.
func filterMetaOpsByChannel(ops []models.MetaOps, cURL string) []models.MetaOps {
	valid := make([]models.MetaOps, 0, len(ops))
	for _, op := range ops {
		if op.ChannelURL == "" || op.ChannelURL == cURL {
			valid = append(valid, op)
		}
	}
	return valid
}

// getDedupedMetaOpStrings converts ops to deduplicated string slice for specific URL.
func getDedupedMetaOpStrings(ops []models.MetaOps) []string {
	seen := make(map[string]bool, len(ops))
	result := make([]string, 0, len(ops))

	for _, op := range ops {
		opStr := parsing.BuildMetaOpsKey(op)
		if !seen[opStr] {
			seen[opStr] = true
			result = append(result, opStr)
		}
	}
	return result
}
