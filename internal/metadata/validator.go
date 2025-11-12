// Package metadata handles video metadata parsing, validation, and filtering.
package metadata

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// ValidateAndFilter parses JSON, applies filters, and checks move operations.
//
// Returns true if the video passes all filters and JSON validity checks.
func ValidateAndFilter(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) (passed bool, useFilteredMetaOps []models.FilteredMetaOps, useFilteredFilenameOps []models.FilteredFilenameOps, err error) {
	logging.I("Validating and filtering JSON file %q...", v.JSONPath)
	// Parse and store JSON
	jsonValid, err := parseAndStoreJSON(v)
	if err != nil {
		logging.E("JSON parsing/storage failed for %q: %v", v.URL, err)
	}

	// Apply filters
	passedFilters, useFilteredMetaOps, useFilteredFilenameOps, err := handleFilters(v, cu, c, dirParser)
	if err != nil {
		logging.E("filter operation checks failed for %q: %v", v.URL, err)
	}
	if !jsonValid || !passedFilters {
		return false, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	// Check move operations
	v.MoveOpOutputDir = handleMoveOps(v, cu, dirParser)

	return true, useFilteredMetaOps, useFilteredFilenameOps, nil
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

	// Titles
	if v.Title == "" {
		for _, key := range []string{"fulltitle", "title", "full_title"} {
			if titleVal, exists := m[key]; exists {
				if title, ok := titleVal.(string); ok {
					v.Title = title
					logging.D(2, "Extracted title %q from metadata (Video URL: %q)", title, v.URL)
					break
				}
			}
		}
		if v.Title == "" && v.URL != "" {
			v.Title = v.URL
		}
	}

	// Upload date
	if v.UploadDate.IsZero() {
		for _, key := range []string{"upload_date", "release_date", "originally_available_at", "date"} {
			if uploadDateVal, exists := m[key]; exists {
				if uploadDate, ok := uploadDateVal.(string); ok {
					if strings.Contains(uploadDate, "-") {
						if t, err := time.Parse("2006-01-02", uploadDate); err == nil { // If error IS nil
							v.UploadDate = t
							if v.UploadDate.IsZero() {
								logging.E("Failed to parse upload date %q: %v", uploadDate, err)
							}
							break
						}
					} else if !strings.Contains(uploadDate, "-") {
						if t, err := time.Parse("20060102", uploadDate); err == nil { // If error IS nil
							v.UploadDate = t
							if v.UploadDate.IsZero() {
								logging.E("Failed to parse upload date %q: %v", uploadDate, err)
							}
							break
						}
					}
				}
			}
		}
	}

	// Description
	if v.Description == "" {
		for _, key := range []string{"description", "longdescription", "long_description", "summary", "synopsis"} {
			if descriptionVal, exists := m[key]; exists {
				if description, ok := descriptionVal.(string); ok {
					v.Description = description
					logging.D(2, "Extracted description %q from metadata (Video URL: %q)", description, v.URL)
					break
				}
			}
		}
	}

	// Thumbnails
	if v.ThumbnailURL == "" {
		// Check directly
		if thumbnailVal, exists := m[consts.MetadataThumbnail]; exists {
			if thumbnail, ok := thumbnailVal.(string); ok {
				v.ThumbnailURL = thumbnail
			}
		}
		// Still empty, check arrays
		if v.ThumbnailURL == "" {
			if thumbsVal, exists := m["thumbnails"]; exists {
				if thumbs, ok := thumbsVal.([]any); ok {
					var best string
					maxPref := math.MinInt
					for _, t := range thumbs {
						if thumbMap, ok := t.(map[string]any); ok {
							urlStr, _ := thumbMap["url"].(string)
							if urlStr == "" {
								continue
							}
							// Highest-resolution or best preference
							if pref, ok := thumbMap["preference"].(float64); ok {
								if int(pref) > maxPref {
									maxPref = int(pref)
									best = urlStr
								}
							} else if best == "" {
								// fallback if preference missing
								best = urlStr
							}
						}
					}
					if best != "" {
						v.ThumbnailURL = best
						logging.D(2, "Selected thumbnail from array: %s", best)
					}
				}
			}
		}
	}

	logging.D(1, "Successfully validated and stored metadata for video: %q (Title: %q)", v.URL, v.Title)
	return true, nil
}

// handleFilters uses user input filters to check if the video should be downloaded.
func handleFilters(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) (pass bool, useFilteredMetaOps []models.FilteredMetaOps, useFilteredFilenameOps []models.FilteredFilenameOps, err error) {
	// Check filtered meta ops
	filteredMetaOps := make([]models.FilteredMetaOps, len(cu.ChanURLMetarrArgs.FilteredMetaOps))
	copy(filteredMetaOps, cu.ChanURLMetarrArgs.FilteredMetaOps)

	filteredMetaOpsFileFilters := loadFilteredMetaOpsFromFile(v, cu, dirParser)
	filteredMetaOps = append(filteredMetaOps, filteredMetaOpsFileFilters...)

	relevantFilteredMetaOps := getRelevantFilteredMetaOps(filteredMetaOps, cu.URL)
	useFilteredMetaOps = filteredMetaOpsMatches(v, cu, relevantFilteredMetaOps, c.Name)

	// Check filtered filename ops
	filteredFilenameOps := make([]models.FilteredFilenameOps, len(cu.ChanURLMetarrArgs.FilteredFilenameOps))
	copy(filteredFilenameOps, cu.ChanURLMetarrArgs.FilteredFilenameOps)

	filteredFilenameOpsFileFilters := loadFilteredFilenameOpsFromFile(v, cu, dirParser)
	filteredFilenameOps = append(filteredFilenameOps, filteredFilenameOpsFileFilters...)

	relevantFilteredFilenameOps := getRelevantFilteredFilenameOps(filteredFilenameOps, cu.URL)
	useFilteredFilenameOps = filteredFilenameOpsMatches(v, cu, relevantFilteredFilenameOps, c.Name)

	// Check download filters
	allFilters := make([]models.Filters, len(cu.ChanURLSettings.Filters))
	copy(allFilters, cu.ChanURLSettings.Filters)

	fileFilters := loadFilterOpsFromFile(v, cu, dirParser)
	allFilters = append(allFilters, fileFilters...)

	relevantFilters := getRelevantFilters(allFilters, cu.URL)

	if !filterOpsFilter(v, relevantFilters, c.Name) {
		return false, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	// Check upload date filter
	passUploadDate, err := uploadDateFilter(v, cu, c.Name)
	if err != nil {
		return false, useFilteredMetaOps, useFilteredFilenameOps, err
	}
	if !passUploadDate {
		return false, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	logging.S("Video %q for channel %q passed all filter checks", v.URL, c.Name)
	return true, useFilteredMetaOps, useFilteredFilenameOps, nil
}

// getRelevantFilters returns filters applicable to the given URL.
func getRelevantFilters(filters []models.Filters, currentURL string) []models.Filters {
	relevant := make([]models.Filters, 0, len(filters))

	for _, filter := range filters {
		// Include if no specific URL specified, or if it matches current URL
		if filter.ChannelURL == "" ||
			strings.EqualFold(strings.TrimSpace(filter.ChannelURL), strings.TrimSpace(currentURL)) {
			relevant = append(relevant, filter)
		} else {
			logging.D(2, "Skipping filter %v. This filter's specific channel URL %q does not match current channel URL %q",
				filter, filter.ChannelURL, currentURL)
		}
	}

	return relevant
}

// getRelevantFilters returns filters applicable to the given URL.
func getRelevantFilteredMetaOps(filteredMetaOps []models.FilteredMetaOps, currentURL string) []models.FilteredMetaOps {
	relevantFilteredMetaOps := make([]models.FilteredMetaOps, 0, len(filteredMetaOps))

	for _, fmo := range filteredMetaOps {
		relevantFilters := make([]models.Filters, 0, len(fmo.Filters))

		for _, filter := range fmo.Filters {
			// Include if no specific URL specified, or if it matches current URL
			if filter.ChannelURL == "" ||
				strings.EqualFold(strings.TrimSpace(filter.ChannelURL), strings.TrimSpace(currentURL)) {
				relevantFilters = append(relevantFilters, filter)
			} else {
				logging.D(2, "Skipping filter %v. This filter's specific channel URL %q does not match current channel URL %q",
					filter, filter.ChannelURL, currentURL)
			}
		}

		fmo.Filters = relevantFilters
		relevantFilteredMetaOps = append(relevantFilteredMetaOps, fmo)
	}
	return relevantFilteredMetaOps
}

// getRelevantFilteredFilenameOps returns filtered filename ops applicable to the given URL.
func getRelevantFilteredFilenameOps(filteredFilenameOps []models.FilteredFilenameOps, currentURL string) []models.FilteredFilenameOps {
	relevantFilteredFilenameOps := make([]models.FilteredFilenameOps, 0, len(filteredFilenameOps))
	for _, ffo := range filteredFilenameOps {
		relevantFilters := make([]models.Filters, 0, len(ffo.Filters))
		for _, filter := range ffo.Filters {
			// Include if no specific URL specified, or if it matches current URL
			if filter.ChannelURL == "" ||
				strings.EqualFold(strings.TrimSpace(filter.ChannelURL), strings.TrimSpace(currentURL)) {
				relevantFilters = append(relevantFilters, filter)
			} else {
				logging.D(2, "Skipping filter %v. This filter's specific channel URL %q does not match current channel URL %q",
					filter, filter.ChannelURL, currentURL)
			}
		}
		ffo.Filters = relevantFilters
		relevantFilteredFilenameOps = append(relevantFilteredFilenameOps, ffo)
	}
	return relevantFilteredFilenameOps
}

// handleMoveOps checks if Metarr should use an output directory based on existent metadata.
func handleMoveOps(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) string {
	// Work with a copy of database move ops
	allMoveOps := make([]models.MetaFilterMoveOps, len(cu.ChanURLSettings.MoveOps))
	copy(allMoveOps, cu.ChanURLSettings.MoveOps)

	// Add file-based move ops (ephemeral - re-read each time)
	fileMoveOps := loadMoveOpsFromFile(v, cu, dirParser)
	allMoveOps = append(allMoveOps, fileMoveOps...)

	// Filter to relevant ones for this URL (non-mutating)
	relevantMoveOps := getRelevantMoveOps(allMoveOps, cu.URL)

	// Check each move operation against video metadata
	for _, op := range relevantMoveOps {
		if raw, exists := v.MetadataMap[op.Field]; exists {
			// Convert any type to string
			val := fmt.Sprint(raw)

			if strings.Contains(strings.ToLower(val), strings.ToLower(op.Value)) {
				logging.I("Move op matched: Field %q contains the value %q. Output directory retrieved as %q",
					op.Field, op.Value, op.OutputDir)
				return op.OutputDir
			}
		}
	}

	return ""
}

// getRelevantMoveOps returns move operations applicable to the given URL.
func getRelevantMoveOps(moveOps []models.MetaFilterMoveOps, currentURL string) []models.MetaFilterMoveOps {
	relevant := make([]models.MetaFilterMoveOps, 0, len(moveOps))

	for _, op := range moveOps {
		// Include if no specific URL specified, or if it matches current URL
		if op.ChannelURL == "" ||
			strings.EqualFold(strings.TrimSpace(op.ChannelURL), strings.TrimSpace(currentURL)) {
			relevant = append(relevant, op)
		} else {
			logging.D(2, "Skipping move op for different URL: %q != %q", op.ChannelURL, currentURL)
		}
	}

	return relevant
}
