package parsing

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// ParseFilenameOps parses filename transformation operation strings into models.
//
// Format: "prefix:[COOL CATEGORY] " or "date-tag:prefix:ymd"
func ParseFilenameOps(filenameOps []string) ([]models.FilenameOps, error) {
	if len(filenameOps) == 0 {
		logger.Pl.D(4, "No filename operations to parse")
		return nil, nil
	}
	// Deduplicate.
	filenameOps = validation.DeduplicateSliceEntries(filenameOps)

	valid := make([]models.FilenameOps, 0, len(filenameOps))
	exists := make(map[string]bool, len(filenameOps))

	logger.Pl.D(1, "Parsing filename operations %v...", filenameOps)

	for _, op := range filenameOps {
		opURL, opPart := validation.CheckForOpURL(op)
		split := validation.EscapedSplit(opPart, ':')

		if len(split) < 2 || len(split) > 3 {
			logger.Pl.E("Invalid filename operation %q (format should be: 'prefix:[COOL CATEGORY] ', or 'date-tag:prefix:ymd', etc.)", op)
			continue
		}

		var newFilenameOp models.FilenameOps
		var key string

		switch len(split) {
		case 2: // e.g. 'prefix:[DOG VIDEOS]'
			newFilenameOp.OpType = split[0]  // e.g. 'append'
			newFilenameOp.OpValue = split[1] // e.g. '(new)'

			// Build uniqueness key
			key = strings.Join([]string{newFilenameOp.OpType, newFilenameOp.OpValue}, ":")

		case 3: // e.g. 'replace-suffix:_1:' or 'date-tag:prefix:ymd'
			newFilenameOp.OpType = split[0]

			switch split[0] {
			case sharedconsts.OpReplaceSuffix, sharedconsts.OpReplacePrefix, sharedconsts.OpReplace:
				newFilenameOp.OpFindString = split[1] // e.g. '_1'
				newFilenameOp.OpValue = split[2]      // e.g. ''
				key = strings.Join([]string{newFilenameOp.OpType, newFilenameOp.OpFindString, newFilenameOp.OpValue}, ":")

			case sharedconsts.OpDateTag, sharedconsts.OpDeleteDateTag:
				newFilenameOp.OpLoc = split[1]      // e.g. 'prefix'
				newFilenameOp.DateFormat = split[2] // e.g. 'ymd'
				key = newFilenameOp.OpType

			default:
				logger.Pl.E("Invalid filename operation type %q", split[0])
				continue
			}

		default:
			logger.Pl.E("Invalid filename operation %q", opPart)
			continue
		}

		// Check if key exists (skip duplicates)
		if exists[key] {
			logger.Pl.I("Duplicate filename operation %q, skipping", opPart)
			continue
		}
		exists[key] = true

		// Add channel URL if present
		if opURL != "" {
			newFilenameOp.ChannelURL = opURL
		}

		// Add successful filename operation
		valid = append(valid, newFilenameOp)
	}

	// Check length of valid filename operations
	if len(valid) == 0 {
		return nil, errors.New("no valid filename operations")
	}
	return valid, nil
}

// ParseMetaOps parses meta transformation operation strings into models.
//
// Format: "director:set:Spielberg" or "title:date-tag:suffix:ymd"
func ParseMetaOps(metaOps []string) ([]models.MetaOps, error) {
	if len(metaOps) == 0 {
		logger.Pl.D(4, "No meta operations to parse")
		return nil, nil
	}
	// Deduplicate.
	metaOps = validation.DeduplicateSliceEntries(metaOps)

	valid := make([]models.MetaOps, 0, len(metaOps))
	exists := make(map[string]bool, len(metaOps))

	logger.Pl.D(1, "Parsing meta operations %v...", metaOps)

	for _, op := range metaOps {
		opURL, opPart := validation.CheckForOpURL(op)
		split := validation.EscapedSplit(opPart, ':')

		if len(split) < 3 || len(split) > 4 {
			logger.Pl.E("Invalid meta operation %q", op)
			continue
		}

		var newMetaOp models.MetaOps
		var key string

		switch len(split) {
		case 3: // e.g. 'director:set:Spielberg'
			newMetaOp.Field = split[0]   // e.g. 'director'
			newMetaOp.OpType = split[1]  // e.g. 'set'
			newMetaOp.OpValue = split[2] // e.g. 'Spielberg'

			// Build uniqueness key.
			key = strings.Join([]string{newMetaOp.Field, newMetaOp.OpType, newMetaOp.OpValue}, ":")

		case 4: // e.g. 'title:date-tag:suffix:ymd' or 'title:replace:old:new'
			newMetaOp.Field = split[0]
			newMetaOp.OpType = split[1]

			switch newMetaOp.OpType {
			case sharedconsts.OpDateTag, sharedconsts.OpDeleteDateTag:
				newMetaOp.OpLoc = split[2]
				newMetaOp.DateFormat = split[3]
				key = strings.Join([]string{newMetaOp.Field, newMetaOp.OpType}, ":")

			case sharedconsts.OpReplace, sharedconsts.OpReplaceSuffix, sharedconsts.OpReplacePrefix:
				newMetaOp.OpFindString = split[2]
				newMetaOp.OpValue = split[3]
				key = strings.Join([]string{newMetaOp.Field, newMetaOp.OpType, newMetaOp.OpFindString, newMetaOp.OpValue}, ":")

			default:
				logger.Pl.E("Invalid 4-part meta op type %q", newMetaOp.OpType)
				continue
			}

		default:
			logger.Pl.E("Invalid meta op %q", opPart)
			continue
		}

		// Check if key exists (skip duplicates).
		if exists[key] {
			logger.Pl.I("Duplicate meta operation %q, skipping", opPart)
			continue
		}
		exists[key] = true

		// Add channel URL if present.
		if opURL != "" {
			newMetaOp.ChannelURL = opURL
		}

		// Add successful meta operation.
		valid = append(valid, newMetaOp)
	}

	if len(valid) == 0 {
		return nil, errors.New("no valid meta operations")
	}
	return valid, nil
}

// ParseFilterOps parses filter operation strings into models.
//
// Format: "title:omits:frogs:must" or "title:contains:cat:any"
func ParseFilterOps(ops []string) ([]models.Filters, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	// Deduplicate.
	ops = validation.DeduplicateSliceEntries(ops)

	const formatErrorMsg = "please enter filters in the format 'field:filter_type:value:must_or_any'.\n\n" +
		"'title:omits:frogs:must' ignores all videos with frogs in the metatitle.\n" +
		"'title:contains:cat:any','title:contains:dog:any' only includes videos with EITHER cat and dog in the title (use 'must' to require both).\n" +
		"'date:omits:must' omits videos only when the metafile contains a date field"

	var filters = make([]models.Filters, 0, len(ops))
	for _, op := range ops {
		// Extract optional channel URL and remaining filter string.
		chanURL, op := validation.CheckForOpURL(op)
		split := validation.EscapedSplit(op, ':')

		if len(split) < 3 {
			logger.Pl.E(formatErrorMsg)
			return nil, errors.New("filter format error")
		}

		// Normalize values.
		field := strings.ToLower(strings.TrimSpace(split[0]))
		containsOmits := strings.ToLower(strings.TrimSpace(split[1]))
		mustAny := strings.ToLower(strings.TrimSpace(split[len(split)-1]))
		var value string
		if len(split) == 4 {
			value = strings.ToLower(split[2])
		}

		// Append filter.
		filters = append(filters, models.Filters{
			Field:         field,
			ContainsOmits: containsOmits,
			Value:         value,
			MustAny:       mustAny,
			ChannelURL:    chanURL,
		})
	}
	return filters, nil
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

		entry := validation.EscapedSplit(n, '|')

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
		split := validation.EscapedSplit(op, ':')

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
// Format: "title:contains:cat:any|director:set:Mr. Cat"
func ParseFilteredMetaOps(filteredMetaOps []string) ([]models.FilteredMetaOps, error) {
	if len(filteredMetaOps) == 0 {
		return nil, nil
	}
	// Deduplicate.
	filteredMetaOps = validation.DeduplicateSliceEntries(filteredMetaOps)

	validFilteredMetaOps := make([]models.FilteredMetaOps, 0, len(filteredMetaOps))
	for _, fmo := range filteredMetaOps {
		if !strings.ContainsRune(fmo, '|') {
			continue
		}

		split := validation.EscapedSplit(fmo, '|')
		if len(split) < 2 {
			return nil, fmt.Errorf("invalid format for filtered meta operation (entered: %s). Please use 'Filter Rules|Meta Operations'", fmo)
		}

		filterRule := split[:1]
		metaRules := split[1:]

		// Parse both filter and meta operations.
		filterOps, err := ParseFilterOps(filterRule)
		if err != nil {
			return nil, err
		}

		metaOps, err := ParseMetaOps(metaRules)
		if err != nil {
			return nil, err
		}

		if len(filterOps) == 0 || len(metaOps) == 0 {
			continue
		}

		validFilteredMetaOps = append(validFilteredMetaOps, models.FilteredMetaOps{
			Filters: filterOps,
			MetaOps: metaOps,
		})
	}
	return validFilteredMetaOps, nil
}

// ParseFilteredFilenameOps parses filter-based filename operation strings into models.
//
// Format: "title:contains:cat:any|prefix:[CATS] "
func ParseFilteredFilenameOps(filteredFilenameOps []string) ([]models.FilteredFilenameOps, error) {
	if len(filteredFilenameOps) == 0 {
		return nil, nil
	}
	// Deduplicate.
	filteredFilenameOps = validation.DeduplicateSliceEntries(filteredFilenameOps)

	validFilteredFilenameOps := make([]models.FilteredFilenameOps, 0, len(filteredFilenameOps))
	for _, fmo := range filteredFilenameOps {
		if !strings.ContainsRune(fmo, '|') {
			continue
		}

		split := validation.EscapedSplit(fmo, '|')
		if len(split) < 2 {
			return nil, fmt.Errorf("invalid format for filtered filename operation (entered: %s). Please use 'Filter Rules|Filename Operations'", fmo)
		}

		filterRule := split[:1]
		filenameRules := split[1:]

		// Parse both filter and filename operations.
		filterOps, err := ParseFilterOps(filterRule)
		if err != nil {
			return nil, err
		}

		filenameOps, err := ParseFilenameOps(filenameRules)
		if err != nil {
			return nil, err
		}

		if len(filterOps) == 0 || len(filenameOps) == 0 {
			continue
		}

		validFilteredFilenameOps = append(validFilteredFilenameOps, models.FilteredFilenameOps{
			Filters:     filterOps,
			FilenameOps: filenameOps,
		})
	}
	return validFilteredFilenameOps, nil
}

// parseURLDirPair parses a 'url|output directory' pairing.
func parseURLDirPair(pair string) (u string, d string, err error) {
	parts := strings.Split(pair, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid URL|directory pair: %q", pair)
	}
	u = parts[0]
	if _, err := url.ParseRequestURI(u); err != nil {
		return "", "", fmt.Errorf("invalid URL format: %q", u)
	}

	return parts[0], parts[1], nil
}
