// Package metadata handles video metadata parsing, validation, and filtering.
package metadata

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedtags"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// ValidateAndFilter parses JSON, applies filters, and checks move operations.
//
// Returns true if the video passes all filters and JSON validity checks.
func ValidateAndFilter(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) (passed bool, useFilteredMetaOps []models.FilteredMetaOps, useFilteredFilenameOps []models.FilteredFilenameOps, err error) {
	logger.Pl.I("Validating and filtering JSON file %q...", v.JSONFilePath)
	// Parse and store JSON.
	jsonValid, err := parseAndStoreJSON(v)
	if err != nil {
		logger.Pl.E("JSON parsing/storage failed for %q: %v", v.URL, err)
	}

	// Apply filters.
	passedFilters, useFilteredMetaOps, useFilteredFilenameOps, err := handleFilters(v, cu, c, dirParser)
	if err != nil {
		if errors.Is(err, errFilterFile) {
			// An error rather than a failed check, so the video is skipped but not marked ignored, and is tried again once the file is fixed.
			return false, useFilteredMetaOps, useFilteredFilenameOps, fmt.Errorf("skipping %q: %w", v.URL, err)
		}
		logger.Pl.E("filter operation checks failed for %q: %v", v.URL, err)
	}

	if !jsonValid || !passedFilters {
		return false, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	return true, useFilteredMetaOps, useFilteredFilenameOps, nil
}

// RefreshFromMetarr reloads v's metadata from the file Metarr finished with, taking the title, description and date it
// has over the stored ones, as Metarr's meta ops may have changed them.
func RefreshFromMetarr(v *models.Video) error {
	m, err := readJSONFile(v.JSONFilePath)
	if err != nil {
		return err
	}
	v.MetadataMap = m
	v.Title = cmp.Or(sharedtags.FirstString(m, sharedtags.TitleKeys), v.Title)
	v.Description = cmp.Or(sharedtags.FirstString(m, sharedtags.DescriptionKeys), v.Description)
	if d := metadataDate(m); !d.IsZero() {
		v.UploadDate = d
	}
	return nil
}

// readJSONFile decodes a JSON metadata file.
func readJSONFile(path string) (map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Pl.E("Failed to close file at %q", path)
		}
	}()

	var m map[string]any
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to decode JSON %q: %w", path, err)
	}
	return m, nil
}

// metadataDate returns the first date in m by sharedtags.DateKeys, as YYYY-MM-DD or YYYYMMDD with any time after it
// ignored, or the zero time if none parses.
func metadataDate(m map[string]any) time.Time {
	for _, k := range sharedtags.DateKeys {
		s, ok := m[k].(string)
		if !ok {
			continue
		}
		s, _, _ = strings.Cut(s, "T")
		for _, layout := range []string{"2006-01-02", "20060102"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// parseAndStoreJSON checks if the JSON is valid and if it passes filter checks.
func parseAndStoreJSON(v *models.Video) (valid bool, err error) {
	m, err := readJSONFile(v.JSONFilePath)
	if err != nil {
		return false, err
	}
	if len(m) == 0 {
		return false, nil
	}
	v.MetadataMap = m

	// Title, date and description, by the precedence Metarr embeds them with.
	if v.Title == "" {
		v.Title = cmp.Or(sharedtags.FirstString(m, sharedtags.TitleKeys), v.URL)
	}
	if v.UploadDate.IsZero() {
		v.UploadDate = metadataDate(m)
	}
	if v.Description == "" {
		v.Description = sharedtags.FirstString(m, sharedtags.DescriptionKeys)
	}

	// Thumbnails.
	if v.ThumbnailURL == "" {
		// Check directly.
		if thumbnailVal, exists := m[sharedtags.JThumbnailURL]; exists {
			if thumbnail, ok := thumbnailVal.(string); ok {
				v.ThumbnailURL = thumbnail
			}
		}
		// Still empty, check arrays.
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
							// Highest-resolution or best preference.
							if pref, ok := thumbMap["preference"].(float64); ok {
								if int(pref) > maxPref {
									maxPref = int(pref)
									best = urlStr
								}
							} else if best == "" {
								// fallback if preference missing.
								best = urlStr
							}
						}
					}
					if best != "" {
						v.ThumbnailURL = best
						logger.Pl.D(2, "Selected thumbnail from array: %s", best)
					}
				}
			}
		}
	}

	logger.Pl.D(1, "Successfully validated and stored metadata for video: %q (Title: %q)", v.URL, v.Title)
	return true, nil
}

// handleFilters uses user input filters to check if the video should be downloaded.
func handleFilters(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) (pass bool, useFilteredMetaOps []models.FilteredMetaOps, useFilteredFilenameOps []models.FilteredFilenameOps, err error) {
	filteredMetaOps, filteredFilenameOps := loadFilteredOps(v, cu, dirParser)

	// Check filtered meta ops.
	relevantFilteredMetaOps := getRelevantFilteredMetaOps(filteredMetaOps, cu.URL)
	useFilteredMetaOps = filteredMetaOpsMatches(v, cu, relevantFilteredMetaOps, c.Name)

	// Check filtered filename ops.

	relevantFilteredFilenameOps := getRelevantFilteredFilenameOps(filteredFilenameOps, cu.URL)
	useFilteredFilenameOps = filteredFilenameOpsMatches(v, cu, relevantFilteredFilenameOps, c.Name)

	// Videos the user chose skip download filters.
	if v.SkipFilters || cu.IsManual {
		logger.Pl.I("Skipped download filters for %q in channel %q: Video was chosen manually.", v.URL, c.Name)
		return true, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	// Check download filters.
	fileFilters, err := LoadFilterOpsFromFile(cu, dirParser)
	if err != nil {
		return false, useFilteredMetaOps, useFilteredFilenameOps, err
	}
	allFilters := slices.Concat(cu.ChanURLSettings.Filters, fileFilters)
	relevantFilters := getRelevantFilters(allFilters, cu.URL)

	if !checkFilters(v, "Download filters", relevantFilters) {
		return false, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	// Check upload date filter.
	passUploadDate, err := uploadDateFilter(v, cu, c.Name)
	if err != nil {
		return false, useFilteredMetaOps, useFilteredFilenameOps, err
	}
	if !passUploadDate {
		return false, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	logger.Pl.S("Video %q for channel %q passed all filter checks", v.URL, c.Name)
	return true, useFilteredMetaOps, useFilteredFilenameOps, nil
}

// loadFilteredOps returns the video's Download As filtered ops if set, else the channel URL's and its files'.
func loadFilteredOps(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) ([]models.FilteredMetaOps, []models.FilteredFilenameOps) {
	if v.Overrides != nil {
		return v.Overrides.FilteredMetaOps, v.Overrides.FilteredFilenameOps
	}

	// An unusable file holds back Metarr for the whole batch, see app.metarrSkip.
	fileFilteredMetaOps, fileErr := LoadFilteredMetaOpsFromFile(cu, dirParser)
	if fileErr != nil {
		logger.Pl.E("Filtered meta ops file for %q can't be used: %v", cu.URL, fileErr)
	}
	fileFilteredFilenameOps, fileErr := LoadFilteredFilenameOpsFromFile(cu, dirParser)
	if fileErr != nil {
		logger.Pl.E("Filtered filename ops file for %q can't be used: %v", cu.URL, fileErr)
	}
	return slices.Concat(cu.ChanURLMetarrArgs.FilteredMetaOps, fileFilteredMetaOps),
		slices.Concat(cu.ChanURLMetarrArgs.FilteredFilenameOps, fileFilteredFilenameOps)
}

// getRelevantFilters returns filters applicable to the given URL.
func getRelevantFilters(filters []models.Filters, currentURL string) []models.Filters {
	relevant := make([]models.Filters, 0, len(filters))

	for _, filter := range filters {
		// Include if no specific URL specified, or if it matches current URL.
		if sharedparsing.AppliesToChannel(filter.ChannelURL, currentURL) {
			relevant = append(relevant, filter)
		} else {
			logger.Pl.D(2, "Skipping filter %v. This filter's specific channel URL %q does not match current channel URL %q",
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
			// Include if no specific URL specified, or if it matches current URL.
			if sharedparsing.AppliesToChannel(filter.ChannelURL, currentURL) {
				relevantFilters = append(relevantFilters, filter)
			} else {
				logger.Pl.D(2, "Skipping filter %v. This filter's specific channel URL %q does not match current channel URL %q",
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
			// Include if no specific URL specified, or if it matches current URL.
			if sharedparsing.AppliesToChannel(filter.ChannelURL, currentURL) {
				relevantFilters = append(relevantFilters, filter)
			} else {
				logger.Pl.D(2, "Skipping filter %v. This filter's specific channel URL %q does not match current channel URL %q",
					filter, filter.ChannelURL, currentURL)
			}
		}
		ffo.Filters = relevantFilters
		relevantFilteredFilenameOps = append(relevantFilteredFilenameOps, ffo)
	}
	return relevantFilteredFilenameOps
}

// HandleMoveOps checks if Metarr should use an output directory based on existent metadata.
func HandleMoveOps(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) string {
	// Copy of the database move ops, plus file-based move ops.
	allMoveOps := slices.Concat(cu.ChanURLSettings.MetaFilterMoveOps, loadMoveOpsFromFile(cu, dirParser))

	// Filter to relevant ones for this URL.
	relevantMoveOps := getRelevantMoveOps(allMoveOps, cu.URL)

	// Check each move operation against video metadata.
	for _, op := range relevantMoveOps {
		if raw, exists := v.MetadataMap[op.Field]; exists {
			// Convert any type to string.
			val := fmt.Sprint(raw)

			if strings.Contains(strings.ToLower(val), strings.ToLower(op.ContainsValue)) {
				logger.Pl.I("Move op matched: Field %q contains the value %q. Output directory retrieved as %q",
					op.Field, op.ContainsValue, op.OutputDir)
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
		// Include if no specific URL specified, or if it matches current URL.
		if sharedparsing.AppliesToChannel(op.ChannelURL, currentURL) {
			relevant = append(relevant, op)
		} else {
			logger.Pl.D(2, "Skipping move op for different URL: %q != %q", op.ChannelURL, currentURL)
		}
	}

	return relevant
}
