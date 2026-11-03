package validation

import (
	"maps"
	"slices"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedmodels"
	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ValidateEncoderArgLines returns an error if "codec:args" lines hold something that can't work, whatever other settings they end up with.
func ValidateEncoderArgLines(lines []string) error {
	parsed, err := sharedparsing.ParseVideoEncoderArgs(lines)
	if err != nil {
		return err
	}

	// Check each codec's line, in a set order so the same settings always give the same error.
	for _, codec := range slices.Sorted(maps.Keys(parsed)) {
		if err := sharedvalidation.ValidateEncoderArgsUsed(codec, parsed[codec]); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMetarrEncoderArgs returns an error if Metarr args, as Metarr gets them, hold encoder settings that can't work.
func ValidateMetarrEncoderArgs(m *models.MetarrArgs) error {
	return sharedparsing.CheckEncoderSettings(sharedmodels.EncoderSettings{
		VideoArgs:   m.TranscodeVideoArgs,
		VideoCodecs: m.TranscodeVideoCodecs,
		AudioCodecs: m.TranscodeAudioCodecs,
		ExtraArgs:   m.ExtraFFmpegArgs,
		OutputExt:   m.OutputExt,

		// A GPU encoder takes other values, and which one runs isn't known here. Auto always encodes in software.
		Software: m.TranscodeGPU == "" || m.TranscodeGPU == GPUNone || m.TranscodeGPU == sharedconsts.AccelTypeAuto,
	})
}
