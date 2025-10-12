package metadata

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"
)

// filterOpsFilter determines whether a video should be filtered out based on metadata it contains or omits.
func filterOpsFilter(v *models.Video, filters []models.DLFilters) bool {
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
			passed, failHard = checkFilterWithEmptyValue(filter, exists)
		default: // non-empty filter value
			passed, failHard = checkFilterWithValue(filter, strVal, filterVal) // Treats non-existent and empty metadata fields the same...
		}

		if failHard {
			if err := removeUnwantedJSON(v.JSONPath); err != nil {
				logging.E("Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
			}
			return false
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

	if len(filters) > 0 {
		logging.I("Video passed download filter checks: %v", filters)
	}
	return true
}

// checkFilterWithEmptyValue checks a filter's empty value against its matching metadata field.
func checkFilterWithEmptyValue(filter models.DLFilters, exists bool) (passed, failHard bool) {
	switch filter.Type {
	case consts.FilterContains:
		if !exists {
			logging.I("Filtering: field %q not found and must contain it", filter.Field)
			return false, true
		}
		return true, false
	case consts.FilterOmits:
		if exists && filter.MustAny == "must" {
			logging.I("Filtering: field %q exists but must omit it", filter.Field)
			return false, true
		}
		return !exists, false
	}
	return false, false
}

// checkFilterWithValue checks a filter's value against its matching metadata field.
func checkFilterWithValue(filter models.DLFilters, strVal, filterVal string) (passed, failHard bool) {
	switch filter.Type {
	case consts.FilterContains:
		if strings.Contains(strVal, filterVal) {
			return true, false
		}
		if filter.MustAny == "must" {
			logging.I("Filtering out: does not contain %q in %q", filter.Value, filter.Field)
			return false, true
		}
	case consts.FilterOmits:
		if !strings.Contains(strVal, filterVal) {
			return true, false
		}
		if filter.MustAny == "must" {
			logging.I("Filtering out: contains %q in %q", filter.Value, filter.Field)
			return false, true
		}
	}
	return false, false
}

// uploadDateFilter filters a video based on its upload date.
func uploadDateFilter(v *models.Video, cu *models.ChannelURL) (passed bool, err error) {
	if v.UploadDate.IsZero() {
		logging.D(1, "Did not parse an upload date from the video %q, skipped applying to/from filters", v.URL)
		return true, nil
	}

	uploadDateNum, err := strconv.Atoi(v.UploadDate.Format("20060102"))
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
	if passed, err = applyToDateFilter(v, cu, uploadDateNum); err != nil {
		return false, err
	}
	if !passed {
		return false, nil
	}

	return true, nil
}

// applyFromDateFilter checks if the video passes the 'from date' filter.
func applyFromDateFilter(v *models.Video, cu *models.ChannelURL, uploadDateNum int) (passed bool, err error) {
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
		return false, nil
	}

	logging.D(1, "URL %q passed 'from date' (%q) filter, upload date is \"%d\"", v.URL, cu.ChanURLSettings.FromDate, uploadDateNum)
	return true, nil
}

// applyToDateFilter checks if the video passes the 'to date' filter.
func applyToDateFilter(v *models.Video, cu *models.ChannelURL, uploadDateNum int) (passed bool, err error) {
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
		return false, nil
	}

	logging.D(1, "URL %q passed 'to date' (%q) filter, upload date is \"%d\"", v.URL, cu.ChanURLSettings.FromDate, uploadDateNum)
	return true, nil
}

// loadFilterOpsFromFile loads filter operations from a file (one per line).
func loadFilterOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.DLFilters {
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

// loadMoveOpsFromFile loads move operations from a file (one per line).
func loadMoveOpsFromFile(v *models.Video, cu *models.ChannelURL, dp *parsing.DirectoryParser) []models.MoveOps {
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

	logging.S(0, "Removed unwanted JSON file %q", path)
	return nil
}
