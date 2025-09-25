// Package cmdvideo holds constants for video download command flags.
package cmdvideo

const (
	AfterMove            = "after_move:%(filepath)s"
	CookiesFromBrowser   = "--cookies-from-browser"
	CookiePath           = "--cookies"
	ExternalDLer         = "--external-downloader"
	ExternalDLArgs       = "--external-downloader-args"
	FilenameSyntax       = "%(title)s.%(ext)s"
	YtdlpOutputExtension = "--merge-output-format"
	RestrictFilenames    = "--restrict-filenames"
	Retries              = "--retries"
	SleepRequests        = "--sleep-requests"
	SleepRequestsNum     = "1"
	MaxFilesize          = "--max-filesize"
	Output               = "-o"
	Print                = "--print"
	YTDLP                = "yt-dlp"
)

const (
	AriaLog = "--console-log-level=info"
)

var (
	RandomizeRequests = []string{"--sleep-interval", "2", "--max-sleep-interval", "10"}
)
