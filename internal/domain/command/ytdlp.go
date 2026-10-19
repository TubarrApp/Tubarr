// Package command holds constants used for building argument strings for commands.
package command

// DownloadDestinationPrefix starts the yt-dlp output line naming each file it downloads to.
const DownloadDestinationPrefix = "[download] Destination: "

// General yt-dlp commands.
const (
	AfterMove            = "after_move:%(filepath)s"
	CookiesFromBrowser   = "--cookies-from-browser"
	AddHeader            = "--add-header" // Accepted shortening of AddHeaders.
	AddHeaders           = "--add-headers"
	CookiePath           = "--cookies"
	EndOfOptions         = "--" // Placed before the URL, so a URL starting with "-" can't be read as an option.
	ExternalDLer         = "--external-downloader"
	ExternalDLArgs       = "--external-downloader-args"
	FilenameSyntax       = "%(title).200B [%(id)s].%(ext)s" // The ID keeps apart same-titled videos. 200 bytes fits the 255 byte name limit.
	Impersonate          = "--impersonate"
	UserAgent            = "--user-agent"
	MaxFilesize          = "--max-filesize"
	Output               = "-o"
	P                    = "-P"
	Print                = "--print"
	Progress             = "--progress" // Needed as --print implies --quiet.
	ProgressNewline      = "--newline"
	ProgressTemplate     = "--progress-template"
	RestrictFilenames    = "--restrict-filenames"
	Retries              = "--retries"
	YTDLP                = "yt-dlp"
	YtDLPOutputExtension = "--merge-output-format"
)

// Progress line format (fields: downloaded bytes, total bytes, estimated total bytes, fragment index, fragment count).
const (
	ProgressMarker      = "[tubarr-progress]"
	ProgressTemplateFmt = "download:" + ProgressMarker +
		" %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s" +
		" %(progress.fragment_index)s %(progress.fragment_count)s"
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
	SleepRequests = []string{"-t", "sleep"}
)

// Downloaders

// External downloaders supported by yt-dlp.
const (
	DownloaderAxel   = "axel"
	DownloaderCurl   = "curl"
	DownloaderFFmpeg = "ffmpeg"
	DownloaderHttpie = "httpie"
	DownloaderWget   = "wget"
)

// Args making external downloaders report progress where Tubarr can read it.
const (
	FFmpegProgress = "-progress pipe:1 -nostats"                            // key=value progress lines on stdout.
	WgetProgress   = "--show-progress --progress=dot:binary -o /dev/stdout" // yt-dlp passes -nv and captures stderr, so log progress to stdout.
)

// HttpieConfigDirEnv is the environment variable setting httpie's config directory.
const HttpieConfigDirEnv = "HTTPIE_CONFIG_DIR"

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
