// Package cmdjson holds constants for video download command flags.
package cmdjson

const (
	CookieSource      = "--cookies-from-browser"
	CookiePath        = "--cookies"
	ExternalDLer      = "--external-downloader"
	ExternalDLArgs    = "--external-downloader-args"
	FilenameSyntax    = "%(title)s.%(ext)s"
	RestrictFilenames = "--restrict-filenames"
	MaxFilesize       = "--max-filesize"
	Output            = "-o"
	P                 = "-P"
	Retries           = "--retries"
	SkipVideo         = "--skip-download"
	WriteInfoJSON     = "--write-info-json"
	YTDLP             = "yt-dlp"
)
