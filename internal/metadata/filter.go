package metadata

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedfilters"
	"github.com/TubarrApp/gocommon/sharedparsing"
)

// errFilterFile marks a filter file that is set but can't be used.
var errFilterFile = errors.New("filter file can't be used")

// checkFilters determines whether a video matches the given filters.
func checkFilters(v *models.Video, filters []models.Filters) bool {
	mustTotal, mustPassed := 0, 0
	anyTotal, anyPassed := 0, 0

	// Filter loop.
	for _, filter := range filters {
		matched, skipped, metaText, err := sharedfilters.Match(v.Metadata.Raw, filter)
		if err != nil {
			logger.Pl.E("Download filter error: Video %q: %v", v.URL, err)
			return false
		}

		// Numerical filters with an absent or empty field are skipped rather than failed,
		// since there is nothing meaningful to compare against.
		if skipped {
			logger.Pl.D(1, "Download filter skip: Video %q has no value for field %q, skipping numerical filter", v.URL, filter.Field)
			continue
		}
		if !matched {
			logger.Pl.D(1, "Download filter mismatch: Video %q has %v for field(s) %q, which fails filter %q", v.URL, metaText, filter.Field, sharedparsing.FormatFilter(filter, true))
		}

		// Add to must/any.
		switch filter.MustAny {
		case sharedconsts.OpMust:
			mustTotal++
			if matched {
				mustPassed++
			}
		case sharedconsts.OpAny:
			anyTotal++
			if matched {
				anyPassed++
			}
		}
	}

	// Post-loop evaluation of must/any filters.
	if mustPassed != mustTotal {
		logger.Pl.D(2, "Download filter mismatch: Video %q did not pass all 'must' filters (passed %d out of %d)", v.URL, mustPassed, mustTotal)
		return false
	}
	if anyTotal > 0 && anyPassed == 0 { // At least one 'any' filter must pass, whatever the 'must' filters did.
		logger.Pl.D(2, "Download filter mismatch: Video %q did not pass any 'any' filters (passed %d out of %d)", v.URL, anyPassed, anyTotal)
		return false
	}

	return true
}

// filteredMetaOpsMatches marks which filtered meta ops match the video, from levels given farthest first, keeping only
// matched ops a nearer level's, or an earlier line's in the same level, doesn't already set.
func filteredMetaOpsMatches(v *models.Video, cu *models.ChannelURL, levels [][]models.FilteredMetaOps) []models.FilteredMetaOps {
	dedupMetaOpsMap := make(map[string]bool)

	metaOps := cu.ChanURLMetarrArgs.MetaOps
	if v.Overrides != nil {
		metaOps = v.Overrides.MetaOps
	}

	// Use buildKey for consistency.
	for _, mo := range metaOps {
		dedupMetaOpsMap[sharedparsing.FormatMetaOp(mo)] = true
	}

	// Nearest level first, so its matched ops claim what they set before a farther level's can.
	byLevel := make([][]models.FilteredMetaOps, len(levels))
	claimed := make(map[string]bool)
	for i, level := range slices.Backward(levels) {
		for _, fmo := range level {
			// Check if filters match.
			filtersMatched, err := sharedfilters.MatchAll(v.Metadata.Raw, fmo.Filters)
			if err != nil {
				logger.Pl.E("Filtered meta ops error: Video %q: %v", v.URL, err)
			}

			// Only keep matched ops nothing nearer or earlier sets, and drop duplicates.
			if filtersMatched {
				kept := make([]models.MetaOps, 0, len(fmo.MetaOps))
				for _, mo := range fmo.MetaOps {
					if key := models.MetaOpConflictKey(mo); key != "" {
						if claimed[key] {
							continue
						}
						claimed[key] = true
					}
					if key := sharedparsing.FormatMetaOp(mo); !dedupMetaOpsMap[key] {
						dedupMetaOpsMap[key] = true
						kept = append(kept, mo)
					}
				}
				fmo.MetaOps = kept
			}

			// Add to result, mark failures.
			fmo.FiltersMatched = filtersMatched
			byLevel[i] = append(byLevel[i], fmo)
		}
	}
	result := slices.Concat(byLevel...)

	if logging.Level() >= 2 {
		logger.Pl.P("Filtered meta op results for channel %q:", v.ChannelName)
		for _, fmo := range result {
			for _, mo := range fmo.MetaOps {
				logger.Pl.P("%q [FILTERS MATCHED?: %v]", sharedparsing.FormatMetaOp(mo), fmo.FiltersMatched)
			}
		}
	}
	return result
}

// filteredFilenameOpsMatches marks which filtered filename ops match the video, from levels given farthest first,
// keeping only matched ops a nearer level's, or an earlier line's in the same level, doesn't already set.
func filteredFilenameOpsMatches(v *models.Video, cu *models.ChannelURL, levels [][]models.FilteredFilenameOps) []models.FilteredFilenameOps {
	dedupFilenameOpsMap := make(map[string]bool)

	filenameOps := cu.ChanURLMetarrArgs.FilenameOps
	if v.Overrides != nil {
		filenameOps = v.Overrides.FilenameOps
	}

	for _, fo := range filenameOps {
		dedupFilenameOpsMap[sharedparsing.FormatFilenameOp(fo)] = true
	}

	// Nearest level first, so its matched ops claim what they set before a farther level's can.
	byLevel := make([][]models.FilteredFilenameOps, len(levels))
	claimed := make(map[string]bool)
	for i, level := range slices.Backward(levels) {
		for _, ffo := range level {
			// Check if filters match.
			filtersMatched, err := sharedfilters.MatchAll(v.Metadata.Raw, ffo.Filters)
			if err != nil {
				logger.Pl.E("Filtered filename ops error: Video %q: %v", v.URL, err)
			}

			// Only keep matched ops nothing nearer or earlier sets, and drop duplicates.
			if filtersMatched {
				kept := make([]models.FilenameOps, 0, len(ffo.FilenameOps))
				for _, fo := range ffo.FilenameOps {
					if key := models.FilenameOpConflictKey(fo); key != "" {
						if claimed[key] {
							continue
						}
						claimed[key] = true
					}
					if key := sharedparsing.FormatFilenameOp(fo); !dedupFilenameOpsMap[key] {
						dedupFilenameOpsMap[key] = true
						kept = append(kept, fo)
					}
				}
				ffo.FilenameOps = kept
			}

			// Add to result, mark failures.
			ffo.FiltersMatched = filtersMatched
			byLevel[i] = append(byLevel[i], ffo)
		}
	}
	result := slices.Concat(byLevel...)

	if logging.Level() >= 2 {
		logger.Pl.P("Filtered filename op results for channel %q:", v.ChannelName)
		for _, ffo := range result {
			for _, fo := range ffo.FilenameOps {
				logger.Pl.P("%q [FILTERS MATCHED?: %v]", sharedparsing.FormatFilenameOp(fo), ffo.FiltersMatched)
			}
		}
	}
	return result
}

// uploadDateFilter filters a video based on its upload date.
func uploadDateFilter(v *models.Video, cu *models.ChannelURL) (passed bool, err error) {
	if v.Metadata.UploadDate.IsZero() {
		logger.Pl.D(1, "Did not parse an upload date from the video %q, skipped applying to/from filters", v.URL)
		return true, nil
	}

	uploadDateNum, err := strconv.Atoi(v.Metadata.UploadDate.Format("20060102"))
	if err != nil {
		return false, fmt.Errorf("failed to convert upload date to integer: %w", err)
	}

	// 'From date' filter
	if passed, err = applyFromDateFilter(v, cu, uploadDateNum); err != nil {
		return false, err
	}
	if !passed {
		return false, nil
	}

	// 'To date' filter
	return applyToDateFilter(v, cu, uploadDateNum)
}

// applyFromDateFilter checks if the video passes the 'from date' filter.
func applyFromDateFilter(v *models.Video, cu *models.ChannelURL, uploadDateNum int) (passed bool, err error) {
	if cu.ChanURLSettings.FromDate == "" {
		return true, nil
	}

	fromDate, err := strconv.Atoi(cu.ChanURLSettings.FromDate)
	if err != nil {
		return false, fmt.Errorf("invalid 'from date' format: %w", err)
	}

	if uploadDateNum < fromDate {
		logger.Pl.I("Video %q failed 'From Date' filter for channel %q. Wanted from: %d Video uploaded at: \"%d\"", v.URL, v.ChannelName, fromDate, uploadDateNum)
		return false, nil
	}

	logger.Pl.S("Video %q passed 'from date' (%q) filter, upload date is \"%d\" (Channel: %q)", v.URL, cu.ChanURLSettings.FromDate, uploadDateNum, v.ChannelName)
	return true, nil
}

// applyToDateFilter checks if the video passes the 'to date' filter.
func applyToDateFilter(v *models.Video, cu *models.ChannelURL, uploadDateNum int) (passed bool, err error) {
	if cu.ChanURLSettings.ToDate == "" {
		return true, nil
	}

	toDate, err := strconv.Atoi(cu.ChanURLSettings.ToDate)
	if err != nil {
		return false, fmt.Errorf("invalid 'to date' format: %w", err)
	}

	if uploadDateNum > toDate {
		logger.Pl.I("Video %q failed 'To Date' filter for channel %q. Wanted to: %d Video uploaded at: \"%d\"", v.URL, v.ChannelName, toDate, uploadDateNum)
		return false, nil
	}

	logger.Pl.S("Video %q passed 'to date' (%q) filter, upload date is \"%d\" (Channel: %q)", v.URL, cu.ChanURLSettings.ToDate, uploadDateNum, v.ChannelName)
	return true, nil
}

// ChannelURLFilters returns a channel URL's download filters from every level, with each level's filter file. Returns an
// error if a filter file can't be used, so videos aren't downloaded unfiltered.
func ChannelURLFilters(cu *models.ChannelURL, dp *parsing.DirectoryParser) ([]models.Filters, error) {
	fileFilters := make([][]models.Filters, len(cu.Levels))
	var errs []error
	for i, l := range cu.Levels {
		if l.Settings == nil {
			continue
		}
		var err error
		fileFilters[i], err = loadFilterOpsFromFile(l.Settings.FilterFile, dp)
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return models.StackFilters(cu.Levels, fileFilters), nil
}

// ChannelURLFilteredMetaOps returns a channel URL's filtered meta ops by level, farthest first, with each level's file.
// Returns the errors of files it can't use, whose ops are left out.
func ChannelURLFilteredMetaOps(cu *models.ChannelURL, dp *parsing.DirectoryParser) ([][]models.FilteredMetaOps, error) {
	fileOps := make([][]models.FilteredMetaOps, len(cu.Levels))
	var errs []error
	for i, l := range cu.Levels {
		if l.MetarrArgs == nil {
			continue
		}
		var err error
		fileOps[i], err = LoadFilteredMetaOpsFromFile(l.MetarrArgs.FilteredMetaOpsFile, dp)
		errs = append(errs, err)
	}
	return models.FilteredMetaOpsByLevel(cu.Levels, fileOps), errors.Join(errs...)
}

// ChannelURLFilteredFilenameOps returns a channel URL's filtered filename ops by level, farthest first, with each level's
// file. Returns the errors of files it can't use, whose ops are left out.
func ChannelURLFilteredFilenameOps(cu *models.ChannelURL, dp *parsing.DirectoryParser) ([][]models.FilteredFilenameOps, error) {
	fileOps := make([][]models.FilteredFilenameOps, len(cu.Levels))
	var errs []error
	for i, l := range cu.Levels {
		if l.MetarrArgs == nil {
			continue
		}
		var err error
		fileOps[i], err = LoadFilteredFilenameOpsFromFile(l.MetarrArgs.FilteredFilenameOpsFile, dp)
		errs = append(errs, err)
	}
	return models.FilteredFilenameOpsByLevel(cu.Levels, fileOps), errors.Join(errs...)
}

// channelURLMoveOps returns a channel URL's move ops from every level, with each level's move ops file.
func channelURLMoveOps(cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.MetaFilterMoveOps {
	fileOps := make([][]models.MetaFilterMoveOps, len(cu.Levels))
	for i, l := range cu.Levels {
		if l.Settings != nil {
			fileOps[i] = loadMoveOpsFromFile(l.Settings.MetaFilterMoveOpFile, dp)
		}
	}
	return models.StackMoveOps(cu.Levels, fileOps)
}

// loadFilterOpsFromFile loads filter operations from the file at path (one per line), if set.
//
// Returns an error if the file is set but can't be read or parsed, so videos aren't downloaded unfiltered.
func loadFilterOpsFromFile(path string, dp *parsing.DirectoryParser) ([]models.Filters, error) {
	if path == "" {
		return nil, nil
	}

	filterFile, err := dp.ParseDirectory(path, "filter-ops")
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse path %q: %w", errFilterFile, path, err)
	}

	logger.Pl.I("Adding filters from file %q...", filterFile)
	filters, err := file.ReadFileLines(filterFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFilterFile, err)
	}

	if len(filters) == 0 {
		logger.Pl.I("No valid filters found in file. Format is one per line 'title:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		return nil, nil
	}

	// Parse filters from strings
	parsedFilters, err := parsing.ParseFilterOps(filters, true)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse filters in %q: %w", errFilterFile, filterFile, err)
	}

	// Validate the parsed filters
	if err := validation.ValidateFilterOps(parsedFilters); err != nil {
		return nil, fmt.Errorf("%w: invalid filters in %q: %w", errFilterFile, filterFile, err)
	}
	if len(parsedFilters) > 0 {
		logger.Pl.D(1, "Found following filters in file:\n\n%v", parsedFilters)
	}

	return parsedFilters, nil
}

// LoadFilteredMetaOpsFromFile loads filtered meta operations from the file at path (one per line), if set.
//
// Returns an error if the file is set but can't be read or parsed.
func LoadFilteredMetaOpsFromFile(path string, dp *parsing.DirectoryParser) ([]models.FilteredMetaOps, error) {
	if path == "" {
		return nil, nil
	}

	filterFile, err := dp.ParseDirectory(path, "metarr-filtered-meta-ops")
	if err != nil {
		return nil, fmt.Errorf("failed to parse filtered meta ops file path %q: %w", path, err)
	}

	logger.Pl.I("Adding filtered meta ops from file %q...", filterFile)
	filters, err := file.ReadOpLines(filterFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read filtered meta ops file: %w", err)
	}

	if len(filters) == 0 {
		logger.Pl.I("No valid filters found in file. Format is one per line 'title:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		return nil, nil
	}

	// Parse and validate filtered meta ops from strings
	parsedFilters, err := parsing.ParseFilteredMetaOps(filters)
	if err != nil {
		return nil, fmt.Errorf("invalid filtered meta ops in %q: %w", filterFile, err)
	}
	if len(parsedFilters) > 0 {
		logger.Pl.D(1, "Found following filtered meta ops in file:\n\n%v", parsedFilters)
	}

	return parsedFilters, nil
}

// LoadFilteredFilenameOpsFromFile loads filtered filename operations from the file at path (one per line), if set.
//
// Returns an error if the file is set but can't be read or parsed.
func LoadFilteredFilenameOpsFromFile(path string, dp *parsing.DirectoryParser) ([]models.FilteredFilenameOps, error) {
	if path == "" {
		return nil, nil
	}

	filterFile, err := dp.ParseDirectory(path, "metarr-filtered-filename-ops")
	if err != nil {
		return nil, fmt.Errorf("failed to parse filtered filename ops file path %q: %w", path, err)
	}

	logger.Pl.I("Adding filtered filename ops from file %q...", filterFile)
	filters, err := file.ReadOpLines(filterFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read filtered filename ops file: %w", err)
	}

	if len(filters) == 0 {
		logger.Pl.I("No valid filtered filename ops found in file. Format is one per line 'title:contains:dogs:must' (Only apply filename ops to videos with 'dogs' in the title)")
		return nil, nil
	}

	// Parse and validate filtered filename ops from strings
	parsedFilters, err := parsing.ParseFilteredFilenameOps(filters)
	if err != nil {
		return nil, fmt.Errorf("invalid filtered filename ops in %q: %w", filterFile, err)
	}
	if len(parsedFilters) > 0 {
		logger.Pl.D(1, "Found following filtered filename ops in file:\n\n%v", parsedFilters)
	}

	return parsedFilters, nil
}

// loadMoveOpsFromFile loads move operations from the file at path (one per line), if set.
func loadMoveOpsFromFile(path string, dp *parsing.DirectoryParser) []models.MetaFilterMoveOps {
	if path == "" {
		return nil
	}

	moveOpFile, err := dp.ParseDirectory(path, "move-ops")
	if err != nil {
		logger.Pl.E("Failed to parse directory %q: %v", path, err)
		return nil
	}

	logger.Pl.I("Adding filter move operations from file %q...", moveOpFile)
	moves, err := file.ReadFileLines(moveOpFile)
	if err != nil {
		logger.Pl.E("Error loading filter move operations from file %q: %v", moveOpFile, err)
	}

	if len(moves) == 0 {
		logger.Pl.I("No valid filter move operations found in file. Format is one per line 'title:contains:dogs||/home/dogs' (Metarr outputs files with 'dogs' in the title to '/home/dogs')")
		return nil
	}

	// Parse meta filter move ops from strings
	parsedMoves, err := parsing.ParseMetaFilterMoveOps(moves)
	if err != nil {
		logger.Pl.E("Error parsing filter move operations from file %q: %v", moveOpFile, err)
		return nil
	}

	// Validate the parsed ops
	if err := validation.ValidateMetaFilterMoveOps(parsedMoves, true); err != nil {
		logger.Pl.E("Error validating filter move operations from file %q: %v", moveOpFile, err)
		return nil
	}
	if len(parsedMoves) > 0 {
		logger.Pl.D(1, "Found following filter move operations in file:\n\n%v", parsedMoves)
	}

	return parsedMoves
}
