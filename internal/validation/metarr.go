package validation

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
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

// ValidatePurgeMetafiles returns the normalized type of metafile purge for Metarr to perform: keep, all, json, nfo or "".
func ValidatePurgeMetafiles(purgeType string) (string, error) {
	purgeType = strings.TrimSpace(purgeType)
	purgeType = strings.ToLower(purgeType)
	purgeType = strings.ReplaceAll(purgeType, ".", "")

	switch purgeType {
	case "", "keep", "all", "json", "nfo":
		return purgeType, nil
	}
	return "", fmt.Errorf("invalid metadata purge type %q (keep, all, json or nfo)", purgeType)
}

// GPUNone turns off a GPU set by the channel (or channel URL), so Metarr encodes in software.
const GPUNone = "none"

// ValidateGPUAcceleration validates the GPU selection, keeping GPUNone so it overrides an inherited GPU.
func ValidateGPUAcceleration(g string) (gpuType string, err error) {
	g = strings.ToLower(strings.TrimSpace(g))
	if g == "" || g == GPUNone {
		return g, nil
	}

	// Validate acceleration type.
	if g, err = sharedvalidation.ValidateGPUAccelType(g); err != nil {
		return "", err
	}

	// Verify OS support.
	if !sharedvalidation.OSSupportsAccelType(g) {
		logger.Pl.W("OS does not support acceleration of type %q, omitting.", g)
		return "", nil
	}

	return g, nil
}

// ValidateAudioTranscodeCodecSlice validates audio codec entries, returning them deduplicated as typed.
func ValidateAudioTranscodeCodecSlice(pairs []string) (validPairs []string, err error) {
	dedupPairs := dedupSlice(pairs)

	// Check the plain codec and pairs as Metarr will read them.
	if _, _, err = sharedparsing.ParseCodecMappings(dedupPairs, sharedvalidation.ValidateAudioCodec); err != nil {
		return nil, err
	}
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
	if _, _, err = sharedparsing.ParseCodecMappings(dedupPairs, validate); err != nil {
		return nil, err
	}
	return dedupPairs, nil
}

// ValidateVideoEncoderArgs validates "codec:args" lines as Metarr will read them, returning them deduplicated as typed.
func ValidateVideoEncoderArgs(lines []string) (validLines []string, err error) {
	dedupLines := dedupSlice(lines)
	if _, err := sharedparsing.ParseVideoEncoderArgs(dedupLines); err != nil {
		return nil, err
	}
	return dedupLines, nil
}

// ValidateAudioEncoderArgs validates "codec:args" lines for audio codecs as Metarr will read them, returning them deduplicated as typed.
func ValidateAudioEncoderArgs(lines []string) (validLines []string, err error) {
	dedupLines := dedupSlice(lines)
	if _, err := sharedparsing.ParseAudioEncoderArgs(dedupLines); err != nil {
		return nil, err
	}
	return dedupLines, nil
}

// ValidateExtraFFmpegArgs refuses extra FFmpeg args holding a blocked option, or one that only works when encoding.
func ValidateExtraFFmpegArgs(a string) error {
	args, err := sharedparsing.ShellSplit(a)
	if err != nil {
		return err
	}
	if !sharedconsts.InsecureArgumentsAllowed() {
		if blocked, option := sharedconsts.HasBlockedOptions(args, sharedconsts.BlockedFFmpegOptions); blocked {
			return fmt.Errorf("option %q is not allowed", option)
		}
	}
	if option, found := sharedparsing.EncoderOnlyOption(args); found {
		return fmt.Errorf("%s only works when encoding a video, so it goes in Video Encoder Args (%s) instead, e.g. av1:%s",
			option, keys.TranscodeVideoArgs, option)
	}
	return nil
}

// dedupSlice drops blank entries, then deduplicates the rest so each entry only appears once.
func dedupSlice(pairs []string) []string {
	nonBlank := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p != "" {
			nonBlank = append(nonBlank, p)
		}
	}
	dedup, _ := sharedparsing.Deduplicate(nonBlank)
	return dedup
}
