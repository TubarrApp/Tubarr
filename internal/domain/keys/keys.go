package domain

// Terminal keys
const (
	VideoDir               string = "video-dir"
	MetaDir                string = "metadata-dir"
	InputExts              string = "input-exts"
	InputPreset            string = "preset"
	MetarrPreset           string = "metarr-preset"
	ChannelFile            string = "check-channels"
	CookieSource           string = "cookie-source"
	ExternalDownloader     string = "external-downloader"
	ExternalDownloaderArgs string = "external-downloader-args"
	MoveOnComplete         string = "move-on-complete"
	OutputFiletype         string = "output-filetype"
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

// Filename edits
const (
	FileNaming string = "ytdlp-naming-style"
)

// Logging
const (
	DebugLevel string = "debug-level"
)
