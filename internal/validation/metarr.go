package validation

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedenums"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ValidateRenameFlag validates the rename style to apply.
func ValidateRenameFlag(flag string) error {
	if flag == "" {
		return nil
	}

	switch sharedvalidation.GetRenameFlag(flag) {
	case sharedconsts.RenameFixesOnly, sharedconsts.RenameSkip, sharedconsts.RenameSpaces, sharedconsts.RenameUnderscores:
		return nil
	default:
		return fmt.Errorf("invalid renaming style %q, accept: %v", flag, slices.Sorted(maps.Keys(sharedconsts.ValidRenameFlags)))
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

// ValidatePurgeMetafiles returns the normalized type of metafile purge for Metarr to perform: all, json, nfo or "".
func ValidatePurgeMetafiles(purgeType string) (string, error) {
	purgeType = strings.TrimSpace(purgeType)
	purgeType = strings.ToLower(purgeType)
	purgeType = strings.ReplaceAll(purgeType, ".", "")

	switch purgeType {
	case "", "all", "json", "nfo":
		return purgeType, nil
	}
	return "", fmt.Errorf("invalid metadata purge type %q (all, json or nfo)", purgeType)
}

// GPUNone turns off a GPU set by the channel (or channel URL), so Metarr encodes in software.
const GPUNone = "none"

// ValidateGPUAcceleration validates the GPU selection, keeping GPUNone so it overrides an inherited GPU.
func ValidateGPUAcceleration(g string) (gpuType string, err error) {
	g = strings.ToLower(strings.TrimSpace(g))
	if g == "" || g == GPUNone {
		return g, nil
	}

	// Verify OS support.
	if !sharedvalidation.OSSupportsAccelType(g) {
		logger.Pl.W("OS does not support acceleration of type %q, omitting.", g)
		return "", nil
	}

	// Validate acceleration type.
	if g, err = sharedvalidation.ValidateGPUAccelType(g); err != nil {
		return "", err
	}

	return g, nil
}

// ValidateAudioTranscodeCodecSlice validates audio codec entries, returning them deduplicated as typed.
func ValidateAudioTranscodeCodecSlice(pairs []string) (validPairs []string, err error) {
	dedupPairs := dedupSlice(pairs)

	// Check the plain codec and pairs as Metarr will read them.
	if _, _, err = sharedvalidation.ParseCodecMappings(dedupPairs, sharedvalidation.ValidateAudioCodec); err != nil {
		return nil, err
	}

	logger.Pl.D(5, "Got audio codec array: %v", dedupPairs)
	return dedupPairs, nil
}

// ValidateVideoTranscodeCodecSlice validates video codec entries, returning them deduplicated as typed.
func ValidateVideoTranscodeCodecSlice(pairs []string, accel string) (validPairs []string, err error) {
	dedupPairs := dedupSlice(pairs)

	// Check the plain codec and pairs as Metarr will read them.
	validate := func(c string) (string, error) {
		// NOTE: var inputs into the function below doesn't affect
		// the above signature, only its return value matters.
		return sharedvalidation.ValidateVideoCodecWithAccel(c, accel)
	}
	if _, _, err = sharedvalidation.ParseCodecMappings(dedupPairs, validate); err != nil {
		return nil, err
	}

	logger.Pl.D(5, "Got video codec array: %v", dedupPairs)
	return dedupPairs, nil
}

// dedupSlice deduplicates entries in a slice so each entry only appears once.
func dedupSlice(pairs []string) []string {
	dedup := make([]string, 0, len(pairs))

	for _, p := range pairs {
		if p == "" {
			continue
		}
		if !slices.Contains(dedup, p) {
			dedup = append(dedup, p)
		}
	}

	return dedup
}
