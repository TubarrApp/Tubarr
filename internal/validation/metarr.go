package validation

import (
	"fmt"
	"slices"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ValidateFilenameOps validates filename transformation operation models.
func ValidateFilenameOps(filenameOps []models.FilenameOps) error {
	if len(filenameOps) == 0 {
		logger.Pl.D(4, "No filename operations to validate")
		return nil
	}
	logger.Pl.D(3, "Validating %d filename operations...", len(filenameOps))

	var validFilenameActions = map[string]struct{}{
		sharedconsts.OpAppend:        {},
		sharedconsts.OpPrefix:        {},
		sharedconsts.OpReplacePrefix: {},
		sharedconsts.OpReplaceSuffix: {},
		sharedconsts.OpReplace:       {},
		sharedconsts.OpDateTag:       {},
		sharedconsts.OpDeleteDateTag: {},
		sharedconsts.OpSet:           {},
	}

	// Validate each filename operation
	for i, op := range filenameOps {
		// Validate operation type is valid
		if _, ok := validFilenameActions[op.OpType]; !ok {
			return fmt.Errorf("invalid filename operation type %q at position %d (valid: append, prefix, replace-prefix, replace-suffix, replace, date-tag, delete-date-tag, set)", op.OpType, i)
		}

		// Validate date-tag operations
		if op.OpType == sharedconsts.OpDateTag {
			if op.OpLoc != sharedconsts.OpLocPrefix && op.OpLoc != sharedconsts.OpLocSuffix {
				return fmt.Errorf("invalid date tag location %q at position %d, use prefix or suffix", op.OpLoc, i)
			}
			if !ValidateDateFormat(op.DateFormat) {
				return fmt.Errorf("invalid date tag format %q at position %d", op.DateFormat, i)
			}
		}

		// Validate delete-date-tag operations
		if op.OpType == sharedconsts.OpDeleteDateTag {
			if op.OpLoc != sharedconsts.OpLocPrefix && op.OpLoc != sharedconsts.OpLocSuffix && op.OpLoc != sharedconsts.OpLocAll {
				return fmt.Errorf("invalid date tag location %q at position %d, use prefix, suffix, or all", op.OpLoc, i)
			}
			if !ValidateDateFormat(op.DateFormat) {
				return fmt.Errorf("invalid date tag format %q at position %d", op.DateFormat, i)
			}
		}
	}

	return nil
}

// ValidateMetaOps validates meta transformation operation models.
func ValidateMetaOps(metaOps []models.MetaOps) error {
	if len(metaOps) == 0 {
		logger.Pl.D(4, "No meta operations to validate")
		return nil
	}
	logger.Pl.D(3, "Validating %d meta operations...", len(metaOps))

	var validMetaActions = map[string]struct{}{
		sharedconsts.OpAppend:        {},
		sharedconsts.OpCopyTo:        {},
		sharedconsts.OpPasteFrom:     {},
		sharedconsts.OpPrefix:        {},
		sharedconsts.OpReplacePrefix: {},
		sharedconsts.OpReplaceSuffix: {},
		sharedconsts.OpReplace:       {},
		sharedconsts.OpSet:           {},
		sharedconsts.OpDateTag:       {},
		sharedconsts.OpDeleteDateTag: {},
	}

	// Validate each meta operation
	for i, op := range metaOps {
		// Validate operation type is valid
		if _, ok := validMetaActions[op.OpType]; !ok {
			return fmt.Errorf("invalid meta operation type %q at position %d (valid: append, copy-to, paste-from, prefix, replace-prefix, replace-suffix, replace, set, date-tag, delete-date-tag)", op.OpType, i)
		}

		// Validate date-tag operations
		if op.OpType == sharedconsts.OpDateTag {
			if op.OpLoc != sharedconsts.OpLocPrefix && op.OpLoc != sharedconsts.OpLocSuffix {
				return fmt.Errorf("invalid date tag location %q at position %d, use prefix or suffix", op.OpLoc, i)
			}
			if !ValidateDateFormat(op.DateFormat) {
				return fmt.Errorf("invalid date tag format %q at position %d", op.DateFormat, i)
			}
		}

		// Validate delete-date-tag operations
		if op.OpType == sharedconsts.OpDeleteDateTag {
			if op.OpLoc != sharedconsts.OpLocPrefix && op.OpLoc != sharedconsts.OpLocSuffix && op.OpLoc != sharedconsts.OpLocAll {
				return fmt.Errorf("invalid date tag location %q at position %d, use prefix, suffix, or all", op.OpLoc, i)
			}
			if !ValidateDateFormat(op.DateFormat) {
				return fmt.Errorf("invalid date tag format %q at position %d", op.DateFormat, i)
			}
		}

		// Validate field is not empty for all operations
		if op.Field == "" {
			return fmt.Errorf("meta operation at position %d has empty field", i)
		}
	}

	return nil
}

// ValidateRenameFlag validates the rename style to apply.
func ValidateRenameFlag(flag string) error {
	if flag == "" {
		return nil
	}

	// Normalize
	flag = sharedvalidation.GetRenameFlag(flag)

	switch flag {
	case sharedconsts.RenameFixesOnly, sharedconsts.RenameSkip, sharedconsts.RenameSpaces, sharedconsts.RenameUnderscores:
		return nil
	default:
		return fmt.Errorf("invalid renaming style %q, accept: %v", flag, sharedconsts.ValidRenameFlags)
	}
}

// ValidateDateFormat returns the date format enum type.
func ValidateDateFormat(dateFmt string) bool {
	if len(dateFmt) > 2 {
		switch dateFmt {
		case "Ymd", "ymd", "Ydm", "ydm", "dmY", "dmy", "mdY", "mdy", "md", "dm":
			return true
		}
	}
	logger.Pl.E("Invalid date format entered as %q, please enter up to three characters (where 'Y' is yyyy and 'y' is yy)", dateFmt)
	return false
}

// ValidatePurgeMetafiles checks and sets the type of metafile purge to perform.
func ValidatePurgeMetafiles(purgeType string) bool {
	purgeType = strings.TrimSpace(purgeType)
	purgeType = strings.ToLower(purgeType)
	purgeType = strings.ReplaceAll(purgeType, ".", "")

	switch purgeType {
	case "all", "json", "nfo":
		logger.Pl.P("Purge metafiles post-Metarr: %s\n", purgeType)
		return true
	}
	return false
}

// ValidateGPUAcceleration validates the GPU selection.
func ValidateGPUAcceleration(g string) (gpuType string, err error) {
	g = strings.ToLower(strings.TrimSpace(g))

	// Verify OS support.
	if !sharedvalidation.OSSupportsAccelType(g) {
		logger.Pl.W("OS does not support acceleration of type %q, omitting.", g)
		g = ""
	}

	// Return on empty or if explicitly unwanted.
	if g == "" || g == "none" {
		return "", nil
	}

	// Validate acceleration type.
	if g, err = sharedvalidation.ValidateGPUAccelType(g); err != nil {
		return "", err
	}

	return g, nil
}

// ValidateAudioTranscodeCodecSlice sets mappings for audio codec inputs.
func ValidateAudioTranscodeCodecSlice(pairs []string) (validPairs []string, err error) {
	// Deduplicate
	dedupPairs := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		if !slices.Contains(dedupPairs, p) {
			dedupPairs = append(dedupPairs, p)
		}
	}

	// Iterate deduped pairs
	for _, p := range dedupPairs {
		split := strings.Split(p, ":")
		if len(split) < 1 { // Impossible condition, added to silence nilaway
			continue
		}

		if _, err := sharedvalidation.ValidateAudioCodec(split[0]); err != nil {
			return nil, err
		}

		// Singular value, apply to every entry
		if len(split) < 2 {
			validPairs = append(validPairs, p)
			continue
		}

		// Multi value entry, apply specific output to specific input
		output := split[1]
		if _, err = sharedvalidation.ValidateAudioCodec(output); err != nil {
			return nil, err
		}
		validPairs = append(validPairs, p)
	}

	logger.Pl.D(3, "Got audio codec array: %v", validPairs)
	return validPairs, nil
}

// ValidateVideoTranscodeCodecSlice validates the input video transcode slice.
func ValidateVideoTranscodeCodecSlice(pairs []string, accel string) (validPairs []string, err error) {
	// Deduplicate
	dedupPairs := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		if !slices.Contains(dedupPairs, p) {
			dedupPairs = append(dedupPairs, p)
		}
	}

	// Iterate deduped pairs
	for _, p := range dedupPairs {
		split := strings.Split(p, ":")
		if len(split) < 1 { // Impossible condition, added to silence nilaway
			continue
		}

		if _, err := sharedvalidation.ValidateVideoCodecWithAccel(split[0], accel); err != nil {
			return nil, err
		}

		// Singular value, apply to every entry
		if len(split) < 2 {
			validPairs = append(validPairs, p)
			continue
		}

		// Multi value entry, apply specific output to specific input
		output := split[1]
		if _, err = sharedvalidation.ValidateVideoCodecWithAccel(output, accel); err != nil {
			return nil, err
		}

		validPairs = append(validPairs, p)
	}

	logger.Pl.D(3, "Got video codec array: %v", validPairs)
	return validPairs, nil
}

// ValidateTranscodeVideoFilter validates the transcode video filter preset.
func ValidateTranscodeVideoFilter(q string) (vf string, err error) {
	logger.Pl.D(1, "No checks in place for transcode video filter...")
	return q, nil
}
