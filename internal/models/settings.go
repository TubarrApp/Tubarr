package models

// ChannelSettings are the primary settings for a channel, affecting videos belonging to it.
type ChannelSettings struct {
	CookieSource           string      `json:"cookie_source"`
	CrawlFreq              int         `json:"crawl_freq"`
	Filters                []DLFilters `json:"filters"`
	Retries                int         `json:"download_retries"`
	ExternalDownloader     string      `json:"external_downloader"`
	ExternalDownloaderArgs string      `json:"external_downloader_args"`
	Concurrency            int         `json:"max_concurrency"`
	MaxFilesize            string      `json:"max_filesize"`
	AutoDownload           bool        `json:"auto_download"`
	FromDate               string      `json:"from_date"`
	ToDate                 string      `json:"to_date"`
}

// DLFilters are used to filter in or out videos from download by metafields.
type DLFilters struct {
	Field string `json:"filter_field"`
	Type  string `json:"filter_type"`
	Value string `json:"filter_value"`
}

// MetarrArgs are the arguments used when calling the Metarr external program.
type MetarrArgs struct {
	Ext                  string   `json:"metarr_ext"`
	FilenameReplaceSfx   []string `json:"metarr_filename_replace_suffix"`
	RenameStyle          string   `json:"metarr_rename_style"`
	FileDatePfx          string   `json:"metarr_filename_date_prefix"`
	MetaOps              []string `json:"metarr_meta_ops"`
	MetaOverwrite        bool     `json:"metarr_meta_overwrite"`
	MetaPreserve         bool     `json:"metarr_meta_preserve"`
	OutputDir            string   `json:"metarr_output_directory"`
	Concurrency          int      `json:"metarr_concurrency"`
	MaxCPU               float64  `json:"metarr_max_cpu_usage"`
	MinFreeMem           string   `json:"metarr_min_free_mem"`
	UseGPU               string   `json:"metarr_gpu"`
	GPUDir               string   `json:"metarr_gpu_directory"`
	TranscodeVideoFilter string   `json:"metarr_transcode_video_filter"`
	TranscodeCodec       string   `json:"metarr_transcode_codec"`
	TranscodeAudioCodec  string   `json:"metarr_transcode_audio_codec"`
	TranscodeQuality     string   `json:"metarr_transcode_quality"`
}

type ChanURLAuthDetails struct {
	Username   string
	Password   string
	LoginURL   string
	BaseDomain string
	CookiePath string
}
