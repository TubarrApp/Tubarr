package process

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"tubarr/internal/cfg"
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

	if dl.Metamap == nil {
		dl.Metamap = make(map[string]interface{})
	}

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

	// Check if filters are set
	if !cfg.IsSet(keys.FilterOps) {
		logging.D(1, "No filters set, returning true")
		return true, nil
	}

	// Get and validate filter configuration
	filters, ok := cfg.Get(keys.FilterOps).([]*models.DLFilter)
	if !ok || filters == nil {
		return false, fmt.Errorf("filter configuration is nil or has wrong type; expected []*models.DLFilter, got %T", filters)
	}

	// Apply filters if any
	for _, filter := range filters {
		val, exists := dl.Metamap[filter.Field]
		if !exists {
			continue
		}

		strVal, ok := val.(string)
		if !ok {
			logging.E(0, "Unexpected type for field %s: expected string, got %T", filter.Field, val)
			continue
		}

		// Apply the filter logic
		if strings.Contains(strings.ToLower(strVal), strings.ToLower(filter.Omit)) {
			logging.D(1, "Filtering out video '%s' which contains '%s' in field '%s'", dl.URL, filter.Omit, filter.Field)

			if err := os.Remove(dl.JSONPath); err != nil {
				logging.E(0, "Failed to remove unwanted JSON file '%s' due to error %v", dl.JSONPath, err)
			} else {
				logging.S(0, "Removed unwanted JSON file '%s'", dl.JSONPath)
			}
			return false, nil
		}
	}
	logging.D(1, "Video '%s' passed filter checks", dl.URL)
	return true, nil
}
