// Package validation handles validation of user flag input.
package validation

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/regex"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// CheckDirectory validates a directory path like sharedvalidation.ValidateDirectory, but without creating it or
// requiring it to exist (whether it's available is checked by CheckDestination). A blank dir is unset.
func CheckDirectory(dir string, templateMap map[string]struct{}) error {
	if dir == "" {
		return nil
	}
	hasTemplating, _, err := sharedvalidation.ValidateDirectory(dir, false, templateMap)
	if err != nil && !hasTemplating {
		if _, statErr := os.Stat(filepath.Clean(dir)); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
	}
	return err
}

// CheckFile validates a file path like sharedvalidation.ValidateFile, but without requiring it to exist (whether it
// does is checked when a save adds or changes it). A blank path is unset.
func CheckFile(path string, templateMap map[string]struct{}) error {
	if path == "" {
		return nil
	}
	hasTemplating, _, err := sharedvalidation.ValidateFile(path, false, templateMap)
	if err != nil && !hasTemplating {
		if _, statErr := os.Stat(filepath.Clean(path)); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
	}
	return err
}

// CheckDestination returns an error if a download destination isn't available (e.g. its drive isn't mounted).
//
// A plain path must exist. For a templated path, which can't exist until its tags are filled in, the directory holding
// the fixed part before the first tag must exist (e.g. "/media" for "/media/{{year}}").
func CheckDestination(dir string) error {
	if dir == "" {
		return nil
	}
	base := dir
	if before, _, ok := strings.Cut(dir, "{{"); ok {
		base = filepath.Dir(before)
	}
	info, err := os.Stat(base)
	if err != nil {
		return fmt.Errorf("destination %q isn't available: %w", base, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("destination %q isn't available: it's a file, not a directory", base)
	}
	return nil
}

// IsHTTPURL reports whether s is an absolute http or https URL with a host.
func IsHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// ValidateNotifyURL checks a notification URL can be sent an HTTP POST request.
//
// url.Parse allows characters URLs can't contain, like spaces and '|', so those are rejected too.
func ValidateNotifyURL(u string) error {
	if !IsHTTPURL(u) || strings.ContainsAny(u, " \"<>\\^`{}|") {
		return fmt.Errorf("notification URL %q must be a valid http(s) URL", u)
	}
	return nil
}

// ValidateThumbnail checks a Download As thumbnail is an http(s) URL or an existing file, and isn't also stripped.
func ValidateThumbnail(t string, strip bool) error {
	switch {
	case t == "":
		return nil
	case strip:
		return errors.New("a thumbnail can't be both set and stripped")
	case IsHTTPURL(t):
		return nil
	}
	info, err := os.Stat(t)
	if err != nil {
		return fmt.Errorf("thumbnail %q is neither an http(s) URL nor a readable file: %w", t, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("thumbnail %q is not a file", t)
	}
	return nil
}

// ValidateChannelURL checks a channel URL is an absolute http(s) URL, so it can't be read as a yt-dlp option or local file.
func ValidateChannelURL(u string) error {
	if !IsHTTPURL(u) {
		return fmt.Errorf("invalid channel URL %q: must be a full http:// or https:// URL", u)
	}
	return nil
}

// ValidateYtdlpOutputExtension validates the merge-output-format compatibility of the inputted extension.
func ValidateYtdlpOutputExtension(e string) error {
	if e == "" || e == sharedconsts.ExtSame {
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
		return fmt.Errorf("yt-dlp output extension %v is invalid or not supported", e)
	}

	return nil
}

// ValidateLoggingLevel checks and validates the debug level.
func ValidateLoggingLevel() {
	logging.SetLevel(min(max(abstractions.GetInt(keys.DebugLevel), 0), 5))
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
	logger.Pl.D(5, "Validating %d filter operations...", len(filters))
	return sharedvalidation.ValidateFilterOps(filters)
}

// ValidateMetaFilterMoveOps validates meta filter move operation models. checkPaths checks their output directories,
// as for ValidateSettingsModel.
func ValidateMetaFilterMoveOps(moveOps []models.MetaFilterMoveOps, checkPaths bool) error {
	if len(moveOps) == 0 {
		return nil
	}
	logger.Pl.D(5, "Validating %d meta filter move operations...", len(moveOps))

	for i, op := range moveOps {
		if err := ValidateFilterOps([]models.Filters{op.Filter()}); err != nil {
			return fmt.Errorf("move operation at position %d: %w", i, err)
		}

		// Validate output directory
		if checkPaths && op.OutputDir != "" {
			if err := CheckDirectory(op.OutputDir, sharedtemplates.AllTemplatesMap); err != nil {
				return fmt.Errorf("move operation at position %d has invalid directory: %w", i, err)
			}
			if err := CheckDestination(op.OutputDir); err != nil {
				return fmt.Errorf("move operation at position %d has invalid directory: %w", i, err)
			}
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

	// Handle "today" special case.
	if d == "today" {
		return time.Now().Format("20060102"), nil
	}

	// Regex to extract explicitly marked years, months, and days.
	re := regex.YearFragmentsCompile()
	matches := re.FindStringSubmatch(d)
	if matches == nil {
		return "", fmt.Errorf("invalid date format %q: expected 'Ymd' format", d)
	}

	// Default values.
	year := strconv.Itoa(time.Now().Year())
	month := "01"
	day := "01"

	// Year.
	if matches[1] != "" {
		year = matches[1]
	} else if len(d) == 8 && !strings.ContainsAny(d, "ymd") { // No markers, assume raw format.
		year, month, day = d[:4], d[4:6], d[6:8]
	}

	// Month.
	if matches[2] != "" {
		if len(matches[2]) == 1 {
			month = "0" + matches[2] // Pad single-digit months.
		} else {
			month = matches[2]
		}
	}

	// Day.
	if matches[3] != "" {
		if len(matches[3]) == 1 {
			day = "0" + matches[3] // Pad single-digit days.
		} else {
			day = matches[3]
		}
	}

	// Convert to integers.
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

	// Check validity.
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

// ValidatePreferredVideoCodecs validates video codec preferences.
func ValidatePreferredVideoCodecs(codecs []string) ([]string, error) {
	if len(codecs) == 0 {
		return nil, nil
	}

	// Remove "copy" codec if present.
	codecs = slices.DeleteFunc(codecs, func(c string) bool {
		return c == sharedconsts.VCodecCopy
	})

	// Normalize strings.
	for i := range codecs {
		codecs[i] = strings.ToLower(strings.TrimSpace(codecs[i]))
		if _, ok := sharedconsts.ValidVideoCodecs[codecs[i]]; !ok {
			return nil, fmt.Errorf("invalid video codec preference %q", codecs[i])
		}
	}

	return codecs, nil
}

// ValidatePreferredAudioCodecs validates audio codec preferences.
func ValidatePreferredAudioCodecs(codecs []string) ([]string, error) {
	if len(codecs) == 0 {
		return nil, nil
	}

	// Remove "copy" codec if present.
	codecs = slices.DeleteFunc(codecs, func(c string) bool {
		return c == sharedconsts.ACodecCopy
	})

	// Normalize strings.
	for i := range codecs {
		codecs[i] = strings.ToLower(strings.TrimSpace(codecs[i]))
		if _, ok := sharedconsts.ValidAudioCodecs[codecs[i]]; !ok {
			return nil, fmt.Errorf("invalid audio codec preference %q", codecs[i])
		}
	}

	return codecs, nil
}
