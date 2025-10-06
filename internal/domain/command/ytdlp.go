package command

// General
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

// Scrape
const (
	YtDLPFlatPlaylist = "--flat-playlist"
)

// JSON only
const (
	SkipVideo     = "--skip-download"
	WriteInfoJSON = "--write-info-json"
	OutputJSON    = "-J"
)

var (
	RandomizeRequests = []string{"-t", "sleep"}
)

// Downloaders

// Aria2c:
const (
	DownloaderAria = "aria2c"
)

const (
	AriaLog         = "--console-log-level=notice"
	AriaLogFile     = "--log=-"
	AriaInterval    = "--summary-interval=1"
	AriaNoColor     = "--enable-color=false"
	AriaNoRPC       = "--enable-rpc=false"
	AriaShowConsole = "--show-console=true"
)
