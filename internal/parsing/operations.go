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
	"github.com/TubarrApp/gocommon/sharedtemplates"
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

// FillChannelTags fills the channel tags in each op, leaving any other tag for Metarr to fill from the metadata.
func FillChannelTags(ops []string, c *models.Channel, cu *models.ChannelURL) []string {
	filled := make([]string, 0, len(ops))
	for _, op := range ops {
		f, err := fillOpTags(op, c, cu)
		if err != nil {
			logger.Pl.E("Failed to fill tags in op %q: %v", op, err)
			f = op
		}
		filled = append(filled, f)
	}
	return filled
}

// fillOpTags fills the channel tags in one op.
func fillOpTags(op string, c *models.Channel, cu *models.ChannelURL) (string, error) {
	var b strings.Builder
	remaining := op
	for {
		// Find the next tag.
		startIdx := strings.Index(remaining, openTemplate)
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(remaining[startIdx:], closeTemplate)
		if endIdx == -1 {
			break
		}
		endIdx += startIdx
		tag := remaining[startIdx+len(openTemplate) : endIdx]

		// Fill channel tags, writing any other as it is.
		b.WriteString(remaining[:startIdx])
		if sharedtemplates.ValidTag(tag, sharedtemplates.TubarrTemplateTags) {
			name, _ := sharedtemplates.ParseTag(tag)
			value, err := channelTagValue(name, c, cu)
			if err != nil {
				return "", err
			}
			b.WriteString(sharedparsing.Escape(value)) // Escaped, as a ':' in the value would split the op.
		} else {
			b.WriteString(remaining[startIdx : endIdx+len(closeTemplate)])
		}
		remaining = remaining[endIdx+len(closeTemplate):]
	}
	b.WriteString(remaining)
	return b.String(), nil
}

// logWarnings logs parse warnings, which only cover duplicates. Malformed or invalid entries fail the parse itself.
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

// ParseNotifications parses notification URLs into models for channelURL, or for every channel URL if it's empty.
func ParseNotifications(channelURL string, notifyURLs []string) ([]*models.Notification, error) {
	if len(notifyURLs) == 0 {
		return nil, nil
	}
	// Deduplicate.
	notifyURLs = validation.DeduplicateSliceEntries(notifyURLs)

	notificationModels := make([]*models.Notification, 0, len(notifyURLs))
	for _, u := range notifyURLs {
		if err := validation.ValidateNotifyURL(u); err != nil {
			return nil, err
		}
		notificationModels = append(notificationModels, &models.Notification{ChannelURL: channelURL, NotifyURL: u})
		logger.Pl.D(3, "Added notification model for channel URL %q", channelURL)
	}
	return notificationModels, nil
}

// ParseMetaFilterMoveOps parses meta filter move operation strings into models.
//
// Format: "title:contains:frogs&&duration:more-than:600||/home/frogs", filters and a directory.
func ParseMetaFilterMoveOps(ops []string) ([]models.MetaFilterMoveOps, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	// Deduplicate.
	ops = validation.DeduplicateSliceEntries(ops)

	const moveOpFormatError string = "please enter move operations in the format 'field:filter_type:value||output directory', joining several filters with '&&' to require all of them.\n\n" +
		"'title:contains:frogs||/home/frogs' moves files with 'frogs' in the metatitle to the directory '/home/frogs' upon Metarr completion.\n" +
		"'title:contains:frogs&&duration:more-than:3600||/home/long frogs' moves files with 'frogs' in the metatitle that are over an hour long to '/home/long frogs'"

	m := make([]models.MetaFilterMoveOps, 0, len(ops))
	for _, op := range ops {
		// An unescaped "||" leaves an empty part between its two '|'.
		parts := sharedparsing.SplitUnescaped(op, '|')
		div := slices.Index(parts[1:], "") + 1
		if div == 0 || div == len(parts)-1 {
			return nil, errors.New(moveOpFormatError)
		}
		filters, err := ParseFilterOps(sharedparsing.SplitFilters(strings.Join(parts[:div], "|")), false)
		if err != nil {
			return nil, fmt.Errorf("move operation %q: %w", op, err)
		}
		outputDir := strings.TrimSpace(strings.Join(sharedparsing.EscapedSplit(strings.Join(parts[div+1:], "|"), '|'), "|"))
		if outputDir == "" {
			return nil, errors.New(moveOpFormatError)
		}

		m = append(m, models.MetaFilterMoveOps{
			Filters:   filters,
			OutputDir: outputDir,
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
