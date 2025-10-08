// Package validation handles validation of user flag input.
package validation

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/regex"
	"tubarr/internal/domain/templates"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/viper"
)

// ValidateMetarrOutputDirs validates the output directories for Metarr.
func ValidateMetarrOutputDirs(defaultDir string, urlDirs []string, c *models.Channel) (map[string]string, error) {
	if len(urlDirs) == 0 && defaultDir == "" {
		return nil, nil
	}

	// Initialize map and fill from existing
	outDirMap := make(map[string]string)
	if c.ChanMetarrArgs.OutputDirMap != nil {
		maps.Copy(outDirMap, c.ChanMetarrArgs.OutputDirMap)
	}

	validatedDirs := make(map[string]bool, len(c.URLModels))

	// Parse and validate URL output directory pairs
	for _, pair := range urlDirs {
		url, dir, err := parseURLDirPair(pair)
		if err != nil {
			return nil, err
		}

		// Check if this URL exists in the channel
		found := false
		for _, cu := range c.URLModels {
			if strings.EqualFold(strings.TrimSpace(cu.URL), strings.TrimSpace(url)) {
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("channel does not contain URL %q, provided in output directory mapping", url)
		}

		outDirMap[url] = dir
	}

	// Fill blank channel entries with channel default
	for _, cu := range c.URLModels {
		if outDirMap[cu.URL] == "" && defaultDir != "" {
			outDirMap[cu.URL] = defaultDir
		}
	}

	// Validate directories
	for _, dir := range outDirMap {
		if !validatedDirs[dir] {
			if _, err := ValidateDirectory(dir, false); err != nil {
				return nil, err
			}
			validatedDirs[dir] = true
		}
	}

	logging.D(1, "Metarr output directories: %q", outDirMap)
	return outDirMap, nil
}

// parseURLDirPair parses a 'url:output directory' pairing and validates the format.
func parseURLDirPair(pair string) (u string, d string, err error) {
	parts := strings.Split(pair, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid URL|directory pair: %q", pair)
	}

	u = parts[0]
	if _, err := url.ParseRequestURI(u); err != nil {
		return "", "", fmt.Errorf("invalid URL format: %q", u)
	}

	d = parts[1]
	if _, err := ValidateDirectory(d, false); err != nil {
		return "", "", err
	}

	return parts[0], parts[1], nil
}

// ValidateDirectory validates that the directory exists, else creates it if desired.
func ValidateDirectory(dir string, createIfNotFound bool) (os.FileInfo, error) {
	possibleTemplate := strings.Contains(dir, "{{") && strings.Contains(dir, "}}")
	logging.D(3, "Statting directory %q. Templating detected? %v...", dir, possibleTemplate)

	// Handle templated directories
	if possibleTemplate {
		if !checkTemplateTags(dir) {
			t := make([]string, 0, len(templates.TemplateMap))
			for k := range templates.TemplateMap {
				t = append(t, k)
			}
			return nil, fmt.Errorf("directory contains unsupported template tags. Supported tags: %v", t)
		}
		logging.D(3, "Directory %q appears to contain templating elements, will not stat", dir)
		return nil, nil // templates are valid, no need to stat
	}

	// Check directory existence
	dirInfo, err := os.Stat(dir)
	switch {
	case err == nil: // If err IS nil

		if !dirInfo.IsDir() {
			return dirInfo, fmt.Errorf("path %q is a file, not a directory", dir)
		}
		return dirInfo, nil

	case os.IsNotExist(err):
		// path does not exist
		if createIfNotFound {
			logging.D(3, "Directory %q does not exist, creating it...", dir)
			if err := os.MkdirAll(dir, consts.PermsGenericDir); err != nil {
				return nil, fmt.Errorf("directory %q does not exist and failed to create: %w", dir, err)
			}
			if dirInfo, err = os.Stat(dir); err != nil { // re-stat to get correct FileInfo
				return dirInfo, fmt.Errorf("failed to stat %q", dir)
			}
			return dirInfo, nil
		}
		return nil, fmt.Errorf("directory %q does not exist", dir)

	default:
		// other error
		return nil, fmt.Errorf("failed to stat directory %q: %w", dir, err)
	}
}

// ValidateFile validates that the file exists, else creates it if desired.
func ValidateFile(f string, createIfNotFound bool) (os.FileInfo, error) {

	logging.D(3, "Statting file %q...", f)
	fileInfo, err := os.Stat(f)
	if err != nil {
		if os.IsNotExist(err) {
			switch {
			case createIfNotFound:
				logging.D(3, "File %q does not exist, creating it...", f)
				if _, err := os.Create(f); err != nil {
					return fileInfo, fmt.Errorf("file %q does not exist and Tubarr failed to create it: %w", f, err)
				}
			default:
				return fileInfo, fmt.Errorf("file %q does not exist: %w", f, err)
			}
		} else {
			return fileInfo, fmt.Errorf("failed to stat file %q: %w", f, err)
		}
	}

	if fileInfo != nil {
		if fileInfo.IsDir() {
			return fileInfo, fmt.Errorf("file entered %q is a directory", f)
		}
	}

	return fileInfo, nil
}

// ValidateViperFlags verifies that the user input flags are valid, modifying them to defaults or returning bools/errors.
func ValidateViperFlags() error {

	// Output filetype
	if viper.IsSet(keys.OutputFiletype) {
		ext := strings.ToLower(viper.GetString(keys.OutputFiletype))

		if ext != "" {
			dottedExt, err := ValidateOutputFiletype(ext)
			if err != nil {
				return fmt.Errorf("invalid output filetype %q", ext)

			}
			viper.Set(keys.OutputFiletype, dottedExt)
		}
	}

	// Meta purge
	if viper.IsSet(keys.MMetaPurge) {
		purge := viper.GetString(keys.MMetaPurge)
		if purge != "" && !ValidatePurgeMetafiles(purge) {
			return fmt.Errorf("invalid meta purge type %q", purge)
		}
	}

	// Logging
	ValidateLoggingLevel()
	viper.Set(keys.Concurrency, ValidateConcurrencyLimit(viper.GetInt(keys.Concurrency)))
	return nil
}

// ValidateConcurrencyLimit checks and ensures correct concurrency limit input.
func ValidateConcurrencyLimit(c int) int {

	switch {
	case c < 1:
		c = 1
	default:
		fmt.Printf("Max concurrency: %d", c)
	}

	return c
}

// ValidateNotificationStrings verifies that the notification pairs entered are valid.
func ValidateNotificationStrings(notifications []string) ([]*models.Notification, error) {
	if len(notifications) == 0 {
		return nil, nil
	}

	notificationModels := make([]*models.Notification, 0, len(notifications))
	for _, n := range notifications {

		if !strings.ContainsRune(n, '|') {
			return nil, fmt.Errorf("notification entry %q does not contain a '|' separator (should be in 'URL|friendly name' format", n)
		}

		entry := EscapedSplit(n, '|')

		// Check entries for validity and fill field details
		var chanURL, nURL, name string
		switch {
		case len(entry) > 3 || len(entry) < 2:
			return nil, fmt.Errorf("malformed notification entry %q, should be in 'Channel URL|Notify URL|Friendly Name' or 'Notify URL|Friendly Name' format", n)

			// 'Notify URL|Name'
		case len(entry) == 2:
			if entry[0] == "" {
				return nil, fmt.Errorf("missing URL from notification entry %q, should be in 'Notify URL|Friendly Name' format", n)
			}
			nURL = entry[0]
			name = entry[1]

			if _, err := url.Parse(nURL); err != nil {
				return nil, fmt.Errorf("notification URL %q not valid: %w", nURL, err)
			}

			// 'Channel URL|Notify URL|Name'
		case len(entry) == 3:
			if entry[0] == "" || entry[1] == "" {
				return nil, fmt.Errorf("missing channel URL or notification URL from notification entry %q, should be in 'Channel URL|Notify URL|Friendly Name' format", n)
			}
			chanURL = entry[0]
			nURL = entry[1]
			name = entry[2]

			if _, err := url.Parse(chanURL); err != nil {
				return nil, fmt.Errorf("notification URL %q not valid: %w", nURL, err)
			}
			if _, err := url.Parse(nURL); err != nil {
				return nil, fmt.Errorf("notification URL %q not valid: %w", nURL, err)
			}
		}

		// Use URL as name if name field is missing
		if name == "" {
			name = nURL
		}

		// Create model
		newNotificationModel := models.Notification{
			ChannelURL: chanURL,
			NotifyURL:  nURL,
			Name:       name,
		}

		// Append to collection
		notificationModels = append(notificationModels, &newNotificationModel)
		logging.D(3, "Added notification model: %+v", newNotificationModel)
	}

	return notificationModels, nil
}

// ValidateYtdlpOutputExtension validates the merge-output-format compatibility of the inputted extension.
func ValidateYtdlpOutputExtension(e string) error {
	e = strings.TrimPrefix(strings.ToLower(e), ".")
	switch e {
	case "avi", "flv", "mkv", "mov", "mp4", "webm":
		return nil
	default:
		return fmt.Errorf("output extension %v is invalid or not supported", e)
	}
}

// ValidateLoggingLevel checks and validates the debug level.
func ValidateLoggingLevel() {
	l := min(max(viper.GetInt(keys.DebugLevel), 0), 5)

	logging.Level = l
	fmt.Printf("Logging level: %d\n", logging.Level)
}

// WarnMalformedKeys warns a user if a key in their config file is mixed casing.
func WarnMalformedKeys() {
	for _, key := range viper.AllKeys() {
		if strings.Contains(key, "-") && strings.Contains(key, "_") {
			logging.W("Config key %q mixes dashes and underscores - use either kebab-case or snake_case consistently", key)
		}
	}
}

// ValidateMaxFilesize checks the max filesize setting.
func ValidateMaxFilesize(m string) (string, error) {
	m = strings.ToUpper(m)
	switch {
	case strings.HasSuffix(m, "B"), strings.HasSuffix(m, "K"), strings.HasSuffix(m, "M"), strings.HasSuffix(m, "G"):
		return strings.TrimSuffix(m, "B"), nil
	default:
		if _, err := strconv.Atoi(m); err != nil {
			return "", err
		}
	}
	return m, nil
}

// ValidateFilterOps verifies that the user inputted filters are valid.
func ValidateFilterOps(ops []string) ([]models.DLFilters, error) {
	if len(ops) == 0 {
		return nil, nil
	}

	const (
		formatErrorMsg = "please enter filters in the format 'field:filter_type:value:must_or_any'.\n\ntitle:omits:frogs:must' ignores all videos with frogs in the metatitle.\n\n'title:contains:cat:any','title:contains:dog:any' only includes videos with EITHER cat and dog in the title (use 'must' to require both).\n\n'date:omits:must' omits videos only when the metafile contains a date field"
	)

	var filters = make([]models.DLFilters, 0, len(ops))

	for _, op := range ops {

		// Extract optional channel URL and remaining filter string
		chanURL, op := CheckForOpURL(op)
		split := EscapedSplit(op, ':')

		if len(split) < 3 {
			logging.E(formatErrorMsg)
			return nil, errors.New("filter format error")
		}

		// Normalize values
		field := strings.ToLower(strings.TrimSpace(split[0]))
		containsOmits := strings.ToLower(strings.TrimSpace(split[1]))
		mustAny := strings.ToLower(strings.TrimSpace(split[len(split)-1]))
		var value string
		if len(split) == 4 {
			value = strings.ToLower(split[2])
		}

		// Validate contains/omits
		if containsOmits != "contains" && containsOmits != "omits" {
			logging.E(formatErrorMsg)
			return nil, errors.New("please enter a filter type of either 'contains' or 'omits'")
		}

		// Validate must/any
		if mustAny != "must" && mustAny != "any" {
			return nil, errors.New("filter type must be set to 'must' or 'any'")
		}

		// Append filter
		filters = append(filters, models.DLFilters{
			Field:      field,
			Type:       containsOmits,
			Value:      value,
			MustAny:    mustAny,
			ChannelURL: chanURL,
		})
	}

	return filters, nil
}

// ValidateMoveOps validates that the user's inputted move filter operations are valid.
func ValidateMoveOps(ops []string) ([]models.MoveOps, error) {
	if len(ops) == 0 {
		return nil, nil
	}

	const (
		moveOpFormatError string = "please enter move operations in the format 'field:value:output directory'.\n\n'title:frogs:/home/frogs' moves files with 'frogs' in the metatitle to the directory '/home/frogs' upon Metarr completion"
	)

	m := make([]models.MoveOps, 0, len(ops))

	for _, op := range ops {

		chanURL, op := CheckForOpURL(op)
		split := EscapedSplit(op, ':')

		if len(split) != 3 {
			return nil, errors.New(moveOpFormatError)
		}

		field := strings.ToLower(strings.TrimSpace(split[0]))
		value := strings.ToLower(split[1])
		outputDir := strings.TrimSpace(strings.TrimSpace(split[2]))

		if _, err := ValidateDirectory(outputDir, false); err != nil {
			return nil, err
		}

		m = append(m, models.MoveOps{
			Field:      field,
			Value:      value,
			OutputDir:  outputDir,
			ChannelURL: chanURL,
		})
	}
	return m, nil
}

// ValidateToFromDate validates a date string in yyyymmdd or formatted like "2025y12m31d".
func ValidateToFromDate(d string) (string, error) {
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
	logging.D(1, "Made from/to date %q", output)

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
