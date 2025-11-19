package validation

import (
	"fmt"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ValidateSettingsModel validates correct Settings values.
//
// Ensures all settings are valid before storage/after retrieval.
func ValidateSettingsModel(s *models.Settings) error {
	var err error

	s.Concurrency = sharedvalidation.ValidateConcurrencyLimit(s.Concurrency)
	s.CrawlFreq = max(s.CrawlFreq, 0)

	if s.FilterFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(s.FilterFile, false, sharedtemplates.AllTemplatesMap); err != nil {
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

	if s.JSONDir != "" {
		if _, _, err = sharedvalidation.ValidateDirectory(s.JSONDir, true, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid JSON directory %q in settings: %w", s.JSONDir, err)
		}
	}

	if s.MaxFilesize != "" {
		if s.MaxFilesize, err = ValidateMaxFilesize(s.MaxFilesize); err != nil {
			return fmt.Errorf("invalid max filesize %q in settings: %w", s.MaxFilesize, err)
		}
	}

	if s.MetaFilterMoveOpFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(s.MetaFilterMoveOpFile, false, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid move op file %q in settings: %w", s.MetaFilterMoveOpFile, err)
		}
	}

	if s.MetaFilterMoveOps != nil {
		if err = ValidateMetaFilterMoveOps(s.MetaFilterMoveOps); err != nil {
			return fmt.Errorf("invalid move ops in settings: %w", err)
		}
	}

	s.Retries = max(s.Retries, 0)
	if s.ToDate != "" {
		if s.ToDate, err = ValidateToFromDate(s.ToDate); err != nil {
			return fmt.Errorf("invalid to date %q in settings: %w", s.ToDate, err)
		}
	}

	if s.VideoDir != "" {
		if _, _, err = sharedvalidation.ValidateDirectory(s.VideoDir, true, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid video directory %q in settings: %w", s.VideoDir, err)
		}
	}

	if s.YtdlpOutputExt != "" {
		if err = ValidateYtdlpOutputExtension(s.YtdlpOutputExt); err != nil {
			return fmt.Errorf("invalid YT-DLP extension %q in settings: %w", s.YtdlpOutputExt, err)
		}
	}

	return nil
}

// ValidateMetarrArgsModel validates correct Metarr Args values.
//
// Ensures all metarr settings are valid before storage/after retrieval.
func ValidateMetarrArgsModel(m *models.MetarrArgs) error {
	m.Concurrency = sharedvalidation.ValidateConcurrencyLimit(m.Concurrency)

	if m.FilenameOps != nil {
		if err := ValidateFilenameOps(m.FilenameOps); err != nil {
			return fmt.Errorf("invalid filename ops in Metarr settings: %w", err)
		}
	}

	if m.FilteredMetaOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.FilteredMetaOpsFile, false, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid filtered meta ops file %q in Metarr settings: %w", m.FilteredMetaOpsFile, err)
		}
	}

	if m.FilteredFilenameOps != nil {
		if err := ValidateFilteredFilenameOps(m.FilteredFilenameOps); err != nil {
			return fmt.Errorf("invalid filtered filename ops in Metarr settings: %w", err)
		}
	}

	if m.FilteredFilenameOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.FilteredFilenameOpsFile, false, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid filtered filename ops file %q in Metarr settings: %w", m.FilteredFilenameOpsFile, err)
		}
	}

	if m.FilteredMetaOps != nil {
		if err := ValidateFilteredMetaOps(m.FilteredMetaOps); err != nil {
			return fmt.Errorf("invalid filtered meta ops in Metarr settings: %w", err)
		}
	}

	if m.FilteredMetaOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.FilteredMetaOpsFile, false, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid filtered meta op(s) file %q in Metarr settings: %w", m.FilteredMetaOpsFile, err)
		}
	}

	if m.TranscodeGPU != "" {
		if _, _, err := ValidateGPU(m.TranscodeGPU, m.TranscodeGPUDirectory); err != nil {
			return fmt.Errorf("invalid GPU config in Metarr Settings: %w", err)
		}
	}

	m.MaxCPU = sharedvalidation.ValidateMaxCPU(m.MaxCPU, true)

	if m.MetaOps != nil {
		if err := ValidateMetaOps(m.MetaOps); err != nil {
			return fmt.Errorf("invalid meta op(s) in Metarr settings: %w", err)
		}
	}

	if m.MetaOpsFile != "" {
		if _, _, err := sharedvalidation.ValidateFile(m.MetaOpsFile, false, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid meta ops file %q in Metarr settings: %w", m.MetaOpsFile, err)
		}
	}

	if m.MinFreeMem != "" {
		if _, err := sharedvalidation.ValidateMinFreeMem(m.MinFreeMem); err != nil {
			return fmt.Errorf("invalid minimum free memory in Metarr settings: %w", err)
		}
	}

	if m.OutputExt != "" {
		if _, err := ValidateMetarrOutputExt(m.OutputExt); err != nil {
			return fmt.Errorf("invalid output filetype in Metarr settings: %w", err)
		}
	}

	if m.RenameStyle != "" {
		if err := ValidateRenameFlag(m.RenameStyle); err != nil {
			return fmt.Errorf("invalid rename flag in Metarr settings: %w", err)
		}
	}

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

	if m.OutputDirMap != nil {
		if err := ValidateMetarrOutputDirs(m.OutputDirMap); err != nil {
			return fmt.Errorf("invalid output dir(s) in Metarr settings: %w", err)
		}
	}

	return nil
}
