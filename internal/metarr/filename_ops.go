package metarr

import (
	"errors"
	"fmt"
	"slices"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
)

// loadAndMergeFilenameOps returns a video's filename ops: its channel URL's from every level, then its matched filtered
// ones.
func loadAndMergeFilenameOps(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// An unusable file holds back Metarr for the whole batch, see app.metarrSkip.
	ops, err := channelURLFilenameOps(cu, dirParser)
	if err != nil {
		logger.Pl.E("Filename ops file for %q can't be used: %v", cu.URL, err)
	}
	return getDedupedFilenameOpStrings(applyFilteredFilenameOps(ops, v.FilteredFilenameOps, v.URL, c.Name))
}

// channelURLFilenameOps returns a channel URL's filename ops from every level, with each level's ops file. Returns the
// errors of files it can't use, whose ops are left out.
func channelURLFilenameOps(cu *models.ChannelURL, dp *parsing.DirectoryParser) ([]models.FilenameOps, error) {
	fileOps := make([][]models.FilenameOps, len(cu.Levels))
	var errs []error
	for i, l := range cu.Levels {
		if l.MetarrArgs == nil {
			continue
		}
		var err error
		fileOps[i], err = LoadFilenameOpsFromFile(l.MetarrArgs.FilenameOpsFile, dp)
		errs = append(errs, err)
	}
	return models.StackFilenameOps(cu.Levels, fileOps), errors.Join(errs...)
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

// LoadFilenameOpsFromFile loads in filename operations from the file at path, if set.
//
// File ops take precedence and will replace any matching DB ops. Returns an error if the file is set but can't be read
// or parsed.
func LoadFilenameOpsFromFile(path string, dp *parsing.DirectoryParser) ([]models.FilenameOps, error) {
	if path == "" {
		return nil, nil
	}

	filenameOpsFile, err := dp.ParseDirectory(path, "filename ops")
	if err != nil {
		return nil, fmt.Errorf("failed to parse filename ops file path %q: %w", path, err)
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

// applyFilteredFilenameOps adds the matched filtered filename ops to ops, each beating any of ops it conflicts with.
func applyFilteredFilenameOps(ops []models.FilenameOps, filteredOps []models.FilteredFilenameOps, videoURL, channelName string) []models.FilenameOps {
	matchedFilteredOps := extractMatchedFilteredFilenameOps(filteredOps)
	result := models.FilterConflictingFilenameOps(matchedFilteredOps, ops)
	for _, op := range result {
		logger.Pl.D(2, "Added filename operation %q for video URL %q (Channel: %q)", sharedparsing.FormatFilenameOp(op), videoURL, channelName)
	}
	return append(result, matchedFilteredOps...)
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
