package validation

import (
	"fmt"
	"strings"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ValidateSettingsModel validates correct Settings values.
//
// Ensures all settings are valid before storage/after retrieval. checkPaths checks every file and directory, done when
// adding a channel: updates check only those they add or change, and loading none, so one unavailable path (e.g. a
// deleted filter file) can't stop a channel saving or loading.
func ValidateSettingsModel(s *models.Settings, checkPaths bool) error {
	var err error

	s.DownloadConcurrency = max(s.DownloadConcurrency, 0)
	s.CrawlFreq = max(s.CrawlFreq, 0)

	if checkPaths && s.FilterFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(s.FilterFile, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid filter file %q in settings: %w", s.FilterFile, err)
		}
	}

	if s.Filters != nil {
		if err = ValidateFilterOps(s.Filters); err != nil {
			return fmt.Errorf("invalid filter ops in settings: %w", err)
		}
	}

	if s.FromDate != "" {
		if s.FromDate, err = ValidateToFromDate(s.FromDate); err != nil {
			return fmt.Errorf("invalid from date %q in settings: %w", s.FromDate, err)
		}
	}

	if checkPaths && s.JSONDir != "" {
		if err = CheckDirectory(s.JSONDir, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid JSON directory %q in settings: %w", s.JSONDir, err)
		}
		if err = CheckDestination(s.JSONDir); err != nil {
			return fmt.Errorf("invalid JSON directory %q in settings: %w", s.JSONDir, err)
		}
	}

	if s.MaxFilesize != "" {
		if s.MaxFilesize, err = ValidateMaxFilesize(s.MaxFilesize); err != nil {
			return fmt.Errorf("invalid max filesize %q in settings: %w", s.MaxFilesize, err)
		}
	}

	if checkPaths && s.MetaFilterMoveOpFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(s.MetaFilterMoveOpFile, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid move op file %q in settings: %w", s.MetaFilterMoveOpFile, err)
		}
	}

	if s.MetaFilterMoveOps != nil {
		if err = ValidateMetaFilterMoveOps(s.MetaFilterMoveOps, checkPaths); err != nil {
			return fmt.Errorf("invalid move ops in settings: %w", err)
		}
	}

	s.Retries = max(s.Retries, 0)
	if s.ToDate != "" {
		if s.ToDate, err = ValidateToFromDate(s.ToDate); err != nil {
			return fmt.Errorf("invalid to date %q in settings: %w", s.ToDate, err)
		}
	}

	if checkPaths && s.VideoDir != "" {
		if err = CheckDirectory(s.VideoDir, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid video directory %q in settings: %w", s.VideoDir, err)
		}
		if err = CheckDestination(s.VideoDir); err != nil {
			return fmt.Errorf("invalid video directory %q in settings: %w", s.VideoDir, err)
		}
	}

	if s.YtdlpOutputExt != "" {
		s.YtdlpOutputExt = strings.ToLower(s.YtdlpOutputExt)
		if err = ValidateYtdlpOutputExtension(s.YtdlpOutputExt); err != nil {
			return fmt.Errorf("invalid YT-DLP extension %q in settings: %w", s.YtdlpOutputExt, err)
		}
	}

	return nil
}

// ValidateMetarrArgsCombination returns an error if Metarr args, as Metarr gets them (a URL's over its channel's, or
// Download As options over those), can't work together: codecs their output container can't hold, or a GPU without
// the device node it needs.
func ValidateMetarrArgsCombination(m *models.MetarrArgs) error {
	if m.OutputExt != "" && m.OutputExt != sharedconsts.ExtSame {
		if err := sharedvalidation.ValidateCodecsForContainer(m.OutputExt, m.TranscodeVideoCodecs, m.TranscodeAudioCodecs); err != nil {
			return err
		}
	}
	if m.TranscodeGPUNode == "" {
		if _, err := sharedvalidation.ValidateAccelTypeDeviceNode(m.TranscodeGPU, ""); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMetarrArgsModel validates correct Metarr Args values.
//
// Ensures all metarr settings are valid before storage/after retrieval. checkPaths is as for ValidateSettingsModel.
func ValidateMetarrArgsModel(m *models.MetarrArgs, checkPaths bool) error {
	m.MetarrConcurrency = max(m.MetarrConcurrency, 0) // 0 inherits the channel's limit, or the global one.

	// Meta ops
	// Filtered:
	if m.FilteredMetaOps != nil {
		if err := sharedvalidation.ValidateFilteredMetaOps(m.FilteredMetaOps); err != nil {
			return fmt.Errorf("invalid filtered meta ops in Metarr settings: %w", err)
		}
	}
	if checkPaths && m.FilteredMetaOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.FilteredMetaOpsFile, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid filtered meta ops file %q in Metarr settings: %w", m.FilteredMetaOpsFile, err)
		}
	}
	// Unfiltered:
	if m.MetaOps != nil {
		if err := sharedvalidation.ValidateMetaOps(m.MetaOps); err != nil {
			return fmt.Errorf("invalid meta op(s) in Metarr settings: %w", err)
		}
	}
	if checkPaths && m.MetaOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.MetaOpsFile, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid meta ops file %q in Metarr settings: %w", m.MetaOpsFile, err)
		}
	}

	// Filename ops
	// Filtered:
	if m.FilteredFilenameOps != nil {
		if err := sharedvalidation.ValidateFilteredFilenameOps(m.FilteredFilenameOps); err != nil {
			return fmt.Errorf("invalid filtered filename ops in Metarr settings: %w", err)
		}
	}
	if checkPaths && m.FilteredFilenameOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.FilteredFilenameOpsFile, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid filtered filename ops file %q in Metarr settings: %w", m.FilteredFilenameOpsFile, err)
		}
	}
	// Unfiltered:
	if m.FilenameOps != nil {
		if err := sharedvalidation.ValidateFilenameOps(m.FilenameOps); err != nil {
			return fmt.Errorf("invalid filename ops in Metarr settings: %w", err)
		}
	}
	if checkPaths && m.FilenameOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.FilenameOpsFile, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid filename ops file %q in Metarr settings: %w", m.FilenameOpsFile, err)
		}
	}

	// GPU arguments
	if m.TranscodeGPU != "" {
		if _, err := ValidateGPUAcceleration(m.TranscodeGPU); err != nil {
			return fmt.Errorf("invalid GPU config in Metarr Settings: %w", err)
		}
	}
	if checkPaths && m.TranscodeGPUNode != "" {
		if _, err := sharedvalidation.ValidateAccelTypeDeviceNode(m.TranscodeGPU, m.TranscodeGPUNode); err != nil {
			return fmt.Errorf("invalid GPU node config in Metarr Settings: %w", err)
		}
	}

	// System resources
	m.MaxCPU = sharedvalidation.ValidateMaxCPU(m.MaxCPU, true)
	if m.MinFreeMem != "" {
		if _, err := sharedvalidation.ValidateMinFreeMem(m.MinFreeMem); err != nil {
			return fmt.Errorf("invalid minimum free memory in Metarr settings: %w", err)
		}
	}

	// File output
	if m.OutputExt != "" && m.OutputExt != sharedconsts.ExtSame {
		m.OutputExt = strings.ToLower(m.OutputExt)
		if _, err := sharedvalidation.ValidateFFmpegOutputExt(m.OutputExt); err != nil {
			return fmt.Errorf("invalid output filetype in Metarr settings: %w", err)
		}
	}
	if m.RenameStyle != "" {
		if err := ValidateRenameFlag(m.RenameStyle); err != nil {
			return fmt.Errorf("invalid rename flag in Metarr settings: %w", err)
		}
	}
	if checkPaths && m.OutputDir != "" {
		if err := CheckDirectory(m.OutputDir, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid output directory in Metarr settings: %w", err)
		}
		if err := CheckDestination(m.OutputDir); err != nil {
			return fmt.Errorf("invalid output directory in Metarr settings: %w", err)
		}
	}
	var err error
	if m.MetaPurge, err = ValidatePurgeMetafiles(m.MetaPurge); err != nil {
		return fmt.Errorf("invalid metadata purge in Metarr settings: %w", err)
	}

	// Codecs
	if m.TranscodeAudioCodecs != nil {
		if _, err := ValidateAudioTranscodeCodecSlice(m.TranscodeAudioCodecs); err != nil {
			return fmt.Errorf("invalid audio codec(s) in Metarr settings: %w", err)
		}
	}
	if m.TranscodeVideoCodecs != nil {
		if _, err := ValidateVideoTranscodeCodecSlice(m.TranscodeVideoCodecs, ""); err != nil {
			return fmt.Errorf("invalid video codec(s) in Metarr settings: %w", err)
		}
	}

	return nil
}
