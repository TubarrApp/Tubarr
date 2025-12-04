package models

// Settings are the primary settings for a channel, affecting videos belonging to it.
type Settings struct {
	// Configurations.
	Concurrency int `json:"max_concurrency"`

	// Download-related operations.
	CookiesFromBrowser     string `json:"cookies_from_browser"`
	CrawlFreq              int    `json:"crawl_freq"`
	ExternalDownloader     string `json:"external_downloader"`
	ExternalDownloaderArgs string `json:"external_downloader_args"`
	MaxFilesize            string `json:"max_filesize"`
	Retries                int    `json:"download_retries"`
	UseGlobalCookies       bool   `json:"use_global_cookies"`
	YtdlpOutputExt         string `json:"ytdlp_output_ext"`

	// Custom args
	ExtraYTDLPVideoArgs string `json:"extra_ytdlp_video_args"`
	ExtraYTDLPMetaArgs  string `json:"extra_ytdlp_meta_args"`

	// Metadata operations.
	Filters              []Filters           `json:"filters"`
	FilterFile           string              `json:"filter_file"`
	MetaFilterMoveOps    []MetaFilterMoveOps `json:"move_ops"`
	MetaFilterMoveOpFile string              `json:"move_ops_file"`
	FromDate             string              `json:"from_date"`
	ToDate               string              `json:"to_date"`

	// JSON and video directories.
	JSONDir  string `json:"json_directory"`
	VideoDir string `json:"video_directory"`

	// Channel toggles.
	Paused bool `json:"paused"`
}

// MetarrArgs are the arguments used when calling the Metarr external program.
type MetarrArgs struct {
	// Metarr file operations.
	OutputExt               string                `json:"metarr_output_ext"`
	FilenameOps             []FilenameOps         `json:"metarr_filename_ops"`
	FilenameOpsFile         string                `json:"metarr_filename_ops_file"`
	FilteredFilenameOps     []FilteredFilenameOps `json:"metarr_filtered_filename_ops"`
	FilteredFilenameOpsFile string                `json:"metarr_filtered_filename_ops_file"`
	RenameStyle             string                `json:"metarr_rename_style"`

	// Metarr metadata operations.
	MetaOps             []MetaOps         `json:"metarr_meta_ops"`
	MetaOpsFile         string            `json:"metarr_meta_ops_file"`
	FilteredMetaOps     []FilteredMetaOps `json:"metarr_filtered_meta_ops"`
	FilteredMetaOpsFile string            `json:"metarr_filtered_meta_ops_file"`

	// Metarr output directories.
	OutputDir     string `json:"metarr_output_directory"`
	OutputDirMap  map[string]string
	URLOutputDirs []string `json:"metarr_url_output_directories"`

	// Program operations.
	Concurrency int     `json:"metarr_concurrency"`
	MaxCPU      float64 `json:"metarr_max_cpu_usage"`
	MinFreeMem  string  `json:"metarr_min_free_mem"`

	// FFmpeg transcoding operations.
	TranscodeGPU         string   `json:"metarr_transcode_gpu"`
	TranscodeVideoFilter string   `json:"metarr_transcode_video_filter"`
	TranscodeVideoCodecs []string `json:"metarr_transcode_video_codecs"`
	TranscodeAudioCodecs []string `json:"metarr_transcode_audio_codecs"`
	TranscodeQuality     string   `json:"metarr_transcode_quality"`
	ExtraFFmpegArgs      string   `json:"metarr_extra_ffmpeg_args"`
}

// ChannelAccessDetails holds details related to authentication and cookies.
type ChannelAccessDetails struct {
	Username,
	Password,
	EncryptedPassword,
	LoginURL,
	ChannelURL,
	CookiePath string
}

// FilteredMetaOps allows meta operation entry based on filter matching.
type FilteredMetaOps struct {
	Filters        []Filters
	MetaOps        []MetaOps
	FiltersMatched bool
}

// FilteredFilenameOps allows file operation entry based on filter matching.
type FilteredFilenameOps struct {
	Filters        []Filters
	FilenameOps    []FilenameOps
	FiltersMatched bool
}

// Filters are used to filter in or out videos (download, or operations) by metafields.
type Filters struct {
	ChannelURL    string `json:"filter_url_specific"`
	Field         string `json:"filter_field"`
	ContainsOmits string `json:"filter_type"`
	Value         string `json:"filter_value"`
	MustAny       string `json:"filter_must_any"`
}

// MetaFilterMoveOps are used to set an output directory in Metarr based on matching metadata fields.
type MetaFilterMoveOps struct {
	ChannelURL    string `json:"move_url_specific"`
	Field         string `json:"move_op_field"`
	ContainsValue string `json:"move_op_value"`
	OutputDir     string `json:"move_op_output_dir"`
}

// FilenameOps are applied to fields by Metarr.
type FilenameOps struct {
	ChannelURL   string `json:"filename_op_channel_url"`
	OpType       string `json:"filename_op_type"`
	OpFindString string `json:"filename_op_find_string"`
	OpValue      string `json:"filename_op_value"`
	OpLoc        string `json:"filename_op_loc"`
	DateFormat   string `json:"filename_op_date_format"`
}

// MetaOps are applied to fields by Metarr.
type MetaOps struct {
	ChannelURL   string `json:"meta_op_channel_url"`
	Field        string `json:"meta_op_field"`
	OpFindString string `json:"meta_op_find_string"`
	OpType       string `json:"meta_op_type"`
	OpValue      string `json:"meta_op_value"`
	OpLoc        string `json:"meta_op_loc"`
	DateFormat   string `json:"meta_op_date_format"`
}

// ChildSettingsMatchParent checks if the child Settings are empty/mismatch the parent on each entry.
func ChildSettingsMatchParent(parent *Settings, child *Settings) bool {
	if (parent == nil && child != nil) || (parent != nil && child == nil) {
		return false
	}

	// Numeric comparisons
	if child.Concurrency != 0 {
		if parent.Concurrency != child.Concurrency {
			return false
		}
	}
	if child.CrawlFreq != 0 {
		if parent.CrawlFreq != child.CrawlFreq {
			return false
		}
	}
	if child.Retries != 0 {
		if parent.Retries != child.Retries {
			return false
		}
	}

	// String comparisons
	if child.CookiesFromBrowser != "" {
		if parent.CookiesFromBrowser != child.CookiesFromBrowser {
			return false
		}
	}
	if child.ExternalDownloader != "" {
		if parent.ExternalDownloader != child.ExternalDownloader {
			return false
		}
	}
	if child.ExternalDownloaderArgs != "" {
		if parent.ExternalDownloaderArgs != child.ExternalDownloaderArgs {
			return false
		}
	}
	if child.ExtraYTDLPMetaArgs != "" {
		if parent.ExtraYTDLPMetaArgs != child.ExtraYTDLPMetaArgs {
			return false
		}
	}
	if child.ExtraYTDLPVideoArgs != "" {
		if parent.ExtraYTDLPVideoArgs != child.ExtraYTDLPVideoArgs {
			return false
		}
	}
	if child.FilterFile != "" {
		if parent.FilterFile != child.FilterFile {
			return false
		}
	}
	if child.FromDate != "" {
		if parent.FromDate != child.FromDate {
			return false
		}
	}
	if child.JSONDir != "" {
		if parent.JSONDir != child.JSONDir {
			return false
		}
	}
	if child.MaxFilesize != "" {
		if parent.MaxFilesize != child.MaxFilesize {
			return false
		}
	}
	if child.MetaFilterMoveOpFile != "" {
		if parent.MetaFilterMoveOpFile != child.MetaFilterMoveOpFile {
			return false
		}
	}
	if child.ToDate != "" {
		if parent.ToDate != child.ToDate {
			return false
		}
	}
	if child.VideoDir != "" {
		if parent.VideoDir != child.VideoDir {
			return false
		}
	}
	if child.YtdlpOutputExt != "" {
		if parent.YtdlpOutputExt != child.YtdlpOutputExt {
			return false
		}
	}

	// Boolean comparisons
	if child.UseGlobalCookies {
		if parent.UseGlobalCookies != child.UseGlobalCookies {
			return false
		}
	}

	// Slice comparisons
	if len(child.Filters) > 0 {
		if len(parent.Filters) != len(child.Filters) {
			return false
		}
		for _, parentf := range parent.Filters {
			for _, childf := range child.Filters {
				if parentf != childf {
					return false
				}
			}
		}
	}

	if len(child.MetaFilterMoveOps) > 0 {
		if len(parent.MetaFilterMoveOps) != len(child.MetaFilterMoveOps) {
			return false
		}
		for _, parentmo := range parent.MetaFilterMoveOps {
			for _, childmo := range child.MetaFilterMoveOps {
				if parentmo != childmo {
					return false
				}
			}
		}
	}

	return true
}

// ChildMetarrArgsMatchParent checks if the child MetarrArgs are empty/mismatch the parent on each entry.
func ChildMetarrArgsMatchParent(parent *MetarrArgs, child *MetarrArgs) bool {
	if (parent == nil && child != nil) || (parent != nil && child == nil) {
		return false
	}

	// String comparisons
	if child.OutputExt != "" {
		if parent.OutputExt != child.OutputExt {
			return false
		}
	}
	if child.FilenameOpsFile != "" {
		if parent.FilenameOpsFile != child.FilenameOpsFile {
			return false
		}
	}
	if child.FilteredFilenameOpsFile != "" {
		if parent.FilteredFilenameOpsFile != child.FilteredFilenameOpsFile {
			return false
		}
	}
	if child.RenameStyle != "" {
		if parent.RenameStyle != child.RenameStyle {
			return false
		}
	}
	if child.MetaOpsFile != "" {
		if parent.MetaOpsFile != child.MetaOpsFile {
			return false
		}
	}
	if child.FilteredMetaOpsFile != "" {
		if parent.FilteredMetaOpsFile != child.FilteredMetaOpsFile {
			return false
		}
	}
	if child.OutputDir != "" {
		if parent.OutputDir != child.OutputDir {
			return false
		}
	}
	if child.MinFreeMem != "" {
		if parent.MinFreeMem != child.MinFreeMem {
			return false
		}
	}
	if child.TranscodeGPU != "" {
		if parent.TranscodeGPU != child.TranscodeGPU {
			return false
		}
	}
	if child.TranscodeVideoFilter != "" {
		if parent.TranscodeVideoFilter != child.TranscodeVideoFilter {
			return false
		}
	}
	if child.TranscodeQuality != "" {
		if parent.TranscodeQuality != child.TranscodeQuality {
			return false
		}
	}
	if child.ExtraFFmpegArgs != "" {
		if parent.ExtraFFmpegArgs != child.ExtraFFmpegArgs {
			return false
		}
	}

	// Numeric comparisons
	if child.Concurrency != 0 {
		if parent.Concurrency != child.Concurrency {
			return false
		}
	}
	if child.MaxCPU != 0 {
		if parent.MaxCPU != child.MaxCPU {
			return false
		}
	}

	// Slice comparisons
	if len(child.FilenameOps) > 0 {
		if len(parent.FilenameOps) != len(child.FilenameOps) {
			return false
		}
		for i := range parent.FilenameOps {
			if parent.FilenameOps[i] != child.FilenameOps[i] {
				return false
			}
		}
	}

	if len(child.FilteredFilenameOps) > 0 {
		if len(parent.FilteredFilenameOps) != len(child.FilteredFilenameOps) {
			return false
		}
		for _, parentffo := range parent.FilteredFilenameOps {
			for _, childffo := range child.FilteredFilenameOps {
				if parentffo.FiltersMatched != childffo.FiltersMatched {
					return false
				}
				for _, parentf := range parentffo.Filters {
					for _, childf := range childffo.Filters {
						if parentf != childf {
							return false
						}
					}
				}
				for _, parentfo := range parentffo.FilenameOps {
					for _, childfo := range childffo.FilenameOps {
						if parentfo != childfo {
							return false
						}
					}
				}
			}
		}
	}

	if len(child.MetaOps) > 0 {
		if len(parent.MetaOps) != len(child.MetaOps) {
			return false
		}
		for i := range parent.MetaOps {
			if parent.MetaOps[i] != child.MetaOps[i] {
				return false
			}
		}
	}

	if len(child.FilteredMetaOps) > 0 {
		if len(parent.FilteredMetaOps) != len(child.FilteredMetaOps) {
			return false
		}
		for _, parentfmo := range parent.FilteredMetaOps {
			for _, childfmo := range child.FilteredMetaOps {
				if parentfmo.FiltersMatched != childfmo.FiltersMatched {
					return false
				}
				for _, parentf := range parentfmo.Filters {
					for _, childf := range childfmo.Filters {
						if parentf != childf {
							return false
						}
					}
				}
				for _, parentmo := range parentfmo.MetaOps {
					for _, childmo := range childfmo.MetaOps {
						if parentmo != childmo {
							return false
						}
					}
				}
			}
		}
	}

	if len(child.URLOutputDirs) > 0 {
		if len(parent.URLOutputDirs) != len(child.URLOutputDirs) {
			return false
		}
		for i := range parent.URLOutputDirs {
			if parent.URLOutputDirs[i] != child.URLOutputDirs[i] {
				return false
			}
		}
	}

	if len(child.TranscodeVideoCodecs) > 0 {
		if len(parent.TranscodeVideoCodecs) != len(child.TranscodeVideoCodecs) {
			return false
		}
		for i := range parent.TranscodeVideoCodecs {
			if parent.TranscodeVideoCodecs[i] != child.TranscodeVideoCodecs[i] {
				return false
			}
		}
	}

	if len(child.TranscodeAudioCodecs) > 0 {
		if len(parent.TranscodeAudioCodecs) != len(child.TranscodeAudioCodecs) {
			return false
		}
		for i := range parent.TranscodeAudioCodecs {
			if parent.TranscodeAudioCodecs[i] != child.TranscodeAudioCodecs[i] {
				return false
			}
		}
	}

	// Map comparison
	if len(child.OutputDirMap) > 0 {
		if len(parent.OutputDirMap) != len(child.OutputDirMap) {
			return false
		}
		for key, val := range parent.OutputDirMap {
			if childVal, ok := child.OutputDirMap[key]; !ok || val != childVal {
				return false
			}
		}
	}

	return true
}

// MetarrArgsAllZero checks if every Metarr Args field is its zero value.
func MetarrArgsAllZero(m *MetarrArgs) bool {
	if m == nil {
		return true
	}

	// Metarr file operations.
	if m.OutputExt != "" {
		return false
	}
	if len(m.FilenameOps) > 0 {
		return false
	}
	if m.FilenameOpsFile != "" {
		return false
	}
	if len(m.FilteredFilenameOps) > 0 {
		return false
	}
	if m.FilteredFilenameOpsFile != "" {
		return false
	}
	if m.RenameStyle != "" {
		return false
	}

	// Metarr metadata operations.
	if len(m.MetaOps) > 0 {
		return false
	}
	if m.MetaOpsFile != "" {
		return false
	}
	if len(m.FilteredMetaOps) > 0 {
		return false
	}
	if m.FilteredMetaOpsFile != "" {
		return false
	}

	// Metarr output directories.
	if m.OutputDir != "" {
		return false
	}
	if len(m.OutputDirMap) > 0 {
		return false
	}
	if len(m.URLOutputDirs) > 0 {
		return false
	}

	// Program operations.
	if m.Concurrency != 0 {
		return false
	}
	if m.MaxCPU != 0.0 {
		return false
	}
	if m.MinFreeMem != "" {
		return false
	}

	// FFmpeg transcoding operations.
	if m.TranscodeGPU != "" {
		return false
	}
	if m.TranscodeVideoFilter != "" {
		return false
	}
	if len(m.TranscodeVideoCodecs) > 0 {
		return false
	}
	if len(m.TranscodeAudioCodecs) > 0 {
		return false
	}
	if m.TranscodeQuality != "" {
		return false
	}
	if m.ExtraFFmpegArgs != "" {
		return false
	}

	return true
}

// SettingsAllZero checks if every Settings field is its zero value.
func SettingsAllZero(s *Settings) bool {
	if s == nil {
		return true
	}

	// Configurations.
	if s.Concurrency != 0 {
		return false
	}

	// Download-related operations.
	if s.CookiesFromBrowser != "" {
		return false
	}
	if s.CrawlFreq != 0 {
		return false
	}
	if s.ExternalDownloader != "" {
		return false
	}
	if s.ExternalDownloaderArgs != "" {
		return false
	}
	if s.MaxFilesize != "" {
		return false
	}
	if s.Retries != 0 {
		return false
	}
	if s.UseGlobalCookies {
		return false
	}
	if s.YtdlpOutputExt != "" {
		return false
	}

	// Custom args.
	if s.ExtraYTDLPVideoArgs != "" {
		return false
	}
	if s.ExtraYTDLPMetaArgs != "" {
		return false
	}

	// Metadata operations.
	if len(s.Filters) > 0 {
		return false
	}
	if s.FilterFile != "" {
		return false
	}
	if len(s.MetaFilterMoveOps) > 0 {
		return false
	}
	if s.MetaFilterMoveOpFile != "" {
		return false
	}
	if s.FromDate != "" {
		return false
	}
	if s.ToDate != "" {
		return false
	}

	// JSON and video directories.
	if s.JSONDir != "" {
		return false
	}
	if s.VideoDir != "" {
		return false
	}

	// Channel toggles.
	if s.Paused {
		return false
	}

	return true
}
