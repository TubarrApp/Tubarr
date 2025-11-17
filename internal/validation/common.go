// Package validation handles validation of user flag input.
package validation

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/regex"
	"tubarr/internal/domain/templates"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ValidateMetarrOutputDirs validates Metarr output directory mappings.
//
// This function validates the directory map structure and ensures all directories exist.
func ValidateMetarrOutputDirs(outDirMap map[string]string) error {
	if len(outDirMap) == 0 {
		return nil
	}

	logger.Pl.D(1, "Validating %d Metarr output directories...", len(outDirMap))

	validatedDirs := make(map[string]bool, len(outDirMap))

	// Validate directories
	for urlStr, dir := range outDirMap {
		// Validate URL format
		if _, err := url.Parse(urlStr); err != nil {
			return fmt.Errorf("output directory map has invalid URL %q: %w", urlStr, err)
		}

		// Validate directory exists (only check once per unique directory)
		if !validatedDirs[dir] {
			if _, err := ValidateDirectory(dir, false); err != nil {
				return fmt.Errorf("output directory %q is invalid: %w", dir, err)
			}
			validatedDirs[dir] = true
		}
	}

	return nil
}

// ValidateDirectory validates that the directory exists, else creates it if desired.
func ValidateDirectory(dir string, createIfNotFound bool) (os.FileInfo, error) {
	possibleTemplate := strings.Contains(dir, "{{") && strings.Contains(dir, "}}")
	logger.Pl.D(3, "Statting directory %q. Templating detected? %v...", dir, possibleTemplate)

	// Handle templated directories
	if possibleTemplate {
		if !checkTemplateTags(dir) {
			t := make([]string, 0, len(templates.TemplateMap))
			for k := range templates.TemplateMap {
				t = append(t, k)
			}
			return nil, fmt.Errorf("directory contains unsupported template tags. Supported tags: %v", t)
		}
		logger.Pl.D(3, "Directory %q appears to contain templating elements, will not stat", dir)
		return nil, nil // templates are valid, no need to stat
	}

	return sharedvalidation.ValidateDirectory(dir, createIfNotFound)
}

// ValidateFile validates that the file exists, else creates it if desired.
func ValidateFile(f string, createIfNotFound bool) (os.FileInfo, error) {
	possibleTemplate := strings.Contains(f, "{{") && strings.Contains(f, "}}")
	logger.Pl.D(3, "Statting file %q...", f)

	// Handle templated directories
	if possibleTemplate {
		if !checkTemplateTags(f) {
			t := make([]string, 0, len(templates.TemplateMap))
			for k := range templates.TemplateMap {
				t = append(t, k)
			}
			return nil, fmt.Errorf("directory contains unsupported template tags. Supported tags: %v", t)
		}
		logger.Pl.D(3, "Directory %q appears to contain templating elements, will not stat", f)
		return nil, nil // templates are valid, no need to stat
	}

	return sharedvalidation.ValidateFile(f, createIfNotFound)
}

// ValidateViperFlags verifies that the user input flags are valid, modifying them to defaults or returning bools/errors.
func ValidateViperFlags() error {
	// Meta purge
	if abstractions.IsSet(keys.MMetaPurge) {
		purge := abstractions.GetString(keys.MMetaPurge)
		if purge != "" && !ValidatePurgeMetafiles(purge) {
			return fmt.Errorf("invalid meta purge type %q", purge)
		}
	}

	// Logging
	ValidateLoggingLevel()
	abstractions.Set(keys.GlobalConcurrency, sharedvalidation.ValidateConcurrencyLimit(abstractions.GetInt(keys.GlobalConcurrency)))
	return nil
}

// ValidateNotifications validates notification models.
func ValidateNotifications(notifications []*models.Notification) error {
	if len(notifications) == 0 {
		return nil
	}

	logger.Pl.D(1, "Validating %d notifications...", len(notifications))

	for i, n := range notifications {
		// Validate notify URL is not empty
		if n.NotifyURL == "" {
			return fmt.Errorf("notification at position %d has empty notify URL", i)
		}

		// Validate notify URL format
		if _, err := url.Parse(n.NotifyURL); err != nil {
			if u, err := url.Parse(n.NotifyURL); err != nil {
				return fmt.Errorf("notify URL %q is not valid", u)
			}
			return fmt.Errorf("notification at position %d has invalid notify URL %q: %w", i, n.NotifyURL, err)
		}
	}
	return nil
}

// ValidateYtdlpOutputExtension validates the merge-output-format compatibility of the inputted extension.
func ValidateYtdlpOutputExtension(e string) error {
	if e == "" {
		return nil
	}
	e = strings.TrimSpace(e)
	e = strings.TrimPrefix(e, ".")
	e = strings.ToLower(e)

	if !slices.Contains([]string{
		"avi",
		"flv",
		"mkv",
		"mov",
		"mp4",
		"webm",
	}, e) {
		return fmt.Errorf("output extension %v is invalid or not supported", e)
	}

	return nil
}

// ValidateLoggingLevel checks and validates the debug level.
func ValidateLoggingLevel() {
	logging.Level = min(max(abstractions.GetInt(keys.DebugLevel), 0), 5)
}

// ValidateMaxFilesize checks that max filesize value is valid for yt-dlp.
func ValidateMaxFilesize(input string) (string, error) {
	if input == "" {
		return "", nil
	}

	s := strings.ToLower(strings.TrimSpace(input))
	s = strings.TrimSuffix(s, "b")

	// Handle K, M, G suffixes
	if strings.HasSuffix(s, "k") || strings.HasSuffix(s, "m") || strings.HasSuffix(s, "g") {
		n := s[:len(s)-1]
		if _, err := strconv.Atoi(n); err != nil {
			return "", fmt.Errorf("invalid size number: %s", s)
		}
		return s, nil
	}

	// Check raw integer is valid
	if _, err := strconv.Atoi(s); err == nil {
		return s, nil
	}

	return "", fmt.Errorf("invalid max filesize format: %s", input)
}

// ValidateFilterOps validates filter operation models.
func ValidateFilterOps(filters []models.Filters) error {
	if len(filters) == 0 {
		return nil
	}

	logger.Pl.D(1, "Validating %d filter operations...", len(filters))

	for i, filter := range filters {
		// Validate contains/omits
		if filter.ContainsOmits != "contains" && filter.ContainsOmits != "omits" {
			return fmt.Errorf("filter at position %d has invalid type %q (must be 'contains' or 'omits')", i, filter.ContainsOmits)
		}

		// Validate must/any
		if filter.MustAny != "must" && filter.MustAny != "any" {
			return fmt.Errorf("filter at position %d has invalid condition %q (must be 'must' or 'any')", i, filter.MustAny)
		}

		// Validate field is not empty
		if filter.Field == "" {
			return fmt.Errorf("filter at position %d has empty field", i)
		}
	}
	return nil
}

// ValidateMetaFilterMoveOps validates meta filter move operation models.
func ValidateMetaFilterMoveOps(moveOps []models.MetaFilterMoveOps) error {
	if len(moveOps) == 0 {
		return nil
	}
	logger.Pl.D(1, "Validating %d meta filter move operations...", len(moveOps))

	for i, op := range moveOps {
		// Validate field is not empty
		if op.Field == "" {
			return fmt.Errorf("move operation at position %d has empty field", i)
		}

		if op.OutputDir != "" {
			if _, err := ValidateDirectory(op.OutputDir, false); err != nil {
				return err
			}
		}

		// Validate output directory exists
		if _, err := ValidateDirectory(op.OutputDir, false); err != nil {
			return fmt.Errorf("move operation at position %d has invalid directory: %w", i, err)
		}
	}
	return nil
}

// ValidateFilteredMetaOps validates filtered meta operation models.
func ValidateFilteredMetaOps(filteredMetaOps []models.FilteredMetaOps) error {
	if len(filteredMetaOps) == 0 {
		return nil
	}

	logger.Pl.D(1, "Validating %d filtered meta operations...", len(filteredMetaOps))

	for i, fmo := range filteredMetaOps {
		// Validate filters
		if err := ValidateFilterOps(fmo.Filters); err != nil {
			return fmt.Errorf("filtered meta operation at position %d has invalid filters: %w", i, err)
		}

		// Validate meta operations
		if err := ValidateMetaOps(fmo.MetaOps); err != nil {
			return fmt.Errorf("filtered meta operation at position %d has invalid meta operations: %w", i, err)
		}

		// Ensure both filters and meta ops are present
		if len(fmo.Filters) == 0 {
			return fmt.Errorf("filtered meta operation at position %d has no filters", i)
		}
		if len(fmo.MetaOps) == 0 {
			return fmt.Errorf("filtered meta operation at position %d has no meta operations", i)
		}
	}
	return nil
}

// ValidateFilteredFilenameOps validates filtered filename operation models.
func ValidateFilteredFilenameOps(filteredFilenameOps []models.FilteredFilenameOps) error {
	if len(filteredFilenameOps) == 0 {
		return nil
	}

	logger.Pl.D(1, "Validating %d filtered filename operations...", len(filteredFilenameOps))

	for i, ffo := range filteredFilenameOps {
		// Validate filters
		if err := ValidateFilterOps(ffo.Filters); err != nil {
			return fmt.Errorf("filtered filename operation at position %d has invalid filters: %w", i, err)
		}

		// Validate filename operations
		if err := ValidateFilenameOps(ffo.FilenameOps); err != nil {
			return fmt.Errorf("filtered filename operation at position %d has invalid filename operations: %w", i, err)
		}

		// Ensure both filters and filename ops are present
		if len(ffo.Filters) == 0 {
			return fmt.Errorf("filtered filename operation at position %d has no filters", i)
		}
		if len(ffo.FilenameOps) == 0 {
			return fmt.Errorf("filtered filename operation at position %d has no filename operations", i)
		}
	}
	return nil
}

// ValidateToFromDate validates a date string in yyyymmdd or formatted like "2025y12m31d".
func ValidateToFromDate(d string) (string, error) {
	if d == "" {
		return "", nil
	}

	d = strings.ToLower(d)
	d = strings.ReplaceAll(d, "-", "")
	d = strings.ReplaceAll(d, " ", "")

	// Handle "today" special case
	if d == "today" {
		return time.Now().Format("20060102"), nil
	}

	// Regex to extract explicitly marked years, months, and days
	re := regex.YearFragmentsCompile()
	matches := re.FindStringSubmatch(d)
	if matches == nil {
		return "", fmt.Errorf("invalid date format %q: expected 'Ymd' format", d)
	}

	// Default values
	year := strconv.Itoa(time.Now().Year())
	month := "01"
	day := "01"

	// Year
	if matches[1] != "" {
		year = matches[1]
	} else if len(d) == 8 && !strings.ContainsAny(d, "ymd") { // No markers, assume raw format
		year, month, day = d[:4], d[4:6], d[6:8]
	}

	// Month
	if matches[2] != "" {
		if len(matches[2]) == 1 {
			month = "0" + matches[2] // Pad single-digit months
		} else {
			month = matches[2]
		}
	}

	// Day
	if matches[3] != "" {
		if len(matches[3]) == 1 {
			day = "0" + matches[3] // Pad single-digit days
		} else {
			day = matches[3]
		}
	}

	// Convert to integers
	yearInt, err := strconv.Atoi(year)
	if err != nil {
		return "", fmt.Errorf("unable to convert year %q", year)
	}
	monthInt, err := strconv.Atoi(month)
	if err != nil {
		return "", fmt.Errorf("unable to convert month %q", month)
	}
	dayInt, err := strconv.Atoi(day)
	if err != nil {
		return "", fmt.Errorf("unable to convert day %q", day)
	}

	// Check validity
	if yearInt < 1000 || yearInt > 9999 {
		return "", fmt.Errorf("invalid year in yyyy-mm-dd date %q: year must be 4 digits", d)
	}
	if monthInt < 1 || monthInt > 12 {
		return "", fmt.Errorf("invalid month in yyyy-mm-dd date %q: month must be between 01-12", d)
	}
	if dayInt < 1 || dayInt > 31 {
		return "", fmt.Errorf("invalid day in yyyy-mm-dd date %q: day must be between 01-31", d)
	}

	output := year + month + day
	logger.Pl.D(1, "Made from/to date %q", output)

	return output, nil
}

// checkTemplateTags checks if templating tags are present.
func checkTemplateTags(d string) (hasTemplating bool) {
	s := strings.Index(d, "{{")
	e := strings.Index(d, "}}")

	if s > -1 && e > s+2 {
		if exists := templates.TemplateMap[d[(s+2):e]]; exists { // "+ 2" to skip the "{{" opening tag and leave just the tag
			return true
		}
	}

	if e+2 < len(d) {
		if s := strings.Index(d[e+2:], "{{"); s >= 0 {
			return checkTemplateTags(d[e+2:])
		}
	}
	return false
}
