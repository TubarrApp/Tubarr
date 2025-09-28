// Package keys holds various keys for software operations, such as terminal input keys and internal Viper keys.
package keys

// Channel identifiers
const (
	URL  string = "channel-urls"
	Name string = "channel-name"
	ID   string = "channel-id"
)

// Auth
const (
	AuthUsername string = "auth-username"
	AuthPassword string = "auth-password"
	AuthURL      string = "auth-url"
	AuthDetails  string = "auth-details"
)

// Notification
const (
	NotifyPair string = "notify"
)

// Files and directories
const (
	ChannelConfigFile string = "channel-config-file"
	VideoDir          string = "video-directory"
	JSONDir           string = "json-directory"
	MetarrPreset      string = "metarr-preset"
	OutputFiletype    string = "ext"
)

// Transcoding
const (
	TranscodeGPU         string = "transcode-gpu"
	TranscodeGPUDir      string = "transcode-gpu-directory"
	TranscodeCodec       string = "transcode-codec"
	TranscodeAudioCodec  string = "transcode-audio-codec"
	TranscodeQuality     string = "transcode-quality"
	TranscodeVideoFilter string = "transcode-video-filter"
)

// Downloading
const (
	YtdlpOutputExt         string = "ytdlp-output-extension"
	MaxFilesize            string = "max-filesize"
	CookieSource           string = "cookie-source"
	DLRetries              string = "dl-retries"
	FromDate               string = "from-date"
	Pause                  string = "pause-toggle"
	ToDate                 string = "to-date"
	UseGlobalCookies       string = "use-global-cookies"
	ExternalDownloader     string = "external-downloader"
	ExternalDownloaderArgs string = "external-downloader-args"
)

// Program inputs
const (
	ConcurrencyLimitInput string = "concurrency-limit"
	MoveOnComplete        string = "move-on-complete"
	URLFile               string = "url-file"
	URLAdd                string = "add-url"
	URLs                  string = "urls"
	Benchmarking          string = "benchmark"
)

// Settings
const (
	FilterOpsInput string = "filter-ops"
	FilterOpsFile  string = "filter-ops-file"
	MoveOps        string = "move-ops"
	MoveOpsFile    string = "move-ops-file"
	CrawlFreq      string = "crawl-freq"
)

// Database operations
const (
	DBOpsInput   string = "db-ops"
	ChanOpsInput string = "channel-ops"
)

// Metarr operations
const (
	MFilenameDateTag       string = "metarr-filename-date-tag"
	MFilenameReplaceSuffix string = "metarr-filename-replace-suffix"
	MDescDatePfx           string = "metarr-desc-date-prefix"
	MDescDateSfx           string = "metarr-desc-date-suffix"
	MGPU                   string = "metarr-gpu"
	MGPUDirectory          string = "metarr-gpu-directory"
	MMetaOps               string = "metarr-meta-ops"
	MMetaPurge             string = "metarr-purge-metafile"
	MFilenamePfx           string = "metarr-metadata-filename-prefix"
	MRenameStyle           string = "metarr-rename-style"
	MMaxCPU                string = "metarr-max-cpu"
	MMinFreeMem            string = "metarr-min-free-mem"
	MMetaOverwrite         string = "metarr-meta-overwrite"
	MMetaPreserve          string = "metarr-meta-preserve"
	MNoFileOverwrite       string = "metarr-no-file-overwrite"
	MTranscodeAudioCodec   string = "metarr-transcode-audio-codec"
	MTranscodeQuality      string = "metarr-transcode-quality"
	MTranscodeVideoFilter  string = "metarr-transcode-video-filter"
	MTranscodeVideoCodec   string = "metarr-transcode-codec"
	MConcurrency           string = "metarr-concurrency"
	MOutputDir             string = "metarr-output-dir"
	MExt                   string = "metarr-ext"
)
