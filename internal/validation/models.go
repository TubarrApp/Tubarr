package validation

import (
	"fmt"
	"tubarr/internal/models"
)

// ValidateSettingsModel validates correct Settings values.
//
// Ensures all settings are valid before storage/after retrieval.
func ValidateSettingsModel(s *models.Settings) error {
	var err error

	s.Concurrency = max(s.Concurrency, 0)
	s.CrawlFreq = max(s.CrawlFreq, 0)

	if s.FilterFile != "" {
		if _, err := ValidateFile(s.FilterFile, false); err != nil {
			return fmt.Errorf("invalid filter file %q in settings: %v", s.FilterFile, err)
		}
	}

	if s.Filters != nil {
		if err = ValidateFilterOps(s.Filters); err != nil {
			return fmt.Errorf("invalid filter ops in settings: %v", err)
		}
	}

	if s.FromDate != "" {
		if s.FromDate, err = ValidateToFromDate(s.FromDate); err != nil {
			return fmt.Errorf("invalid from date %q in settings: %v", s.FromDate, err)
		}
	}

	if s.JSONDir != "" {
		if _, err = ValidateDirectory(s.JSONDir, true); err != nil {
			return fmt.Errorf("invalid JSON directory %q in settings: %v", s.JSONDir, err)
		}
	}

	if s.MaxFilesize != "" {
		if s.MaxFilesize, err = ValidateMaxFilesize(s.MaxFilesize); err != nil {
			return fmt.Errorf("invalid max filesize %q in settings: %v", s.MaxFilesize, err)
		}
	}

	if s.MetaFilterMoveOpFile != "" {
		if _, err := ValidateFile(s.MetaFilterMoveOpFile, false); err != nil {
			return fmt.Errorf("invalid move op file %q in settings: %v", s.MetaFilterMoveOpFile, err)
		}
	}

	if s.MetaFilterMoveOps != nil {
		if err = ValidateMetaFilterMoveOps(s.MetaFilterMoveOps); err != nil {
			return fmt.Errorf("invalid move ops in settings: %v", err)
		}
	}

	s.Retries = max(s.Retries, 0)

	if s.ToDate != "" {
		if s.ToDate, err = ValidateToFromDate(s.ToDate); err != nil {
			return fmt.Errorf("invalid to date %q in settings: %v", s.ToDate, err)
		}
	}

	if s.VideoDir != "" {
		if _, err = ValidateDirectory(s.VideoDir, true); err != nil {
			return fmt.Errorf("invalid video directory %q in settings: %v", s.VideoDir, err)
		}
	}

	if s.YtdlpOutputExt != "" {
		if err = ValidateYtdlpOutputExtension(s.YtdlpOutputExt); err != nil {
			return fmt.Errorf("invalid YT-DLP extension %q in settings: %v", s.YtdlpOutputExt, err)
		}
	}

	return nil
}

// ValidateMetarrArgsModel validates correct Metarr Args values.
//
// Ensures all metarr settings are valid before storage/after retrieval.
func ValidateMetarrArgsModel(m *models.MetarrArgs) error {
	m.Concurrency = max(m.Concurrency, 0)

	if m.FilenameOps != nil {
		if err := ValidateFilenameOps(m.FilenameOps); err != nil {
			return fmt.Errorf("invalid filename ops in Metarr settings: %v", err)
		}
	}

	if m.FilteredMetaOpsFile != "" {
		if _, err := ValidateFile(m.FilteredMetaOpsFile, false); err != nil {
			return fmt.Errorf("invalid filtered meta ops file %q in Metarr settings: %v", m.FilteredMetaOpsFile, err)
		}
	}

	if m.FilteredFilenameOps != nil {
		if err := ValidateFilteredFilenameOps(m.FilteredFilenameOps); err != nil {
			return fmt.Errorf("invalid filtered filename ops in Metarr settings: %v", err)
		}
	}

	if m.FilteredFilenameOpsFile != "" {
		if _, err := ValidateFile(m.FilteredFilenameOpsFile, false); err != nil {
			return fmt.Errorf("invalid filtered filename ops file %q in Metarr settings: %v", m.FilteredFilenameOpsFile, err)
		}
	}

	if m.FilteredMetaOps != nil {
		if err := ValidateFilteredMetaOps(m.FilteredMetaOps); err != nil {
			return fmt.Errorf("invalid filtered meta ops in Metarr settings: %v", err)
		}
	}

	if m.FilteredMetaOpsFile != "" {
		if _, err := ValidateFile(m.FilteredMetaOpsFile, false); err != nil {
			return fmt.Errorf("invalid filtered meta op(s) file %q in Metarr settings: %v", m.FilteredMetaOpsFile, err)
		}
	}

	if m.UseGPU != "" {
		if _, _, err := ValidateGPU(m.UseGPU, m.GPUDir); err != nil {
			return fmt.Errorf("invalid GPU config in Metarr Settings: %v", err)
		}
	}

	m.MaxCPU = max(m.MaxCPU, 0)

	if m.MetaOps != nil {
		if err := ValidateMetaOps(m.MetaOps); err != nil {
			return fmt.Errorf("invalid meta op(s) in Metarr settings: %v", err)
		}
	}

	if m.MetaOpsFile != "" {
		if _, err := ValidateFile(m.MetaOpsFile, false); err != nil {
			return fmt.Errorf("invalid meta ops file %q in Metarr settings: %v", m.MetaOpsFile, err)
		}
	}

	if m.MinFreeMem != "" {
		if err := ValidateMinFreeMem(m.MinFreeMem); err != nil {
			return fmt.Errorf("invalid minimum free memory in Metarr settings: %v", m.MinFreeMem)
		}
	}

	if m.OutputExt != "" {
		if _, err := ValidateMetarrOutputExt(m.OutputExt); err != nil {
			return fmt.Errorf("invalid output filetype in Metarr settings: %v", err)
		}
	}

	if m.RenameStyle != "" {
		if err := ValidateRenameFlag(m.RenameStyle); err != nil {
			return fmt.Errorf("invalid rename flag in Metarr settings: %v", err)
		}
	}

	if m.TranscodeAudioCodecs != nil {
		if _, err := ValidateAudioTranscodeCodecSlice(m.TranscodeAudioCodecs); err != nil {
			return fmt.Errorf("invalid audio codec(s) in Metarr settings: %v", err)
		}
	}

	if m.TranscodeVideoCodecs != nil {
		if _, err := ValidateVideoTranscodeCodecSlice(m.TranscodeVideoCodecs, ""); err != nil {
			return fmt.Errorf("invalid video codec(s) in Metarr settings: %v", err)
		}
	}

	if m.OutputDirMap != nil {
		if err := ValidateMetarrOutputDirs(m.OutputDirMap); err != nil {
			return fmt.Errorf("invalid output dir(s) in Metarr settings: %v", err)
		}
	}

	return nil
}
