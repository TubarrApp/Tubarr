// Package command holds constants used for building argument strings for commands.
package command

// General yt-dlp commands.
const (
	AfterMove            = "after_move:%(filepath)s"
	CookiesFromBrowser   = "--cookies-from-browser"
	CookiePath           = "--cookies"
	ExternalDLer         = "--external-downloader"
	ExternalDLArgs       = "--external-downloader-args"
	FilenameSyntax       = "%(title)s.%(ext)s"
	MaxFilesize          = "--max-filesize"
	Output               = "-o"
	P                    = "-P"
	Print                = "--print"
	RestrictFilenames    = "--restrict-filenames"
	Retries              = "--retries"
	YTDLP                = "yt-dlp"
	YtDLPOutputExtension = "--merge-output-format"
)

// Scrape-related yt-dlp commands.
const (
	YtDLPFlatPlaylist = "--flat-playlist"
)

// JSON-specific yt-dlp commands.
const (
	SkipVideo     = "--skip-download"
	WriteInfoJSON = "--write-info-json"
	OutputJSON    = "-J"
)

// Sleep yt-dlp commands.
var (
	RandomizeRequests = []string{"-t", "sleep"}
)

// Downloaders

// Aria2c downloader and arg tags.
const (
	DownloaderAria = "aria2c"

	AriaLog         = "--console-log-level=notice"
	AriaLogFile     = "--log=-"
	AriaInterval    = "--summary-interval=1"
	AriaNoColor     = "--enable-color=false"
	AriaNoRPC       = "--enable-rpc=false"
	AriaShowConsole = "--show-console=true"
)
