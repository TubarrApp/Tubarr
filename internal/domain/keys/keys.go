package domain

// Terminal keys
const (
	VideoDir     string = "video-dir"
	MetaDir      string = "metadata-dir"
	InputExts    string = "input-exts"
	InputPreset  string = "preset"
	MetarrPreset string = "metarr-preset"
	ChannelFile  string = "check-channels"
	CookieSource string = "cookie-source"
)

// Primary program
const (
	Context    string = "Context"
	WaitGroup  string = "WaitGroup"
	SingleFile string = "SingleFile"
)

const (
	ChannelCheckNew string = "CheckChannelsForNew"
)

// Filename edits
const (
	FileNaming string = "ytdlp-naming-style"
)

// Logging
var (
	DebugLevel string
)
