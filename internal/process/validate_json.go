package process

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"tubarr/internal/cfg"
	enums "tubarr/internal/domain/enums"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// validateJson checks if the JSON is valid and if it passes filter checks
func validateJson(dl *models.DLs) (valid bool, err error) {

	if dl == nil {
		return false, fmt.Errorf("dl model is null")
	}

	if dl.JSONPath == "" {
		return false, fmt.Errorf("json path cannot be empty")
	}

	jInfo, err := os.Stat(dl.JSONPath)
	if err != nil {
		return false, err
	}

	switch {
	case jInfo.IsDir():
		return false, fmt.Errorf("json path should be path to json file, not directory")
	case filepath.Ext(jInfo.Name()) != ".json":
		return false, fmt.Errorf("json file should end in .json")
	case !jInfo.Mode().IsRegular():
		return false, fmt.Errorf("json not a regular file")
	}

	f, err := os.Open(dl.JSONPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	logging.D(1, "About to decode JSON to metamap")

	m := make(map[string]interface{})
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&m); err != nil {
		return false, fmt.Errorf("failed to decode JSON: %w", err)
	}

	if len(m) > 0 {
		dl.Metamap = m
	} else {
		return false, nil
	}

	if valid, err = filterRequests(dl); err != nil {
		return false, err
	}
	if !valid {
		return false, nil
	}

	return valid, nil
}

// filterRequests uses user input filters to check if the video should be downloaded
func filterRequests(dl *models.DLs) (valid bool, err error) {

	// Check if filters are set and validate
	if !cfg.IsSet(keys.FilterOps) {
		logging.D(1, "No filters set, returning true")
		return true, nil
	}

	filters, ok := cfg.Get(keys.FilterOps).([]*models.DLFilter)
	if !ok || filters == nil {
		return false, fmt.Errorf("filter configuration is nil or has wrong type; expected []*models.DLFilter, got %T", filters)
	}

	// Apply filters if any match metadata content
	for _, filter := range filters {

		val, exists := dl.Metamap[filter.Field]

		// Performing field existent filter checks
		if !exists {
			switch filter.FilterType {
			case enums.DLFILTER_CONTAINS_FIELD:

				logging.I("Filtering: Field '%s' not found in metadata for URL '%s' and filter is set to require it, filtering out", filter.Field, dl.URL)
				if err := removeUnwantedJSON(dl.JSONPath); err != nil {
					logging.E(0, err.Error())
				}
				return false, nil // Failed check (does not exist in meta and filter set to contain)

			case enums.DLFILTER_OMIT_FIELD:
				logging.D(2, "Passed check: Field '%s' does not exist", filter.Field)
				continue // Passed check
			}
		}
		switch filter.FilterType {
		case enums.DLFILTER_OMIT_FIELD:

			logging.I("Filtering: Field '%s' found in metadata for URL '%s' and filter is set to omit it, filtering out", filter.Field, dl.URL)
			if err := removeUnwantedJSON(dl.JSONPath); err != nil {
				logging.E(0, err.Error())
			}
			return false, nil // Failed check (does exist in meta and filter set to omit)

		case enums.DLFILTER_CONTAINS_FIELD:
			logging.D(2, "Passed check: Field '%s' exists", filter.Field)
			continue // Passed this check
		}

		// Performing field contents checks
		strVal, ok := val.(string)
		if !ok {
			logging.E(0, "Unexpected type for field %s: expected string, got %T", filter.Field, val)
			continue
		}

		lowerStrVal := strings.ToLower(strVal)
		lowerFilterVal := strings.ToLower(filter.Value)

		// Apply the filter logic
		switch filter.FilterType {

		case enums.DLFILTER_OMIT:
			if strings.Contains(lowerStrVal, lowerFilterVal) {

				logging.D(1, "Filtering out video '%s' which contains '%s' in field '%s'", dl.URL, filter.Value, filter.Field)
				if err := removeUnwantedJSON(dl.JSONPath); err != nil {
					logging.E(0, err.Error())
				}
				return false, nil
			}

		case enums.DLFILTER_CONTAINS:
			if !strings.Contains(lowerStrVal, lowerFilterVal) {

				logging.D(1, "Filtering out video '%s' which does not contain '%s' in field '%s'", dl.URL, filter.Value, filter.Field)
				if err := removeUnwantedJSON(dl.JSONPath); err != nil {
					logging.E(0, err.Error())
				}
				return false, nil
			}

		default:
			logging.D(1, "Unrecognized filter type, skipping...")
			continue
		}
	}
	logging.D(1, "Video '%s' passed filter checks", dl.URL)
	return true, nil
}

// removeUnwantedJSON removes filtered out JSON files
func removeUnwantedJSON(path string) error {
	if path == "" {
		return fmt.Errorf("path sent in empty, not removing")
	}

	check, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("not deleting unwanted JSON file, got error: %w", err)
	}

	switch {
	case check.IsDir():
		return fmt.Errorf("JSON path sent in as a directory '%s', not deleting", path)
	case !check.Mode().IsRegular():
		return fmt.Errorf("JSON file '%s' is not a regular file, not deleting", path)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove unwanted JSON file '%s' due to error %w", path, err)
	} else {
		logging.S(0, "Removed unwanted JSON file '%s'", path)
	}

	return nil
}
