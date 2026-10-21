package parsing

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// ParseFilenameOps parses filename transformation operation strings into models.
//
// Format: "prefix:[COOL CATEGORY] " or "date-tag:prefix:ymd"
func ParseFilenameOps(filenameOps []string) ([]models.FilenameOps, error) {
	parsed, warnings, err := sharedparsing.ParseFilenameOps(filenameOps)
	logWarnings(warnings)
	if err != nil {
		return nil, err
	}
	logger.Pl.D(1, "Parsed %d filename operations from %v", len(parsed), filenameOps)
	return parsed, nil
}

// ParseMetaOps parses meta transformation operation strings into models.
//
// Format: "director:set:Spielberg" or "title:date-tag:suffix:ymd"
func ParseMetaOps(metaOps []string) ([]models.MetaOps, error) {
	parsed, warnings, err := sharedparsing.ParseMetaOps(metaOps)
	logWarnings(warnings)
	if err != nil {
		return nil, err
	}
	logger.Pl.D(1, "Parsed %d meta operations from %v", len(parsed), metaOps)
	return parsed, nil
}

// logWarnings logs parse warnings, which only cover duplicates and unusable channel URLs.
// Malformed or invalid entries fail the parse itself.
func logWarnings(warnings []sharedparsing.Warning) {
	for _, w := range warnings {
		logger.Pl.W("%s", w)
	}
}

// ParseFilterOps parses filter operation strings into models.
//
// Format: "title:omits:frogs:must" or "title:contains:cat"
//
// When requireMustAny is false, must is added automatically to each filter.
func ParseFilterOps(ops []string, requireMustAny bool) ([]models.Filters, error) {
	return sharedparsing.ParseFilterOps(ops, requireMustAny)
}

// ParseNotifications parses notification strings into models.
//
// Format: "Notify URL" or "Channel URL|Notify URL".
func ParseNotifications(notifications []string) ([]*models.Notification, error) {
	if len(notifications) == 0 {
		return nil, nil
	}
	// Deduplicate.
	notifications = validation.DeduplicateSliceEntries(notifications)

	notificationModels := make([]*models.Notification, 0, len(notifications))
	for _, n := range notifications {
		entry := sharedparsing.EscapedSplit(n, '|')

		// Check entries for validity and fill field details.
		var chanURL, nURL string
		switch len(entry) {
		case 1:
			nURL = entry[0]
		case 2:
			chanURL, nURL = entry[0], entry[1]
		default:
			return nil, fmt.Errorf("malformed notification entry %q, should be in 'Notify URL' or 'Channel URL|Notify URL' format", n)
		}

		if chanURL != "" && !validation.IsHTTPURL(chanURL) {
			return nil, fmt.Errorf("channel URL %q in notification entry %q must be a valid http(s) URL", chanURL, n)
		}

		// Notifications are sent as HTTP POST requests (url.Parse allows characters URLs can't contain, like spaces).
		if !validation.IsHTTPURL(nURL) || strings.ContainsAny(nURL, " \"<>\\^`{}") {
			return nil, fmt.Errorf("notification URL %q in entry %q must be a valid http(s) URL", nURL, n)
		}

		// Create model.
		newNotificationModel := models.Notification{
			ChannelURL: chanURL,
			NotifyURL:  nURL,
		}

		// Append to collection.
		notificationModels = append(notificationModels, &newNotificationModel)
		logger.Pl.D(3, "Added notification model for channel URL %q", chanURL)
	}
	return notificationModels, nil
}

// ParseMetaFilterMoveOps parses meta filter move operation strings into models.
//
// Format: "title:contains:frogs||/home/frogs", a filter and a directory.
func ParseMetaFilterMoveOps(ops []string) ([]models.MetaFilterMoveOps, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	// Deduplicate.
	ops = validation.DeduplicateSliceEntries(ops)

	const moveOpFormatError string = "please enter move operations in the format 'field:filter_type:value||output directory'.\n\n" +
		"'title:contains:frogs||/home/frogs' moves files with 'frogs' in the metatitle to the directory '/home/frogs' upon Metarr completion"

	m := make([]models.MetaFilterMoveOps, 0, len(ops))
	for _, op := range ops {
		chanURL, op := validation.CheckForOpURL(op)

		// An unescaped "||" leaves an empty part between its two '|'.
		parts := sharedparsing.SplitUnescaped(op, '|')
		div := slices.Index(parts[1:], "") + 1
		if div == 0 || div == len(parts)-1 {
			return nil, errors.New(moveOpFormatError)
		}
		filters, err := ParseFilterOps([]string{strings.Join(parts[:div], "|")}, false)
		if err != nil {
			return nil, fmt.Errorf("move operation %q: %w", op, err)
		}
		outputDir := strings.TrimSpace(strings.Join(sharedparsing.EscapedSplit(strings.Join(parts[div+1:], "|"), '|'), "|"))
		if outputDir == "" {
			return nil, errors.New(moveOpFormatError)
		}

		m = append(m, models.MetaFilterMoveOps{
			ChannelURL: chanURL,
			Field:      filters[0].Field,
			FilterType: filters[0].FilterType,
			Value:      filters[0].Value,
			OutputDir:  outputDir,
		})
	}
	return m, nil
}

// ParseFilteredMetaOps parses filter-based meta operation strings into models.
//
// Format: "title:contains:cat||director:set:Mr. Cat"
func ParseFilteredMetaOps(filteredMetaOps []string) ([]models.FilteredMetaOps, error) {
	parsed, warnings, err := sharedparsing.ParseFilteredMetaOps(filteredMetaOps)
	logWarnings(warnings)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

// ParseFilteredFilenameOps parses filter-based filename operation strings into models.
//
// Format: "title:contains:cat||prefix:[CATS] "
func ParseFilteredFilenameOps(filteredFilenameOps []string) ([]models.FilteredFilenameOps, error) {
	parsed, warnings, err := sharedparsing.ParseFilteredFilenameOps(filteredFilenameOps)
	logWarnings(warnings)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}
