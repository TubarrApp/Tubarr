package models

// ChannelInputPtrs contains pointers to variables used for adding channels.
type ChannelInputPtrs struct {

	// Channel identifiers
	Name *string   `viper:"channel-name"`
	URLs *[]string `viper:"channel-urls"`

	// Directory paths
	VideoDir *string `viper:"video-directory"`
	JSONDir  *string `viper:"json-directory"`
	OutDir   *string `viper:"metarr-default-output-dir"`
	GPUDir   *string `viper:"transcode-gpu-directory"`

	// Configuration files
	ChannelConfigFile       *string `viper:"channel-config-file"`
	DLFilterFile            *string `viper:"filter-ops-file"`
	MoveOpFile              *string `viper:"move-ops-file"`
	MetaOpsFile             *string `viper:"metarr-meta-ops-file"`
	FilteredMetaOpsFile     *string `viper:"metarr-filtered-meta-ops-file"`
	FilenameOpsFile         *string `viper:"metarr-filename-ops-file"`
	FilteredFilenameOpsFile *string `viper:"metarr-filtered-filename-ops-file"`

	// Authentication details
	Username    *string   `viper:"auth-username"`
	Password    *string   `viper:"auth-password"`
	LoginURL    *string   `viper:"auth-url"`
	AuthDetails *[]string `viper:"auth-details"`

	// Notification details
	Notification *[]string `viper:"notify"`

	// Download settings
	CookiesFromBrowser     *string `viper:"cookies-from-browser"`
	ExternalDownloader     *string `viper:"external-downloader"`
	ExternalDownloaderArgs *string `viper:"external-downloader-args"`
	MaxFilesize            *string `viper:"max-filesize"`
	YTDLPOutputExt         *string `viper:"ytdlp-output-extension"`
	FromDate               *string `viper:"from-date"`
	ToDate                 *string `viper:"to-date"`
	UseGlobalCookies       *bool   `viper:"use-global-cookies"`

	// Filter and operation settings
	DLFilters           *[]string `viper:"filter-ops"`
	MoveOps             *[]string `viper:"move-ops"`
	MetaOps             *[]string `viper:"metarr-meta-ops"`
	FilenameOps         *[]string `viper:"metarr-filename-ops"`
	FilteredMetaOps     *[]string `viper:"metarr-filtered-meta-ops"`
	FilteredFilenameOps *[]string `viper:"metarr-filtered-filename-ops"`

	// Metarr settings
	MetarrExt   *string `viper:"metarr-output-ext"`
	RenameStyle *string `viper:"metarr-rename-style"`
	MinFreeMem  *string `viper:"metarr-min-free-mem"`

	// Transcoding settings
	TranscodeGPU         *string   `viper:"transcode-gpu"`
	TranscodeQuality     *string   `viper:"transcode-quality"`
	TranscodeVideoFilter *string   `viper:"transcode-video-filter"`
	VideoCodec           *[]string `viper:"transcode-video-codecs"`
	AudioCodec           *[]string `viper:"transcode-audio-codecs"`

	// Extra arguments
	ExtraYTDLPVideoArgs *string `viper:"extra-ytdlp-video-args"`
	ExtraYTDLPMetaArgs  *string `viper:"extra-ytdlp-meta-args"`
	ExtraFFmpegArgs     *string `viper:"extra-ffmpeg-args"`

	// Concurrency and performance settings
	CrawlFreq         *int     `viper:"crawl-freq"`
	Concurrency       *int     `viper:"concurrency-limit"`
	MetarrConcurrency *int     `viper:"metarr-concurrency"`
	Retries           *int     `viper:"dl-retries"`
	MaxCPU            *float64 `viper:"metarr-max-cpu"`

	// Boolean flags
	Pause     *bool `viper:"pause-toggle"`
	IgnoreRun *bool `viper:"ignore-run"`
}
