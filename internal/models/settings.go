package models

import "net/http"

// ChannelSettings are the primary settings for a channel, affecting videos belonging to it.
type ChannelSettings struct {
	ChannelConfigFile      string      `json:"channel_config_file" mapstructure:"channel-config-file"`
	Concurrency            int         `json:"max_concurrency" mapstructure:"max-concurrency"`
	CookieSource           string      `json:"cookie_source" mapstructure:"cookie-source"`
	CrawlFreq              int         `json:"crawl_freq" mapstructure:"crawl-freq"`
	ExternalDownloader     string      `json:"external_downloader" mapstructure:"external-downloader"`
	ExternalDownloaderArgs string      `json:"external_downloader_args" mapstructure:"external-downloader-args"`
	Filters                []DLFilters `json:"filters" mapstructure:"filters"`
	FilterFile             string      `json:"filter_file" mapstructure:"filter-file"`
	MoveOps                []MoveOps   `json:"move_ops" mapstructure:"move-ops"`
	MoveOpFile             string      `json:"move_ops_file" mapstructure:"move-ops-file"`
	FromDate               string      `json:"from_date" mapstructure:"from-date"`
	JSONDir                string      `json:"json_directory" mapstructure:"json-directory"`
	MaxFilesize            string      `json:"max_filesize" mapstructure:"max-filesize"`
	Paused                 bool        `json:"paused" mapstructure:"pause"`
	Retries                int         `json:"download_retries" mapstructure:"download-retries"`
	ToDate                 string      `json:"to_date" mapstructure:"to-date"`
	VideoDir               string      `json:"video_directory" mapstructure:"video-directory"`
	UseGlobalCookies       bool        `json:"use_global_cookies" mapstructure:"use-global-cookies"`
	YtdlpOutputExt         string      `json:"ytdlp_output_ext" mapstructure:"ytdlp-output-ext"`
}

// DLFilters are used to filter in or out videos from download by metafields.
type DLFilters struct {
	ChannelURL string `json:"filter_url_specific"`
	Field      string `json:"filter_field"`
	Type       string `json:"filter_type"`
	Value      string `json:"filter_value"`
	MustAny    string `json:"filter_must_any"`
}

// MoveOps are used to set an output directory in Metarr based on matching metadata fields.
type MoveOps struct {
	ChannelURL string `json:"move_url_specific"`
	Field      string `json:"move_op_field"`
	Value      string `json:"move_op_value"`
	OutputDir  string `json:"move_op_output_dir"`
}

// MetaOps are metadata operations set in Metarr.
type MetaOps struct {
	ChannelURL string `json:"meta_url_specific"`
	Op         string
}

// MetarrArgs are the arguments used when calling the Metarr external program.
type MetarrArgs struct {
	Ext                  string   `json:"metarr_ext" mapstructure:"metarr-ext"`
	FilenameReplaceSfx   []string `json:"metarr_filename_replace_suffix" mapstructure:"metarr-filename-replace-suffix"`
	RenameStyle          string   `json:"metarr_rename_style" mapstructure:"metarr-rename-style"`
	FilenameDateTag      string   `json:"metarr_filename_date_prefix" mapstructure:"metarr-filename-date-prefix"`
	MetaOps              []string `json:"metarr_meta_ops" mapstructure:"metarr-meta-ops"`
	OutputDir            string   `json:"metarr_output_directory" mapstructure:"metarr-default-output-dir"`
	URLOutputDirs        []string `json:"metarr_url_output_directories" mapstructure:"metarr-url-output-dirs"`
	Concurrency          int      `json:"metarr_concurrency" mapstructure:"metarr-concurrency"`
	MaxCPU               float64  `json:"metarr_max_cpu_usage" mapstructure:"metarr-max-cpu"`
	MinFreeMem           string   `json:"metarr_min_free_mem" mapstructure:"metarr-min-free-mem"`
	UseGPU               string   `json:"metarr_gpu" mapstructure:"transcode-gpu"`
	GPUDir               string   `json:"metarr_gpu_directory" mapstructure:"transcode-gpu-directory"`
	TranscodeVideoFilter string   `json:"metarr_transcode_video_filter" mapstructure:"transcode-video-filter"`
	TranscodeCodec       string   `json:"metarr_transcode_codec" mapstructure:"transcode-codec"`
	TranscodeAudioCodec  string   `json:"metarr_transcode_audio_codec" mapstructure:"transcode-audio-codec"`
	TranscodeQuality     string   `json:"metarr_transcode_quality" mapstructure:"transcode-quality"`
	ExtraFFmpegArgs      string   `json:"metarr_extra_ffmpeg_args" mapstructure:"extra-ffmpeg-args"`
	OutputDirMap         map[string]string
}

// ChannelAccessDetails holds details related to authentication and cookies.
type ChannelAccessDetails struct {
	Username   string
	Password   string
	LoginURL   string
	BaseDomain string
	CookiePath string
	Cookies    []*http.Cookie
}
