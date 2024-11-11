package domain

// Terminal keys
const (
	VideoDir string = "video-dir"
	MetaDir  string = "metadata-dir"
	JsonDir  string = "json-dir"

	InputExts    string = "input-exts"
	InputPreset  string = "preset"
	MetarrPreset string = "metarr-preset"
	ChannelFile  string = "check-channels"
	CookieSource string = "cookie-source"
	CookiePath   string = "cookie-file"

	ConcurrencyLimitInput string = "concurrency-limit"

	ExternalDownloader     string = "external-downloader"
	ExternalDownloaderArgs string = "external-downloader-args"
	MoveOnComplete         string = "move-on-complete"

	OutputFiletype string = "ext"

	FilterOpsInput    string = "filter-ops"
	TomlPath          string = "config-file"
	RestrictFilenames string = "restrict_filenames"
	DLRetries         string = "dl-retries"
)

// Primary program
const (
	Context    string = "Context"
	WaitGroup  string = "WaitGroup"
	SingleFile string = "SingleFile"
)

// Check channels for new uploads
const (
	ChannelCheckNew string = "CheckChannelsForNew"
)

// Download operations
const (
	FilterOps   string = "filterOps"
	Concurrency string = "concurrency"
)

// Filename edits
const (
	FileNaming string = "ytdlp-naming-style"
)

// Logging
const (
	DebugLevel string = "debug-level"
)

// Web related
const (
	Configuration string = "configuration"
	ValidURLs     string = "validUrls"
)
