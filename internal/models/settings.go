package models

import (
	"time"
)

// Settings are the primary settings for a channel, affecting videos belonging to it.
type Settings struct {
	// Configurations.
	ChannelConfigFile string `json:"channel_config_file" mapstructure:"channel-config-file"`
	Concurrency       int    `json:"max_concurrency" mapstructure:"max-concurrency"`

	// Download-related operations.
	CookiesFromBrowser     string `json:"cookies_from_browser" mapstructure:"cookie-from-browser"`
	CrawlFreq              int    `json:"crawl_freq" mapstructure:"crawl-freq"`
	ExternalDownloader     string `json:"external_downloader" mapstructure:"external-downloader"`
	ExternalDownloaderArgs string `json:"external_downloader_args" mapstructure:"external-downloader-args"`
	MaxFilesize            string `json:"max_filesize" mapstructure:"max-filesize"`
	Retries                int    `json:"download_retries" mapstructure:"download-retries"`
	UseGlobalCookies       bool   `json:"use_global_cookies" mapstructure:"use-global-cookies"`
	YtdlpOutputExt         string `json:"ytdlp_output_ext" mapstructure:"ytdlp-output-ext"`

	// Custom args
	ExtraYTDLPVideoArgs string `json:"extra_ytdlp_video_args" mapstructure:"extra-ytdlp-video-args"`
	ExtraYTDLPMetaArgs  string `json:"extra_ytdlp_meta_args" mapstructure:"extra-ytdlp-meta-args"`

	// Metadata operations.
	Filters    []Filters `json:"filters" mapstructure:"filters"`
	FilterFile string    `json:"filter_file" mapstructure:"filter-file"`
	MoveOps    []MoveOps `json:"move_ops" mapstructure:"move-ops"`
	MoveOpFile string    `json:"move_ops_file" mapstructure:"move-ops-file"`
	FromDate   string    `json:"from_date" mapstructure:"from-date"`
	ToDate     string    `json:"to_date" mapstructure:"to-date"`

	// JSON and video directories.
	JSONDir  string `json:"json_directory" mapstructure:"json-directory"`
	VideoDir string `json:"video_directory" mapstructure:"video-directory"`

	// Bot blocking elements.
	BotBlocked           bool                 `json:"bot_blocked"`
	BotBlockedHostnames  []string             `json:"bot_blocked_hostname"`
	BotBlockedTimestamps map[string]time.Time `json:"bot_blocked_timestamps"`

	// Channel toggles.
	Paused bool `json:"paused" mapstructure:"pause"`
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

// MoveOps are used to set an output directory in Metarr based on matching metadata fields.
type MoveOps struct {
	ChannelURL string `json:"move_url_specific"`
	Field      string `json:"move_op_field"`
	Value      string `json:"move_op_value"`
	OutputDir  string `json:"move_op_output_dir"`
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

// MetarrArgs are the arguments used when calling the Metarr external program.
type MetarrArgs struct {
	// Metarr file operations.
	OutputExt               string                `json:"metarr_output_ext" mapstructure:"metarr-output-ext"`
	FilenameOps             []FilenameOps         `json:"metarr_filename_ops"`
	FilenameOpsFile         string                `json:"metarr_filename_ops_file" mapstructure:"metarr-filename-ops-file"`
	FilteredFilenameOps     []FilteredFilenameOps `json:"filtered_filename_ops"`
	FilteredFilenameOpsFile string                `json:"filtered_filename_ops_file" mapstructure:"metarr-filtered-filename-ops-file"`
	RenameStyle             string                `json:"metarr_rename_style" mapstructure:"metarr-rename-style"`

	// Metarr metadata operations.
	MetaOps             []MetaOps         `json:"metarr_meta_ops"`
	MetaOpsFile         string            `json:"metarr_meta_ops_file" mapstructure:"metarr-meta-ops-file"`
	FilteredMetaOps     []FilteredMetaOps `json:"filtered_meta_ops"`
	FilteredMetaOpsFile string            `json:"filtered_meta_ops_file" mapstructure:"metarr-filtered-meta-ops-file"`

	// Metarr output directories.
	OutputDir     string `json:"metarr_output_directory" mapstructure:"metarr-default-output-dir"`
	OutputDirMap  map[string]string
	URLOutputDirs []string `json:"metarr_url_output_directories" mapstructure:"metarr-url-output-dirs"`

	// Program operations.
	Concurrency int     `json:"metarr_concurrency" mapstructure:"metarr-concurrency"`
	MaxCPU      float64 `json:"metarr_max_cpu_usage" mapstructure:"metarr-max-cpu"`
	MinFreeMem  string  `json:"metarr_min_free_mem" mapstructure:"metarr-min-free-mem"`

	// FFmpeg transcoding operations.
	UseGPU               string `json:"metarr_gpu" mapstructure:"transcode-gpu"`
	GPUDir               string `json:"metarr_gpu_directory" mapstructure:"transcode-gpu-directory"`
	TranscodeVideoFilter string `json:"metarr_transcode_video_filter" mapstructure:"transcode-video-filter"`
	TranscodeCodec       string `json:"metarr_transcode_codec" mapstructure:"transcode-codec"`
	TranscodeAudioCodec  string `json:"metarr_transcode_audio_codec" mapstructure:"transcode-audio-codec"`
	TranscodeQuality     string `json:"metarr_transcode_quality" mapstructure:"transcode-quality"`
	ExtraFFmpegArgs      string `json:"metarr_extra_ffmpeg_args" mapstructure:"extra-ffmpeg-args"`
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
