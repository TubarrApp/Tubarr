// Package keys holds various keys for software operations, such as terminal input keys and internal Viper keys.
package keys

// Channel identifiers.
const (
	URL  string = "channel-urls"
	Name string = "channel-name"
	ID   string = "channel-id"
)

// Auth.
const (
	AuthUsername string = "auth-username"
	AuthPassword string = "auth-password"
	AuthURL      string = "auth-url"
	AuthDetails  string = "auth-details"
)

// Notification.
const (
	NotifyPair string = "notify"
)

// Files and directories.
const (
	ChannelConfigFile string = "channel-config-file"
	VideoDir          string = "video-directory"
	JSONDir           string = "json-directory"
	MetarrPreset      string = "metarr-preset"
	OutputFiletype    string = "output-ext"
)

// Transcoding.
const (
	TranscodeGPU         string = "metarr-transcode-gpu"
	TranscodeGPUNode     string = "metarr-transcode-gpu-node"
	TranscodeCodec       string = "metarr-transcode-video-codecs"
	TranscodeAudioCodec  string = "metarr-transcode-audio-codecs"
	TranscodeQuality     string = "metarr-transcode-quality"
	TranscodeVideoFilter string = "metarr-transcode-video-filter"
)

// Downloading.
const (
	ChanOrURLConcurrencyLimit string = "concurrency"
	YtdlpOutputExt            string = "ytdlp-output-extension"
	MaxFilesize               string = "max-filesize"
	CookiesFromBrowser        string = "cookies-from-browser"
	DLRetries                 string = "dl-retries"
	FromDate                  string = "from-date"
	Pause                     string = "pause-toggle"
	IgnoreRun                 string = "ignore-run"
	ToDate                    string = "to-date"
	UseGlobalCookies          string = "use-global-cookies"
	ExternalDownloader        string = "external-downloader"
	ExternalDownloaderArgs    string = "external-downloader-args"
	YTDLPPreferredVideoCodecs string = "ytdlp-preferred-video-codecs"
	YTDLPPreferredAudioCodecs string = "ytdlp-preferred-audio-codecs"
)

// Program inputs.
const (
	GlobalConcurrency string = "global-concurrency"
	MoveOnComplete    string = "move-on-complete"
	URLFile           string = "url-file"
	URLAdd            string = "add-url"
	URLs              string = "urls"
	Benchmarking      string = "benchmark"
	SkipInitialWait   string = "skip-initial-wait"
	SkipAllWaits      string = "skip-all-waits"
)

// Settings.
const (
	FilterOpsInput string = "filter-ops"
	FilterOpsFile  string = "filter-ops-file"
	MoveOps        string = "move-ops"
	MoveOpsFile    string = "move-ops-file"
	CrawlFreq      string = "crawl-freq"
)

// Database operations.
const (
	DBOpsInput   string = "db-ops"
	ChanOpsInput string = "channel-ops"
)

// Metarr operations.
const (
	MDescDatePfx             string = "metarr-desc-date-prefix"
	MDescDateSfx             string = "metarr-desc-date-suffix"
	MMetaOps                 string = "metarr-meta-ops"
	MMetaOpsFile             string = "metarr-meta-ops-file"
	MFilenameOps             string = "metarr-filename-ops"
	MFilenameOpsFile         string = "metarr-filename-ops-file"
	MFilteredFilenameOps     string = "metarr-filtered-filename-ops"
	MFilteredFilenameOpsFile string = "metarr-filtered-filename-ops-file"
	MFilteredMetaOps         string = "metarr-filtered-meta-ops"
	MFilteredMetaOpsFile     string = "metarr-filtered-meta-ops-file"
	MMetaPurge               string = "metarr-purge-metafile"
	MFilenamePfx             string = "metarr-metadata-filename-prefix"
	MRenameStyle             string = "metarr-rename-style"
	MMaxCPU                  string = "metarr-max-cpu"
	MMinFreeMem              string = "metarr-min-free-mem"
	MMetaOverwrite           string = "metarr-meta-overwrite"
	MMetaPreserve            string = "metarr-meta-preserve"
	MNoFileOverwrite         string = "metarr-no-file-overwrite"
	MTranscodeAudioCodec     string = "metarr-transcode-audio-codecs"
	MTranscodeQuality        string = "metarr-transcode-quality"
	MTranscodeVideoFilter    string = "metarr-transcode-video-filter"
	MTranscodeVideoCodec     string = "metarr-transcode-video-codec"
	MConcurrency             string = "metarr-concurrency"
	MOutputDir               string = "metarr-output-directory"
	MOutputExt               string = "metarr-output-ext"
	MExtraFFmpegArgs         string = "metarr-extra-ffmpeg-args"
)

// Custom additional commands.
const (
	ExtraYTDLPVideoArgs = "extra-ytdlp-video-args" // applied only to video downloads.
	ExtraYTDLPMetaArgs  = "extra-ytdlp-meta-args"  // applies only to metadata downloads.
)

// Web interface or terminal.
const (
	RunWebInterface = "web"
)

// Miscellaneous.
const (
	DebugLevel    string = "debug"
	PurgeMetaFile string = "purge-metafile"
)
