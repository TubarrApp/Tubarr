// Package keys holds various keys for software operations, such as terminal input keys and internal Viper keys.
package keys

// Terminal keys:
// Files and directories
const (
	URL  string = "url"
	Name string = "name"
	ID   string = "id"
)
const (
	VideoDir       string = "video-directory"
	JSONDir        string = "json-directory"
	MetarrPreset   string = "metarr-preset"
	OutputFiletype string = "ext"
)

// Web inputs
const (
	MaxFilesize            string = "max-filesize"
	CookieSource           string = "cookie-source"
	DLRetries              string = "dl-retries"
	ExternalDownloader     string = "external-downloader"
	ExternalDownloaderArgs string = "external-downloader-args"
)

// Program inputs
const (
	ConcurrencyLimitInput string = "concurrency-limit"
	MoveOnComplete        string = "move-on-complete"
	URLFile               string = "url-file"
	URLAdd                string = "add-url"
)

// Settings
const (
	FilterOpsInput string = "filter-ops"
	CrawlFreq      string = "crawl-freq"
)

// Database operations
const (
	DBOpsInput   string = "db-ops"
	ChanOpsInput string = "channel-ops"
)

// Metarr operations
const (
	InputFileDatePfx        string = "metarr-filename-date-tag"
	InputFilenameReplaceSfx string = "metarr-filename-replace-suffix"
	MDescDatePfx            string = "metarr-desc-date-prefix"
	MDescDateSfx            string = "metarr-desc-date-suffix"
	MetaOps                 string = "metarr-meta-ops"
	MetaPurge               string = "metarr-purge-metafile"
	MFilenamePfx            string = "metarr-metadata-filename-prefix"
	OutputFiletypeInput     string = "metarr-ext"
	RenameStyle             string = "metarr-rename-style"
	MaxCPU                  string = "metarr-max-cpu"
	MinFreeMem              string = "metarr-min-free-mem"
	NoFileOverwrite         string = "metarr-no-file-overwrite"
	MetarrConcurrency       string = "metarr-concurrency"
	MetarrOutputDir         string = "metarr-output-dir"
)
