package validation

import (
	"fmt"
	"slices"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedenums"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ValidateFilenameOps validates filename transformation operation models.
func ValidateFilenameOps(filenameOps []models.FilenameOps) error {
	logger.Pl.D(5, "Validating %d filename operations...", len(filenameOps))
	return sharedvalidation.ValidateFilenameOps(filenameOps)
}

// ValidateMetaOps validates meta transformation operation models.
func ValidateMetaOps(metaOps []models.MetaOps) error {
	logger.Pl.D(5, "Validating %d meta operations...", len(metaOps))
	return sharedvalidation.ValidateMetaOps(metaOps)
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

// ValidateDateFormat reports whether dateFmt is a date format directive Metarr accepts.
func ValidateDateFormat(dateFmt string) bool {
	if _, err := sharedenums.ParseDateFormat(dateFmt); err != nil {
		logger.Pl.E("%v", err)
		return false
	}
	return true
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

	logger.Pl.D(5, "Got audio codec array: %v", validPairs)
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

	logger.Pl.D(5, "Got video codec array: %v", validPairs)
	return validPairs, nil
}

// ValidateTranscodeVideoFilter validates the transcode video filter preset.
func ValidateTranscodeVideoFilter(q string) (vf string, err error) {
	logger.Pl.D(1, "No checks in place for transcode video filter...")
	return q, nil
}
