package models

// ChannelInputPtrs contains pointers to variables used for adding channels.
type ChannelInputPtrs struct {
	// Channel identifiers.
	Name              *string   `viper:"channel-name"`
	URLs              *[]string `viper:"channel-urls"`
	ChannelConfigFile *string   `viper:"channel-config-file"`

	// Directory paths.
	VideoDir *string `viper:"video-directory"`
	JSONDir  *string `viper:"json-directory"`

	// Configuration files.
	DLFilterFile *string `viper:"filter-ops-file"`

	// Authentication details.
	Username    *string   `viper:"auth-username"`
	Password    *string   `viper:"auth-password"`
	LoginURL    *string   `viper:"auth-url"`
	AuthDetails *[]string `viper:"auth-details"`

	// Notification details.
	Notification *[]string `viper:"notify"`

	// Download settings.
	CookiesFromBrowser     *string `viper:"cookies-from-browser"`
	ExternalDownloader     *string `viper:"external-downloader"`
	ExternalDownloaderArgs *string `viper:"external-downloader-args"`
	MaxFilesize            *string `viper:"max-filesize"`
	YTDLPOutputExt         *string `viper:"ytdlp-output-extension"`
	FromDate               *string `viper:"from-date"`
	ToDate                 *string `viper:"to-date"`
	UseGlobalCookies       *bool   `viper:"use-global-cookies"`

	// Filter and operation settings.
	DLFilters *[]string `viper:"filter-ops"`
	MoveOps   *[]string `viper:"move-ops"`

	// Extra arguments.
	ExtraYTDLPVideoArgs *string `viper:"extra-ytdlp-video-args"`
	ExtraYTDLPMetaArgs  *string `viper:"extra-ytdlp-meta-args"`

	// Concurrency and performance settings.
	CrawlFreq   *int `viper:"crawl-freq"`
	Concurrency *int `viper:"concurrency-limit"`
	Retries     *int `viper:"dl-retries"`

	// Boolean flags.
	Pause     *bool `viper:"pause-toggle"`
	IgnoreRun *bool `viper:"ignore-run"`

	// Per-URL settings overrides (no viper tag - handled manually).
	URLSettings map[string]*URLSettingsOverride

	// -- METARR --
	// Output settings.
	OutDir      *string `viper:"metarr-default-output-dir"`
	MetarrExt   *string `viper:"metarr-output-ext"`
	RenameStyle *string `viper:"metarr-rename-style"`

	// Op files.
	MoveOpFile              *string   `viper:"move-ops-file"`
	MetaOpsFile             *string   `viper:"metarr-meta-ops-file"`
	FilteredMetaOpsFile     *string   `viper:"metarr-filtered-meta-ops-file"`
	FilenameOpsFile         *string   `viper:"metarr-filename-ops-file"`
	FilteredFilenameOpsFile *string   `viper:"metarr-filtered-filename-ops-file"`
	MetaOps                 *[]string `viper:"metarr-meta-ops"`

	// Ops.
	FilenameOps         *[]string `viper:"metarr-filename-ops"`
	FilteredMetaOps     *[]string `viper:"metarr-filtered-meta-ops"`
	FilteredFilenameOps *[]string `viper:"metarr-filtered-filename-ops"`
	URLOutputDirs       *[]string `viper:"metarr-url-output-dirs"`

	// Transcoding.
	TranscodeGPU          *string   `viper:"metarr-transcode-gpu"`
	TranscodeGPUDirectory *string   `viper:"metarr-transcode-gpu-node"`
	TranscodeQuality      *string   `viper:"metarr-transcode-quality"`
	TranscodeVideoFilter  *string   `viper:"metarr-transcode-video-filter"`
	TranscodeVideoCodec   *[]string `viper:"metarr-transcode-video-codecs"`
	TranscodeAudioCodec   *[]string `viper:"metarr-transcode-audio-codecs"`

	// Resources and limits.
	MinFreeMem        *string  `viper:"metarr-min-free-mem"`
	MaxCPU            *float64 `viper:"metarr-max-cpu"`
	MetarrConcurrency *int     `viper:"metarr-concurrency"`

	// Miscellaneous.
	ExtraFFmpegArgs *string `viper:"metarr-extra-ffmpeg-args"`
}

// URLSettingsOverride contains per-URL setting overrides.
type URLSettingsOverride struct {
	Settings *ChannelInputPtrs
	Metarr   *ChannelInputPtrs
}
