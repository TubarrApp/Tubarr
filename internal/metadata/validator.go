// Package metadata handles video metadata parsing, validation, and filtering.
package metadata

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedfiles"
	"github.com/TubarrApp/gocommon/sharedfilters"
	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/TubarrApp/gocommon/sharedtags"
)

// ValidateAndFilter reads the video's metadata and checks its download filters, returning whether it passed and its filtered ops.
func ValidateAndFilter(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) (passed bool, useFilteredMetaOps []models.FilteredMetaOps, useFilteredFilenameOps []models.FilteredFilenameOps, err error) {
	cu.Log().D(2, "Validating and filtering JSON file %q...", v.Files.JSONFilePath)

	// A metadata file that can't be read is an error rather than a failed check, so the video isn't marked ignored.
	if err := parseAndStoreJSON(v); err != nil {
		return false, nil, nil, fmt.Errorf("metadata can't be used: %w", err)
	}

	// Apply filters. A filter or filter file that can't be used is an error too, so the video is tried again once it's fixed.
	if passed, useFilteredMetaOps, useFilteredFilenameOps, err = handleFilters(v, cu, dirParser); err != nil {
		return false, useFilteredMetaOps, useFilteredFilenameOps, fmt.Errorf("skipping %q: %w", v.URL, err)
	}
	return passed, useFilteredMetaOps, useFilteredFilenameOps, nil
}

// RefreshFromMetarr reloads v's metadata from the file Metarr finished with, taking the title, description and date it
// has over the stored ones, as Metarr's meta ops may have changed them.
func RefreshFromMetarr(v *models.Video) error {
	m, err := readJSONFile(v.Files.JSONFilePath)
	if err != nil {
		return err
	}
	v.Metadata.Raw = m
	v.Metadata.Title = cmp.Or(sharedtags.FirstString(m, sharedtags.TitleKeys), v.Metadata.Title)
	v.Metadata.Description = cmp.Or(sharedtags.FirstString(m, sharedtags.DescriptionKeys), v.Metadata.Description)
	if d := metadataDate(m); !d.IsZero() {
		v.Metadata.UploadDate = d
	}
	return nil
}

// readJSONFile decodes a JSON metadata file.
func readJSONFile(path string) (map[string]any, error) {
	f, err := sharedfiles.OpenRegularInRoot(path)
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

// metadataDuration returns the duration in m in seconds, from duration or else duration_string, or 0 if neither parses.
func metadataDuration(m map[string]any) int {
	switch d := m[sharedtags.JDuration].(type) {
	case float64:
		if d > 0 {
			return int(d)
		}
	case string:
		if secs, _ := sharedparsing.ParseDuration(d); secs > 0 {
			return secs
		}
	}
	s, _ := m[sharedtags.JDurationString].(string)

	// Parse and return.
	parsed, _ := sharedparsing.ParseDuration(s)
	return parsed
}

// parseAndStoreJSON reads the video's metadata file, storing its title, date, description, duration and thumbnail.
func parseAndStoreJSON(v *models.Video) error {
	m, err := readJSONFile(v.Files.JSONFilePath)
	if err != nil {
		return err
	}
	if len(m) == 0 {
		return fmt.Errorf("metadata file %q has no fields", v.Files.JSONFilePath)
	}
	v.Metadata.Raw = m

	// Title, date and description, by the precedence Metarr embeds them with.
	if v.Metadata.Title == "" {
		v.Metadata.Title = cmp.Or(sharedtags.FirstString(m, sharedtags.TitleKeys), v.URL)
	}
	if v.Metadata.UploadDate.IsZero() {
		v.Metadata.UploadDate = metadataDate(m)
	}
	if v.Metadata.Description == "" {
		v.Metadata.Description = sharedtags.FirstString(m, sharedtags.DescriptionKeys)
	}

	// The video's own duration replaces the crawl listing's.
	if secs := metadataDuration(m); secs > 0 {
		v.Metadata.Duration = secs
	}

	// Select thumbnail
	bestThumbnail(v, m)

	v.Log().D(2, "Successfully validated and stored metadata for video: %q (Title: %q)", v.URL, v.Metadata.Title)
	return nil
}

// bestThumbnail sets the video's thumbnail URL from its metadata, if it has no thumbnail URL already set.
func bestThumbnail(v *models.Video, m map[string]any) {
	// Already got thumbnail URL.
	if v.Metadata.ThumbnailURL != "" {
		return
	}

	// Check the single thumbnail field.
	if thumbnail, _ := m[sharedtags.JThumbnail].(string); thumbnail != "" {
		v.Metadata.ThumbnailURL = thumbnail
		return
	}

	// Otherwise pick from the thumbnails list.
	thumbs, _ := m[sharedtags.JThumbnails].([]any)
	var best string
	maxPref := math.MinInt
	for _, t := range thumbs {
		thumbMap, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// Skip empty URL values.
		urlStr, _ := thumbMap[sharedtags.JURL].(string)
		if urlStr == "" {
			continue
		}
		// Highest preference wins.
		if pref, ok := thumbMap[sharedtags.JPreference].(float64); ok {
			if int(pref) > maxPref {
				maxPref = int(pref)
				best = urlStr
			}
		} else if best == "" {
			// Entries without a preference are only used if none have one.
			best = urlStr
		}
	}
	if best != "" {
		v.Metadata.ThumbnailURL = best
		v.Log().D(3, "Selected thumbnail from array: %s", best)
	}
}

// handleFilters uses user input filters to check if the video should be downloaded.
func handleFilters(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) (pass bool, useFilteredMetaOps []models.FilteredMetaOps, useFilteredFilenameOps []models.FilteredFilenameOps, err error) {
	filteredMetaOps, filteredFilenameOps := loadFilteredOps(v, cu, dirParser)

	// Check filtered meta ops.
	useFilteredMetaOps = filteredMetaOpsMatches(v, cu, filteredMetaOps)

	// Check filtered filename ops.
	useFilteredFilenameOps = filteredFilenameOpsMatches(v, cu, filteredFilenameOps)

	// Videos the user chose skip download filters.
	if v.SkipFilters || cu.IsManual {
		cu.Log().I("Skipped download filters for %q in channel %q: Video was chosen manually.", v.URL, v.ChannelName)
		return true, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	// Check download filters.
	allFilters, err := ChannelURLFilters(cu, dirParser)
	if err != nil {
		return false, useFilteredMetaOps, useFilteredFilenameOps, err
	}

	if pass, err = checkFilters(v, allFilters); err != nil || !pass {
		return false, useFilteredMetaOps, useFilteredFilenameOps, err
	}

	// Check upload date filter.
	passUploadDate, err := uploadDateFilter(v, cu)
	if err != nil {
		return false, useFilteredMetaOps, useFilteredFilenameOps, err
	}
	if !passUploadDate {
		return false, useFilteredMetaOps, useFilteredFilenameOps, nil
	}

	cu.Log().D(2, "Video %q for channel %q passed all filter checks", v.URL, v.ChannelName)
	return true, useFilteredMetaOps, useFilteredFilenameOps, nil
}

// loadFilteredOps returns the video's Download As filtered ops if set, as one level, else the channel URL's from every
// level, farthest first.
func loadFilteredOps(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) ([][]models.FilteredMetaOps, [][]models.FilteredFilenameOps) {
	if v.Overrides != nil {
		return [][]models.FilteredMetaOps{v.Overrides.FilteredMetaOps}, [][]models.FilteredFilenameOps{v.Overrides.FilteredFilenameOps}
	}

	// An unusable file holds back Metarr for the whole batch, see metarr.SkipReason.
	filteredMetaOps, fileErr := ChannelURLFilteredMetaOps(cu, dirParser)
	if fileErr != nil {
		cu.Log().E("Filtered meta ops file for %q can't be used: %v", cu.URL, fileErr)
	}
	filteredFilenameOps, fileErr := ChannelURLFilteredFilenameOps(cu, dirParser)
	if fileErr != nil {
		cu.Log().E("Filtered filename ops file for %q can't be used: %v", cu.URL, fileErr)
	}
	return filteredMetaOps, filteredFilenameOps
}

// HandleMoveOps checks if Metarr should use an output directory based on existent metadata.
func HandleMoveOps(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) string {
	allMoveOps := channelURLMoveOps(cu, dirParser)

	// Check each move operation against video metadata.
	for _, op := range allMoveOps {
		filters := sharedparsing.FormatFilters(op.Filters)
		matched, err := sharedfilters.MatchAll(v.Metadata.Raw, op.Filters)
		if err != nil {
			cu.Log().E("Skipping move op %q: %v", filters, err)
			continue
		}
		if !matched {
			continue
		}

		// Fill the output directory tags, skipping the op if one has no directory to fill from.
		outputDir, allFilled := parsing.FillOutputDirectoryTags(op.OutputDir, cu, c)
		if !allFilled {
			cu.Log().W("Skipping move op %q: %q uses an output directory tag with no directory to fill it from", filters, op.OutputDir)
			continue
		}
		cu.Log().I("Move op matched: %q. Output directory retrieved as %q", filters, outputDir)
		return outputDir
	}

	return ""
}
