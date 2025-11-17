package validation

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

var validMetaActions = map[string]bool{
	"append": true, "copy-to": true, "paste-from": true, "prefix": true,
	"replace-prefix": true, "replace-suffix": true, "replace": true, "set": true,
	"date-tag": true, "delete-date-tag": true,
}

var validFilenameActions = map[string]bool{
	"append": true, "prefix": true, "replace-prefix": true, "replace-suffix": true,
	"replace": true, "date-tag": true, "delete-date-tag": true, "set": true,
}

// ValidateFilenameOps validates filename transformation operation models.
func ValidateFilenameOps(filenameOps []models.FilenameOps) error {
	if len(filenameOps) == 0 {
		logger.Pl.D(4, "No filename operations to validate")
		return nil
	}

	logger.Pl.D(1, "Validating %d filename operations...", len(filenameOps))

	// Validate each filename operation
	for i, op := range filenameOps {
		// Validate operation type is valid
		if !validFilenameActions[op.OpType] {
			return fmt.Errorf("invalid filename operation type %q at position %d (valid: append, prefix, replace-prefix, replace-suffix, replace, date-tag, delete-date-tag, set)", op.OpType, i)
		}

		// Validate date-tag operations
		if op.OpType == "date-tag" {
			if op.OpLoc != "prefix" && op.OpLoc != "suffix" {
				return fmt.Errorf("invalid date tag location %q at position %d, use prefix or suffix", op.OpLoc, i)
			}
			if !ValidateDateFormat(op.DateFormat) {
				return fmt.Errorf("invalid date tag format %q at position %d", op.DateFormat, i)
			}
		}

		// Validate delete-date-tag operations
		if op.OpType == "delete-date-tag" {
			if op.OpLoc != "prefix" && op.OpLoc != "suffix" && op.OpLoc != "all" {
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

	logger.Pl.D(1, "Validating %d meta operations...", len(metaOps))

	// Validate each meta operation
	for i, op := range metaOps {
		// Validate operation type is valid
		if !validMetaActions[op.OpType] {
			return fmt.Errorf("invalid meta operation type %q at position %d (valid: append, copy-to, paste-from, prefix, replace-prefix, replace-suffix, replace, set, date-tag, delete-date-tag)", op.OpType, i)
		}

		// Validate date-tag operations
		if op.OpType == "date-tag" {
			if op.OpLoc != "prefix" && op.OpLoc != "suffix" {
				return fmt.Errorf("invalid date tag location %q at position %d, use prefix or suffix", op.OpLoc, i)
			}
			if !ValidateDateFormat(op.DateFormat) {
				return fmt.Errorf("invalid date tag format %q at position %d", op.DateFormat, i)
			}
		}

		// Validate delete-date-tag operations
		if op.OpType == "delete-date-tag" {
			if op.OpLoc != "prefix" && op.OpLoc != "suffix" && op.OpLoc != "all" {
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

// ValidateMetarrOutputExt verifies the output filetype is valid for FFmpeg.
func ValidateMetarrOutputExt(o string) (dottedExt string, err error) {
	o = strings.ToLower(strings.TrimSpace(o))
	if !strings.HasPrefix(o, ".") {
		o = "." + o
	}

	logger.Pl.D(4, "Checking Metarr output filetype: %q", o)

	valid := false
	for _, ext := range consts.AllVidExtensions {
		if o != ext {
			continue
		}
		valid = true
		break
	}

	if valid {
		return o, nil
	}
	return "", fmt.Errorf("output filetype %q is not supported", o)
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

// ValidateGPU validates the GPU selection.
func ValidateGPU(g, devDir string) (gpuType string, dir string, err error) {
	g = strings.ToLower(strings.TrimSpace(g))
	if g == "" || g == "none" {
		return "", "", nil
	}

	// Validate acceleration type
	if g, err = sharedvalidation.ValidateGPUAccelType(g); err != nil {
		return "", "", err
	}

	// Check device directory exists
	if devDir != "" {
		if _, err := os.Stat(devDir); err != nil {
			return "", "", fmt.Errorf("cannot access driver location %q: %w", devDir, err)
		}
	}

	return g, devDir, nil
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

	logger.Pl.D(1, "Got audio codec array: %v", validPairs)
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

	logger.Pl.D(1, "Got video codec array: %v", validPairs)
	return validPairs, nil
}

// ValidateTranscodeVideoFilter validates the transcode video filter preset.
func ValidateTranscodeVideoFilter(q string) (vf string, err error) {
	logger.Pl.D(1, "No checks in place for transcode video filter...")
	return q, nil
}
