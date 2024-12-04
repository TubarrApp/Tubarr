// Package cmdvideo holds constants for video download command flags.
package cmdvideo

const (
	AfterMove         = "after_move:%(filepath)s"
	CookieSource      = "--cookies-from-browser"
	ExternalDLer      = "--external-downloader"
	ExternalDLArgs    = "--external-downloader-args"
	FilenameSyntax    = "%(title)s.%(ext)s"
	RestrictFilenames = "--restrict-filenames"
	Retries           = "--retries"
	SleepRequests     = "--sleep-requests"
	SleepRequestsNum  = "1"
	MaxFilesize       = "--max-filesize"
	Output            = "-o"
	Print             = "--print"
	YTDLP             = "yt-dlp"
)

const (
	AriaLog = "--console-log-level=info"
)
