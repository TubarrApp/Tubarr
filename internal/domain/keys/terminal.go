package keys

// Terminal keys:
// Files and directories
const (
	VideoDir       string = "video-dir"
	JSONDir        string = "json-dir"
	MetarrPreset   string = "metarr-preset"
	OutputFiletype string = "ext"
)

// Web inputs
const (
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
	URLAdd                string = "url"
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
	InputFileDatePfx        string = "filename-date-tag"
	InputFilenameReplaceSfx string = "filename-replace-suffix"
	MDescDatePfx            string = "desc-date-prefix"
	MDescDateSfx            string = "desc-date-suffix"
	MetaOps                 string = "meta-ops"
	MetaPurge               string = "purge-metafile"
	MFilenamePfx            string = "metadata-filename-prefix"
	OutputFiletypeInput     string = "ext"
	RenameStyle             string = "rename-style"
)
