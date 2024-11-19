package process

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// validateAndStoreJSON checks if the JSON is valid and if it passes filter checks
func validateAndStoreJSON(v *models.Video) (valid bool, err error) {
	if v == nil {
		return false, fmt.Errorf("dl model is null")
	}

	if v.JPath == "" {
		return false, fmt.Errorf("json path cannot be empty")
	}

	jInfo, err := os.Stat(v.JPath)
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

	f, err := os.Open(v.JPath)
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
		v.MetadataMap = m

		// Extract title from metadata
		if title, ok := m["title"].(string); ok {
			v.Title = title
			logging.D(2, "Extracted title from metadata: %s", title)
		} else {
			logging.D(2, "No title found in metadata or invalid type")
		}

		// Extract upload date if available
		if uploadDate, ok := m["upload_date"].(string); ok {
			if t, err := time.Parse("20060102", uploadDate); err == nil { // If error IS nil
				v.UploadDate = t
				logging.D(2, "Extracted upload date: %s", t.Format("2006-01-02"))
			} else {
				logging.D(2, "Failed to parse upload date %q: %v", uploadDate, err)
			}
		}

		// Extract any additional metadata fields you want to store
		if description, ok := m["description"].(string); ok {
			v.Description = description
			logging.D(2, "Extracted description from metadata")
		}

	} else {
		return false, nil
	}

	if valid, err = filterRequests(v); err != nil {
		return false, err
	}
	if !valid {
		return false, nil
	}

	logging.D(1, "Successfully validated and stored metadata for video: %s (Title: %s)", v.URL, v.Title)
	return true, nil
}

// filterRequests uses user input filters to check if the video should be downloaded
func filterRequests(v *models.Video) (valid bool, err error) {
	// Check if filters are set and validate if so
	if len(v.Settings.Filters) == 0 {
		logging.D(2, "No filters to check for %q", v.URL)
		return true, nil
	}

	// Apply filters if any match metadata content
	for _, filter := range v.Settings.Filters {
		val, exists := v.MetadataMap[filter.Field]

		if filter.Value == "" {
			if !exists {
				switch filter.Type {
				case consts.FilterContains:

					logging.I("Filtering: Field %q not found in metadata for URL %q and filter is set to require it, filtering out", filter.Field, v.URL)
					if err := removeUnwantedJSON(v.JPath); err != nil {
						logging.E(0, err.Error())
					}
					return false, nil

				case consts.FilterOmit:
					logging.D(2, "Passed check: Field %q does not exist", filter.Field)
					continue
				}
			}
			if exists {
				switch filter.Type {
				case consts.FilterOmit:

					logging.I("Filtering: Field %q found in metadata for URL %q and filter is set to omit it, filtering out", filter.Field, v.URL)
					if err := removeUnwantedJSON(v.JPath); err != nil {
						logging.E(0, err.Error())
					}
					return false, nil

				case consts.FilterContains:
					logging.D(2, "Passed check: Field %q exists", filter.Field)
					continue
				}
			}
		}

		// Performing field contents checks
		if filter.Value != "" {

			strVal, ok := val.(string)
			if !ok {
				logging.E(0, "Unexpected type for field %s: expected string, got %T", filter.Field, val)
				continue
			}

			lowerStrVal := strings.ToLower(strVal)
			lowerFilterVal := strings.ToLower(filter.Value)

			// Apply the filter logic
			switch filter.Type {
			case consts.FilterOmit:
				if strings.Contains(lowerStrVal, lowerFilterVal) {

					logging.D(1, "Filtering out video %q which contains %q in field %q", v.URL, filter.Value, filter.Field)
					if err := removeUnwantedJSON(v.JPath); err != nil {
						logging.E(0, err.Error())
					}
					return false, nil
				}

			case consts.FilterContains:
				if !strings.Contains(lowerStrVal, lowerFilterVal) {

					logging.D(1, "Filtering out video %q which does not contain %q in field %q", v.URL, filter.Value, filter.Field)
					if err := removeUnwantedJSON(v.JPath); err != nil {
						logging.E(0, err.Error())
					}
					return false, nil
				}

			default:
				logging.D(1, "Unrecognized filter type, skipping...")
				continue
			}
		}
	}
	logging.D(1, "Video %q passed filter checks", v.URL)
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
		return fmt.Errorf("JSON path sent in as a directory %q, not deleting", path)
	case !check.Mode().IsRegular():
		return fmt.Errorf("JSON file %q is not a regular file, not deleting", path)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove unwanted JSON file %q due to error %w", path, err)
	} else {
		logging.S(0, "Removed unwanted JSON file %q", path)
	}

	return nil
}

// isPrivateNetwork returns true if the URL is detected as a LAN network
func isPrivateNetwork(host string) bool {
	return strings.HasPrefix(host, "192.168") ||
		strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "172.16.") || // 172.16.0.0 - 172.31.255.255
		strings.HasPrefix(host, "127.") || // localhost
		host == "localhost"
}
