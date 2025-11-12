package metadata

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"
)

// filterOpsFilter determines whether a video should be filtered out based on metadata it contains or omits.
func filterOpsFilter(v *models.Video, filters []models.Filters, channelName string) bool {
	mustTotal, mustPassed := 0, 0
	anyTotal, anyPassed := 0, 0

	for _, filter := range filters {

		switch filter.MustAny {
		case "must":
			mustTotal++
		case "any":
			anyTotal++
		}

		val, exists := v.MetadataMap[filter.Field]
		strVal := strings.ToLower(fmt.Sprint(val))
		filterVal := strings.ToLower(filter.Value)

		var passed, failHard bool
		switch filter.Value {
		case "": // empty filter value
			passed, failHard = checkFilterWithEmptyValue(filter, "Download Filters", v.URL, exists)
		default: // non-empty filter value
			passed, failHard = checkFilterWithValue(filter, "Download Filters", v.URL, strVal, filterVal) // Treats non-existent and empty metadata fields the same...
		}

		if failHard {
			if err := removeUnwantedJSON(v.JSONPath); err != nil {
				logging.E("Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
			}
			logging.I("Video %q failed hard on filter %v for channel %q", v.URL, filter, channelName)
			return false
		}

		if passed {
			switch filter.MustAny {
			case "must":
				mustPassed++
			case "any":
				anyPassed++
			}
			logging.S("Video %q passed filter %v for channel %q", v.URL, filter, channelName)
		}
	}

	// Tally checks
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

	// Use buildKey for consistency
	for _, mo := range cu.ChanURLMetarrArgs.MetaOps {
		dedupMetaOpsMap[keys.BuildMetaOpsKeyWithChannel(mo)] = true
	}

	for _, fmo := range filteredMetaOps {
		// Check if filters match
		filtersMatched := checkFiltersOnly(v, "Filtered Meta Ops", fmo.Filters)

		// Deduplicate meta ops using buildKey
		dedupMetaOps := make([]models.MetaOps, 0, len(fmo.MetaOps))
		for _, mo := range fmo.MetaOps {
			key := keys.BuildMetaOpsKeyWithChannel(mo)
			if !dedupMetaOpsMap[key] {
				dedupMetaOpsMap[key] = true
				dedupMetaOps = append(dedupMetaOps, mo)
			}
		}

		// Add to result, mark failures
		fmo.MetaOps = dedupMetaOps
		fmo.FiltersMatched = filtersMatched
		result = append(result, fmo)
	}

	if logging.Level >= 2 {
		logging.P("Filtered meta op results for channel %q:", channelName)
		for _, fmo := range result {
			for _, mo := range fmo.MetaOps {
				logging.P("%q [FILTERS MATCHED?: %v]", keys.BuildMetaOpsKeyWithChannel(mo), fmo.FiltersMatched)
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
		dedupFilenameOpsMap[keys.BuildFilenameOpsKeyWithChannel(fo)] = true
	}

	for _, ffo := range filteredFilenameOps {
		// Check if filters match
		filtersMatched := checkFiltersOnly(v, "Filtered Filename Ops", ffo.Filters)

		// Deduplicate filename ops using buildKey
		dedupFilenameOps := make([]models.FilenameOps, 0, len(ffo.FilenameOps))
		for _, fo := range ffo.FilenameOps {
			key := keys.BuildFilenameOpsKeyWithChannel(fo)
			if !dedupFilenameOpsMap[key] {
				dedupFilenameOpsMap[key] = true
				dedupFilenameOps = append(dedupFilenameOps, fo)
			}
		}

		// Add to result, mark failures
		ffo.FilenameOps = dedupFilenameOps
		ffo.FiltersMatched = filtersMatched
		result = append(result, ffo)
	}

	if logging.Level >= 2 {
		logging.P("Filtered filename op results for channel %q:", channelName)
		for _, ffo := range result {
			for _, fo := range ffo.FilenameOps {
				logging.P("%q [FILTERS MATCHED?: %v]", keys.BuildFilenameOpsKeyWithChannel(fo), ffo.FiltersMatched)
			}
		}
	}
	return result
}

// checkFiltersOnly checks if filters match WITHOUT removing JSON files on failure.
func checkFiltersOnly(v *models.Video, filterType string, filters []models.Filters) bool {
	mustTotal, mustPassed := 0, 0
	anyTotal, anyPassed := 0, 0

	for _, filter := range filters {
		switch filter.MustAny {
		case "must":
			mustTotal++
		case "any":
			anyTotal++
		}

		val, exists := v.MetadataMap[filter.Field]
		strVal := strings.ToLower(fmt.Sprint(val))
		filterVal := strings.ToLower(filter.Value)

		var passed bool
		switch filter.Value {
		case "": // empty filter value
			passed, _ = checkFilterWithEmptyValue(filter, filterType, v.URL, exists)
		default: // non-empty filter value
			passed, _ = checkFilterWithValue(filter, filterType, v.URL, strVal, filterVal)
		}

		if passed {
			switch filter.MustAny {
			case "must":
				mustPassed++
			case "any":
				anyPassed++
			}
		}
	}

	// Tally checks
	if mustPassed != mustTotal {
		return false
	}
	if anyTotal > 0 && anyPassed == 0 && mustPassed == 0 {
		return false
	}

	return true
}

// checkFilterWithEmptyValue checks a filter's empty value against its matching metadata field.
func checkFilterWithEmptyValue(filter models.Filters, filterType, videoURL string, exists bool) (passed, failHard bool) {
	switch filter.ContainsOmits {
	case consts.FilterContains:
		if !exists {
			logging.I("%s mismatch: Video %q does not contain desired field %q", filterType, videoURL, filter.Field)
			return false, true
		}
		return true, false
	case consts.FilterOmits:
		if exists && filter.MustAny == "must" {
			logging.I("%s mismatch: Video %q contains unwanted field %q", filterType, videoURL, filter.Field)
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
		if filter.MustAny == "must" {
			logging.I("%s mismatch: Video %q does not contain desired %q in %q", filterType, videoURL, filter.Value, filter.Field)
			return false, true
		}
	case consts.FilterOmits:
		if !strings.Contains(strVal, filterVal) {
			return true, false
		}
		if filter.MustAny == "must" {
			logging.I("%s mismatch: Video %q contains unwanted %q in %q", filterType, videoURL, filter.Value, filter.Field)
			return false, true
		}
	}
	return false, false
}

// uploadDateFilter filters a video based on its upload date.
func uploadDateFilter(v *models.Video, cu *models.ChannelURL, channelName string) (passed bool, err error) {
	if v.UploadDate.IsZero() {
		logging.D(1, "Did not parse an upload date from the video %q, skipped applying to/from filters", v.URL)
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
		if err := removeUnwantedJSON(v.JSONPath); err != nil {
			logging.E("Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
		}
		return false, fmt.Errorf("invalid 'from date' format: %w", err)
	}

	if uploadDateNum < fromDate {
		logging.I("Filtering out %q: uploaded on \"%d\", before 'from date' %q", v.URL, uploadDateNum, cu.ChanURLSettings.FromDate)
		if err := removeUnwantedJSON(v.JSONPath); err != nil {
			logging.E("Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
		}
		logging.I("Video %q failed 'From Date' filter for channel %q. Wanted from: %q Video uploaded at: \"%d\"", v.URL, channelName, fromDate, uploadDateNum)
		return false, nil
	}

	logging.S("Video %q passed 'from date' (%q) filter, upload date is \"%d\" (Channel: %q)", v.URL, cu.ChanURLSettings.FromDate, uploadDateNum, channelName)
	return true, nil
}

// applyToDateFilter checks if the video passes the 'to date' filter.
func applyToDateFilter(v *models.Video, cu *models.ChannelURL, uploadDateNum int, channelName string) (passed bool, err error) {
	if cu.ChanURLSettings.ToDate == "" {
		return true, nil
	}

	toDate, err := strconv.Atoi(cu.ChanURLSettings.ToDate)
	if err != nil {
		if err := removeUnwantedJSON(v.JSONPath); err != nil {
			logging.E("Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
		}
		return false, fmt.Errorf("invalid 'to date' format: %w", err)
	}

	if uploadDateNum > toDate {
		logging.I("Filtering out %q: uploaded on \"%d\", after 'to date' %q", v.URL, uploadDateNum, cu.ChanURLSettings.ToDate)
		if err := removeUnwantedJSON(v.JSONPath); err != nil {
			logging.E("Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
		}
		logging.I("Video %q failed 'To Date' filter for channel %q. Wanted from: %q Video uploaded at: \"%d\"", v.URL, channelName, toDate, uploadDateNum)
		return false, nil
	}

	logging.S("Video %q passed 'to date' (%q) filter, upload date is \"%d\" (Channel: %q)", v.URL, cu.ChanURLSettings.FromDate, uploadDateNum, channelName)
	return true, nil
}

// loadFilterOpsFromFile loads filter operations from a file (one per line).
func loadFilterOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.Filters {
	var err error

	if cu.ChanURLSettings.FilterFile == "" {
		return nil
	}

	filterFile := cu.ChanURLSettings.FilterFile

	if filterFile, err = dp.ParseDirectory(filterFile, v, "filter-ops"); err != nil {
		logging.E("Failed to parse directory %q: %v", filterFile, err)
		return nil
	}

	logging.I("Adding filters from file %q...", filterFile)
	filters, err := file.ReadFileLines(filterFile)
	if err != nil {
		logging.E("Error loading filters from file %q: %v", filterFile, err)
	}

	if len(filters) == 0 {
		logging.I("No valid filters found in file. Format is one per line 'title:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		return nil
	}

	validFilters, err := validation.ValidateFilterOps(filters)
	if err != nil {
		logging.E("Error loading filters from file %v: %v", filterFile, err)
	}
	if len(validFilters) > 0 {
		logging.D(1, "Found following filters in file:\n\n%v", validFilters)
	}

	return validFilters
}

// loadFilteredMetaOpsFromFile loads filter operations from a file (one per line).
func loadFilteredMetaOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.FilteredMetaOps {
	var err error

	if cu.ChanURLMetarrArgs.FilteredMetaOpsFile == "" {
		return nil
	}

	filterFile := cu.ChanURLMetarrArgs.FilteredMetaOpsFile

	if filterFile, err = dp.ParseDirectory(filterFile, v, "filtered-meta-ops"); err != nil {
		logging.E("Failed to parse directory %q: %v", filterFile, err)
		return nil
	}

	logging.I("Adding filtered meta ops from file %q...", filterFile)
	filters, err := file.ReadFileLines(filterFile)
	if err != nil {
		logging.E("Error loading filters from file %q: %v", filterFile, err)
	}

	if len(filters) == 0 {
		logging.I("No valid filters found in file. Format is one per line 'title:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		return nil
	}

	validFilters, err := validation.ValidateFilteredMetaOps(filters)
	if err != nil {
		logging.E("Error loading filters from file %v: %v", filterFile, err)
	}
	if len(validFilters) > 0 {
		logging.D(1, "Found following filters in file:\n\n%v", validFilters)
	}

	return validFilters
}

// loadFilteredFilenameOpsFromFile loads filtered filename operations from a file (one per line).
func loadFilteredFilenameOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.FilteredFilenameOps {
	var err error

	if cu.ChanURLMetarrArgs.FilteredFilenameOpsFile == "" {
		return nil
	}

	filterFile := cu.ChanURLMetarrArgs.FilteredFilenameOpsFile

	if filterFile, err = dp.ParseDirectory(filterFile, v, "filtered-filename-ops"); err != nil {
		logging.E("Failed to parse directory %q: %v", filterFile, err)
		return nil
	}

	logging.I("Adding filtered filename ops from file %q...", filterFile)
	filters, err := file.ReadFileLines(filterFile)
	if err != nil {
		logging.E("Error loading filtered filename ops from file %q: %v", filterFile, err)
	}

	if len(filters) == 0 {
		logging.I("No valid filtered filename ops found in file. Format is one per line 'title:contains:dogs:must' (Only apply filename ops to videos with 'dogs' in the title)")
		return nil
	}

	validFilters, err := validation.ValidateFilteredFilenameOps(filters)
	if err != nil {
		logging.E("Error loading filtered filename ops from file %v: %v", filterFile, err)
	}
	if len(validFilters) > 0 {
		logging.D(1, "Found following filtered filename ops in file:\n\n%v", validFilters)
	}

	return validFilters
}

// loadMoveOpsFromFile loads move operations from a file (one per line).
func loadMoveOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.MetaFilterMoveOps {
	var err error

	if cu.ChanURLSettings.MoveOpFile == "" {
		return nil
	}

	moveOpFile := cu.ChanURLSettings.MoveOpFile

	if moveOpFile, err = dp.ParseDirectory(moveOpFile, v, "move-ops"); err != nil {
		logging.E("Failed to parse directory %q: %v", moveOpFile, err)
		return nil
	}

	logging.I("Adding filter move operations from file %q...", moveOpFile)
	moves, err := file.ReadFileLines(moveOpFile)
	if err != nil {
		logging.E("Error loading filter move operations from file %q: %v", moveOpFile, err)
	}

	if len(moves) == 0 {
		logging.I("No valid filter move operations found in file. Format is one per line 'title:dogs:/home/dogs' (Metarr outputs files with 'dogs' in the title to '/home/dogs)")
	}

	validMoves, err := validation.ValidateMoveOps(moves)
	if err != nil {
		logging.E("Error loading filter move operations from file %q: %v", moveOpFile, err)
	}
	if len(validMoves) > 0 {
		logging.D(1, "Found following filter move operations in file:\n\n%v", validMoves)
	}

	return validMoves
}

// removeUnwantedJSON removes filtered out JSON files.
func removeUnwantedJSON(path string) error {
	if path == "" {
		err := errors.New("path sent in empty, not removing")
		return err
	}

	check, err := os.Stat(path)
	if err != nil {
		err = fmt.Errorf("not deleting unwanted JSON file, got error: %w", err)
		return err
	}

	switch {
	case check.IsDir():
		err := fmt.Errorf("JSON path sent in as a directory %q, not deleting", path)
		return err
	case !check.Mode().IsRegular():
		err := fmt.Errorf("JSON file %q is not a regular file, not deleting", path)
		return err
	}

	if err := os.Remove(path); err != nil {
		err = fmt.Errorf("failed to remove unwanted JSON file at %q: %w", path, err)
		return err
	}

	logging.S("Removed unwanted JSON file %q", path)
	return nil
}
