// Package metadata handles video metadata parsing, validation, and filtering.
package metadata

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// ValidateAndFilter parses JSON, applies filters, and checks move operations.
//
// Returns true if the video passes all filters and JSON validity checks.
func ValidateAndFilter(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) (passed bool, err error) {
	// Parse and store JSON
	jsonValid, err := parseAndStoreJSON(v)
	if err != nil {
		logging.E("JSON parsing/storage failed for %q: %v", v.URL, err)
	}

	// Apply filters
	passedFilters, err := handleFilters(v, cu, c, dirParser)
	if err != nil {
		logging.E("filter operation checks failed for %q: %v", v.URL, err)
	}
	if !jsonValid || !passedFilters {
		return false, nil
	}

	// Check move operations
	cu.MoveOpOutputDir, cu.MoveOpChannelURL = handleMoveOps(v, cu, dirParser)

	return true, nil
}

// parseAndStoreJSON checks if the JSON is valid and if it passes filter checks.
func parseAndStoreJSON(v *models.Video) (valid bool, err error) {
	f, err := os.Open(v.JSONPath)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			logging.E("Failed to close file at %q", v.JSONPath)
		}
	}()

	logging.D(1, "About to decode JSON to metamap")

	m := make(map[string]any)
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&m); err != nil {
		return false, fmt.Errorf("failed to decode JSON: %w", err)
	}

	if len(m) == 0 {
		return false, nil
	}

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

	// Extract description
	if description, ok := m["description"].(string); ok {
		v.Description = description
		logging.D(2, "Extracted description from metadata")
	}

	logging.D(1, "Successfully validated and stored metadata for video: %s (Title: %s)", v.URL, v.Title)
	return true, nil
}

// handleFilters uses user input filters to check if the video should be downloaded.
func handleFilters(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) (valid bool, err error) {

	// Load filter ops from file if present
	cu.ChanURLSettings.Filters = append(cu.ChanURLSettings.Filters, loadFilterOpsFromFile(v, cu, dirParser)...)

	// Filter out irrelevant filter operations
	cu.SetRelevantFilterOps()

	// Check filter ops
	if passFilterOps := filterOpsFilter(v, cu); !passFilterOps {
		return false, nil
	}

	// Upload date filter
	passUploadDate, err := uploadDateFilter(v, cu)
	if err != nil {
		return false, err
	}
	if !passUploadDate {
		return false, nil
	}

	logging.I("Video %q for channel %q passed filter checks", v.URL, c.Name)
	return true, nil
}

// handleMoveOps checks if Metarr should use an output directory based on existent metadata.
func handleMoveOps(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) (outputDir string, channelURL string) {
	// Load move ops from file if present
	cu.ChanURLSettings.MoveOps = append(cu.ChanURLSettings.MoveOps, loadMoveOpsFromFile(v, cu, dirParser)...)

	// Filter out irrelevant move operations
	cu.SetRelevantMoveOps()

	for _, op := range cu.ChanURLSettings.MoveOps {
		if raw, exists := v.MetadataMap[op.Field]; exists {
			// Convert any type to string
			val := fmt.Sprint(raw)

			if strings.Contains(strings.ToLower(val), strings.ToLower(op.Value)) {
				logging.I("Move op filters matched: Field %q contains the value %q. Output directory retrieved as %q", op.Field, op.Value, op.OutputDir)
				return op.OutputDir, op.ChannelURL
			}
		}
	}
	return "", ""
}
