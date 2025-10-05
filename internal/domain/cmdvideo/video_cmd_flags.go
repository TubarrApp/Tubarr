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
	MaxFilesize          = "--max-filesize"
	Output               = "-o"
	Print                = "--print"
	YTDLP                = "yt-dlp"
)

const (
	AriaLog         = "--console-log-level=notice"
	AriaLogFile     = "--log=-"
	AriaInterval    = "--summary-interval=1"
	AriaNoColor     = "--enable-color=false"
	AriaNoRPC       = "--enable-rpc=false"
	AriaShowConsole = "--show-console=true"
)

var (
	RandomizeRequests = []string{"-t", "sleep"}
)
