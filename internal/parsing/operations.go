package parsing

import (
	"errors"
	"fmt"
	"net/url"
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
	for _, w := range warnings {
		logger.Pl.W("%s", w)
	}
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
	for _, w := range warnings {
		logger.Pl.W("%s", w)
	}
	if err != nil {
		return nil, err
	}
	logger.Pl.D(1, "Parsed %d meta operations from %v", len(parsed), metaOps)
	return parsed, nil
}

// ParseFilterOps parses filter operation strings into models.
//
// Format: "title:omits:frogs:must" or "title:contains:cat"
//
// When requireMustAny is false, must is added automatically to each filter.
func ParseFilterOps(ops []string, requireMustAny bool) ([]models.Filters, error) {
	return sharedparsing.ParseFilterOps(ops, requireMustAny)
}

// ParseNotifications parses notification string pairs into models.
//
// Format: "URL|friendly name" or "Channel URL|Notify URL|Name"
func ParseNotifications(notifications []string) ([]*models.Notification, error) {
	if len(notifications) == 0 {
		return nil, nil
	}
	// Deduplicate.
	notifications = validation.DeduplicateSliceEntries(notifications)

	notificationModels := make([]*models.Notification, 0, len(notifications))
	for _, n := range notifications {
		if !strings.ContainsRune(n, '|') {
			return nil, fmt.Errorf("notification entry %q does not contain a '|' separator (should be in 'URL|friendly name' format", n)
		}

		entry := sharedparsing.EscapedSplit(n, '|')

		// Check entries for validity and fill field details.
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
				return nil, fmt.Errorf("channel URL %q not valid: %w", chanURL, err)
			}
			if _, err := url.Parse(nURL); err != nil {
				return nil, fmt.Errorf("notification URL %q not valid: %w", nURL, err)
			}
		}

		// Use URL as name if name field is missing.
		if name == "" {
			name = nURL
		}

		// Create model.
		newNotificationModel := models.Notification{
			ChannelURL: chanURL,
			NotifyURL:  nURL,
			Name:       name,
		}

		// Append to collection.
		notificationModels = append(notificationModels, &newNotificationModel)
		logger.Pl.D(3, "Added notification model: %+v", newNotificationModel)
	}
	return notificationModels, nil
}

// ParseMetaFilterMoveOps parses meta filter move operation strings into models.
//
// Format: "title:frogs:/home/frogs"
func ParseMetaFilterMoveOps(ops []string) ([]models.MetaFilterMoveOps, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	// Deduplicate.
	ops = validation.DeduplicateSliceEntries(ops)

	const moveOpFormatError string = "please enter move operations in the format 'field:value:output directory'.\n\n" +
		"'title:frogs:/home/frogs' moves files with 'frogs' in the metatitle to the directory '/home/frogs' upon Metarr completion"

	m := make([]models.MetaFilterMoveOps, 0, len(ops))
	for _, op := range ops {
		chanURL, op := validation.CheckForOpURL(op)
		split := sharedparsing.EscapedSplit(op, ':')

		if len(split) != 3 {
			return nil, errors.New(moveOpFormatError)
		}

		field := strings.ToLower(strings.TrimSpace(split[0]))
		value := strings.ToLower(split[1])
		outputDir := strings.TrimSpace(strings.TrimSpace(split[2]))

		m = append(m, models.MetaFilterMoveOps{
			Field:         field,
			ContainsValue: value,
			OutputDir:     outputDir,
			ChannelURL:    chanURL,
		})
	}
	return m, nil
}

// ParseFilteredMetaOps parses filter-based meta operation strings into models.
//
// Format: "title:contains:cat|director:set:Mr. Cat"
func ParseFilteredMetaOps(filteredMetaOps []string) ([]models.FilteredMetaOps, error) {
	parsed, warnings, err := sharedparsing.ParseFilteredMetaOps(filteredMetaOps)
	for _, w := range warnings {
		logger.Pl.W("%s", w)
	}
	return parsed, err
}

// ParseFilteredFilenameOps parses filter-based filename operation strings into models.
//
// Format: "title:contains:cat|prefix:[CATS] "
func ParseFilteredFilenameOps(filteredFilenameOps []string) ([]models.FilteredFilenameOps, error) {
	parsed, warnings, err := sharedparsing.ParseFilteredFilenameOps(filteredFilenameOps)
	for _, w := range warnings {
		logger.Pl.W("%s", w)
	}
	return parsed, err
}
