package metadata

import (
	"fmt"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// checkFilters determines whether a video matches the given filters.
func checkFilters(v *models.Video, filterType string, filters []models.Filters) bool {
	mustTotal, mustPassed := 0, 0
	anyTotal, anyPassed := 0, 0

	for _, filter := range filters {
		switch filter.MustAny {
		case sharedconsts.OpMust:
			mustTotal++
		case sharedconsts.OpAny:
			anyTotal++
		}

		val, exists := v.MetadataMap[filter.Field]
		strVal := strings.ToLower(fmt.Sprint(val))
		filterVal := strings.ToLower(filter.Value)

		var passed, failHard bool
		switch filter.Value {
		case "":
			passed, failHard = checkFilterWithEmptyValue(filter, filterType, v.URL, exists)
		default:
			passed, failHard = checkFilterWithValue(filter, filterType, v.URL, strVal, filterVal)
		}

		if failHard {
			return false
		}

		if passed {
			switch filter.MustAny {
			case sharedconsts.OpMust:
				mustPassed++
			case sharedconsts.OpAny:
				anyPassed++
			}
		}
	}

	if mustPassed != mustTotal {
		return false
	}
	if anyTotal > 0 && anyPassed == 0 && mustPassed == 0 {
		return false
	}

	return true
}

// filteredMetaOpsMatches checks which arguments match and returns the meta operations.
func filteredMetaOpsMatches(v *models.Video, cu *models.ChannelURL, filteredMetaOps []models.FilteredMetaOps, channelName string) []models.FilteredMetaOps {
	if len(filteredMetaOps) == 0 {
		return nil
	}

	result := make([]models.FilteredMetaOps, 0, len(filteredMetaOps))
	dedupMetaOpsMap := make(map[string]bool)

	// Use buildKey for consistency.
	for _, mo := range cu.ChanURLMetarrArgs.MetaOps {
		dedupMetaOpsMap[models.MetaOpToString(mo, true)] = true
	}

	// Check filtered meta ops.
	for _, fmo := range filteredMetaOps {
		// Check if filters match.
		filtersMatched := checkFilters(v, "Filtered meta ops", fmo.Filters)

		// Deduplicate meta ops using buildKey.
		dedupMetaOps := make([]models.MetaOps, 0, len(fmo.MetaOps))
		for _, mo := range fmo.MetaOps {
			key := models.MetaOpToString(mo, true)
			if !dedupMetaOpsMap[key] {
				dedupMetaOpsMap[key] = true
				dedupMetaOps = append(dedupMetaOps, mo)
			}
		}

		// Add to result, mark failures.
		fmo.MetaOps = dedupMetaOps
		fmo.FiltersMatched = filtersMatched
		result = append(result, fmo)
	}

	if logging.Level >= 2 {
		logger.Pl.P("Filtered meta op results for channel %q:", channelName)
		for _, fmo := range result {
			for _, mo := range fmo.MetaOps {
				logger.Pl.P("%q [FILTERS MATCHED?: %v]", models.MetaOpToString(mo, true), fmo.FiltersMatched)
			}
		}
	}
	return result
}

// filteredFilenameOpsMatches checks which arguments match and returns the filename operations.
func filteredFilenameOpsMatches(v *models.Video, cu *models.ChannelURL, filteredFilenameOps []models.FilteredFilenameOps, channelName string) []models.FilteredFilenameOps {
	if len(filteredFilenameOps) == 0 {
		return nil
	}

	result := make([]models.FilteredFilenameOps, 0, len(filteredFilenameOps))
	dedupFilenameOpsMap := make(map[string]bool)

	for _, fo := range cu.ChanURLMetarrArgs.FilenameOps {
		dedupFilenameOpsMap[models.FilenameOpToString(fo, true)] = true
	}

	// Check filtered filename ops.
	for _, ffo := range filteredFilenameOps {
		// Check if filters match.
		filtersMatched := checkFilters(v, "Filtered filename ops", ffo.Filters)

		// Deduplicate filename ops using buildKey.
		dedupFilenameOps := make([]models.FilenameOps, 0, len(ffo.FilenameOps))
		for _, fo := range ffo.FilenameOps {
			key := models.FilenameOpToString(fo, true)
			if !dedupFilenameOpsMap[key] {
				dedupFilenameOpsMap[key] = true
				dedupFilenameOps = append(dedupFilenameOps, fo)
			}
		}

		// Add to result, mark failures.
		ffo.FilenameOps = dedupFilenameOps
		ffo.FiltersMatched = filtersMatched
		result = append(result, ffo)
	}

	if logging.Level >= 2 {
		logger.Pl.P("Filtered filename op results for channel %q:", channelName)
		for _, ffo := range result {
			for _, fo := range ffo.FilenameOps {
				logger.Pl.P("%q [FILTERS MATCHED?: %v]", models.FilenameOpToString(fo, true), ffo.FiltersMatched)
			}
		}
	}
	return result
}

// checkFilterWithEmptyValue checks a filter's empty value against its matching metadata field.
func checkFilterWithEmptyValue(filter models.Filters, filterType, videoURL string, exists bool) (passed, failHard bool) {
	switch filter.ContainsOmits {
	case consts.FilterContains:
		if !exists {
			logger.Pl.I("%s mismatch: Video %q does not contain desired field %q", filterType, videoURL, filter.Field)
			return false, true
		}
		return true, false
	case consts.FilterOmits:
		if exists && filter.MustAny == sharedconsts.OpMust {
			logger.Pl.I("%s mismatch: Video %q contains unwanted field %q", filterType, videoURL, filter.Field)
			return false, true
		}
		return !exists, false
	}
	return false, false
}

// checkFilterWithValue checks a filter's value against its matching metadata field.
func checkFilterWithValue(filter models.Filters, filterType, videoURL, strVal, filterVal string) (passed, failHard bool) {
	switch filter.ContainsOmits {
	case consts.FilterContains:
		if strings.Contains(strVal, filterVal) {
			return true, false
		}
		if filter.MustAny == sharedconsts.OpMust {
			logger.Pl.I("%s mismatch: Video %q does not contain desired %q in %q", filterType, videoURL, filter.Value, filter.Field)
			return false, true
		}
	case consts.FilterOmits:
		if !strings.Contains(strVal, filterVal) {
			return true, false
		}
		if filter.MustAny == sharedconsts.OpMust {
			logger.Pl.I("%s mismatch: Video %q contains unwanted %q in %q", filterType, videoURL, filter.Value, filter.Field)
			return false, true
		}
	}
	return false, false
}

// uploadDateFilter filters a video based on its upload date.
func uploadDateFilter(v *models.Video, cu *models.ChannelURL, channelName string) (passed bool, err error) {
	if v.UploadDate.IsZero() {
		logger.Pl.D(1, "Did not parse an upload date from the video %q, skipped applying to/from filters", v.URL)
		return true, nil
	}

	uploadDateNum, err := strconv.Atoi(v.UploadDate.Format("20060102"))
	if err != nil {
		return false, fmt.Errorf("failed to convert upload date to integer: %w", err)
	}

	// 'From date' filter
	if passed, err = applyFromDateFilter(v, cu, uploadDateNum, channelName); err != nil {
		return false, err
	}
	if !passed {
		return false, nil
	}

	// 'To date' filter
	if passed, err = applyToDateFilter(v, cu, uploadDateNum, channelName); err != nil {
		return false, err
	}
	if !passed {
		return false, nil
	}

	return true, nil
}

// applyFromDateFilter checks if the video passes the 'from date' filter.
func applyFromDateFilter(v *models.Video, cu *models.ChannelURL, uploadDateNum int, channelName string) (passed bool, err error) {
	if cu.ChanURLSettings.FromDate == "" {
		return true, nil
	}

	fromDate, err := strconv.Atoi(cu.ChanURLSettings.FromDate)
	if err != nil {
		return false, fmt.Errorf("invalid 'from date' format: %w", err)
	}

	if uploadDateNum < fromDate {
		logger.Pl.I("Video %q failed 'From Date' filter for channel %q. Wanted from: %q Video uploaded at: \"%d\"", v.URL, channelName, fromDate, uploadDateNum)
		return false, nil
	}

	logger.Pl.S("Video %q passed 'from date' (%q) filter, upload date is \"%d\" (Channel: %q)", v.URL, cu.ChanURLSettings.FromDate, uploadDateNum, channelName)
	return true, nil
}

// applyToDateFilter checks if the video passes the 'to date' filter.
func applyToDateFilter(v *models.Video, cu *models.ChannelURL, uploadDateNum int, channelName string) (passed bool, err error) {
	if cu.ChanURLSettings.ToDate == "" {
		return true, nil
	}

	toDate, err := strconv.Atoi(cu.ChanURLSettings.ToDate)
	if err != nil {
		return false, fmt.Errorf("invalid 'to date' format: %w", err)
	}

	if uploadDateNum > toDate {
		logger.Pl.I("Video %q failed 'To Date' filter for channel %q. Wanted from: %q Video uploaded at: \"%d\"", v.URL, channelName, toDate, uploadDateNum)
		return false, nil
	}

	logger.Pl.S("Video %q passed 'to date' (%q) filter, upload date is \"%d\" (Channel: %q)", v.URL, cu.ChanURLSettings.FromDate, uploadDateNum, channelName)
	return true, nil
}

// loadFilterOpsFromFile loads filter operations from a file (one per line).
func loadFilterOpsFromFile(cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.Filters {
	var err error

	if cu.ChanURLSettings.FilterFile == "" {
		return nil
	}

	filterFile := cu.ChanURLSettings.FilterFile

	if filterFile, err = dp.ParseDirectory(filterFile, "filter-ops"); err != nil {
		logger.Pl.E("Failed to parse directory %q: %v", filterFile, err)
		return nil
	}

	logger.Pl.I("Adding filters from file %q...", filterFile)
	filters, err := file.ReadFileLines(filterFile)
	if err != nil {
		logger.Pl.E("Error loading filters from file %q: %v", filterFile, err)
	}

	if len(filters) == 0 {
		logger.Pl.I("No valid filters found in file. Format is one per line 'title:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		return nil
	}

	// Parse filters from strings
	parsedFilters, err := parsing.ParseFilterOps(filters)
	if err != nil {
		logger.Pl.E("Error parsing filters from file %v: %v", filterFile, err)
		return nil
	}

	// Validate the parsed filters
	if err := validation.ValidateFilterOps(parsedFilters); err != nil {
		logger.Pl.E("Error validating filters from file %v: %v", filterFile, err)
	}
	if len(parsedFilters) > 0 {
		logger.Pl.D(1, "Found following filters in file:\n\n%v", parsedFilters)
	}

	return parsedFilters
}

// loadFilteredMetaOpsFromFile loads filter operations from a file (one per line).
func loadFilteredMetaOpsFromFile(cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.FilteredMetaOps {
	var err error

	if cu.ChanURLMetarrArgs.FilteredMetaOpsFile == "" {
		return nil
	}

	filterFile := cu.ChanURLMetarrArgs.FilteredMetaOpsFile

	if filterFile, err = dp.ParseDirectory(filterFile, "metarr-filtered-meta-ops"); err != nil {
		logger.Pl.E("Failed to parse directory %q: %v", filterFile, err)
		return nil
	}

	logger.Pl.I("Adding filtered meta ops from file %q...", filterFile)
	filters, err := file.ReadFileLines(filterFile)
	if err != nil {
		logger.Pl.E("Error loading filters from file %q: %v", filterFile, err)
	}

	if len(filters) == 0 {
		logger.Pl.I("No valid filters found in file. Format is one per line 'title:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		return nil
	}

	// Parse filtered meta ops from strings
	parsedFilters, err := parsing.ParseFilteredMetaOps(filters)
	if err != nil {
		logger.Pl.E("Error parsing filtered meta ops from file %v: %v", filterFile, err)
		return nil
	}

	// Validate the parsed ops
	if err := validation.ValidateFilteredMetaOps(parsedFilters); err != nil {
		logger.Pl.E("Error validating filtered meta ops from file %v: %v", filterFile, err)
	}
	if len(parsedFilters) > 0 {
		logger.Pl.D(1, "Found following filtered meta ops in file:\n\n%v", parsedFilters)
	}

	return parsedFilters
}

// loadFilteredFilenameOpsFromFile loads filtered filename operations from a file (one per line).
func loadFilteredFilenameOpsFromFile(cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.FilteredFilenameOps {
	var err error

	if cu.ChanURLMetarrArgs.FilteredFilenameOpsFile == "" {
		return nil
	}

	filterFile := cu.ChanURLMetarrArgs.FilteredFilenameOpsFile

	if filterFile, err = dp.ParseDirectory(filterFile, "metarr-filtered-filename-ops"); err != nil {
		logger.Pl.E("Failed to parse directory %q: %v", filterFile, err)
		return nil
	}

	logger.Pl.I("Adding filtered filename ops from file %q...", filterFile)
	filters, err := file.ReadFileLines(filterFile)
	if err != nil {
		logger.Pl.E("Error loading filtered filename ops from file %q: %v", filterFile, err)
	}

	if len(filters) == 0 {
		logger.Pl.I("No valid filtered filename ops found in file. Format is one per line 'title:contains:dogs:must' (Only apply filename ops to videos with 'dogs' in the title)")
		return nil
	}

	// Parse filtered filename ops from strings
	parsedFilters, err := parsing.ParseFilteredFilenameOps(filters)
	if err != nil {
		logger.Pl.E("Error parsing filtered filename ops from file %v: %v", filterFile, err)
		return nil
	}

	// Validate the parsed ops
	if err := validation.ValidateFilteredFilenameOps(parsedFilters); err != nil {
		logger.Pl.E("Error validating filtered filename ops from file %v: %v", filterFile, err)
	}
	if len(parsedFilters) > 0 {
		logger.Pl.D(1, "Found following filtered filename ops in file:\n\n%v", parsedFilters)
	}

	return parsedFilters
}

// loadMoveOpsFromFile loads move operations from a file (one per line).
func loadMoveOpsFromFile(cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.MetaFilterMoveOps {
	var err error

	if cu.ChanURLSettings.MetaFilterMoveOpFile == "" {
		return nil
	}

	moveOpFile := cu.ChanURLSettings.MetaFilterMoveOpFile

	if moveOpFile, err = dp.ParseDirectory(moveOpFile, "move-ops"); err != nil {
		logger.Pl.E("Failed to parse directory %q: %v", moveOpFile, err)
		return nil
	}

	logger.Pl.I("Adding filter move operations from file %q...", moveOpFile)
	moves, err := file.ReadFileLines(moveOpFile)
	if err != nil {
		logger.Pl.E("Error loading filter move operations from file %q: %v", moveOpFile, err)
	}

	if len(moves) == 0 {
		logger.Pl.I("No valid filter move operations found in file. Format is one per line 'title:dogs:/home/dogs' (Metarr outputs files with 'dogs' in the title to '/home/dogs)")
		return nil
	}

	// Parse meta filter move ops from strings
	parsedMoves, err := parsing.ParseMetaFilterMoveOps(moves)
	if err != nil {
		logger.Pl.E("Error parsing filter move operations from file %q: %v", moveOpFile, err)
		return nil
	}

	// Validate the parsed ops
	if err := validation.ValidateMetaFilterMoveOps(parsedMoves); err != nil {
		logger.Pl.E("Error validating filter move operations from file %q: %v", moveOpFile, err)
	}
	if len(parsedMoves) > 0 {
		logger.Pl.D(1, "Found following filter move operations in file:\n\n%v", parsedMoves)
	}

	return parsedMoves
}
