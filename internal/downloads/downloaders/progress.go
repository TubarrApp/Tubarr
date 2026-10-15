package downloaders

import (
	"path/filepath"
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
)

// Name resolves a yt-dlp external downloader setting (name, path, or "PROTO:NAME") to its downloader name, as yt-dlp does.
func Name(downloader string) string {
	if strings.TrimSpace(downloader) == "" {
		return ""
	}
	if proto, name, found := strings.Cut(downloader, ":"); found && len(proto) > 1 && !strings.ContainsAny(proto, `/\`) {
		downloader = name // Protocol prefix, not a Windows drive letter.
	}
	base := strings.ToLower(filepath.Base(strings.TrimSpace(downloader)))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "http" {
		return command.DownloaderHttpie
	}
	for _, name := range []string{command.DownloaderAria, command.DownloaderAxel, command.DownloaderCurl, command.DownloaderFFmpeg, command.DownloaderHttpie, command.DownloaderWget} {
		if strings.Contains(base, name) {
			return name
		}
	}
	return base
}

// ProgressParser tracks download progress across yt-dlp and external downloader output lines.
type ProgressParser struct {
	downloader          string
	durationSec         float64
	ariaTotal, ariaDone int
	pct                 float64
}

// NewProgressParser returns a parser for the given external downloader setting ("" for yt-dlp's native downloader).
//
// durationSec is only needed for ffmpeg, which reports progress as time downloaded.
func NewProgressParser(externalDownloader string, durationSec float64) *ProgressParser {
	return &ProgressParser{downloader: Name(externalDownloader), durationSec: durationSec}
}

// Parse returns the download percentage if the line reports progress.
//
// wget and httpie progress is captured by yt-dlp, so only yt-dlp's final report reaches here for them.
func (p *ProgressParser) Parse(line string) (pct float64, ok bool) {
	// yt-dlp progress (also reports final progress for external downloaders).
	if pct, ok = YTDLPProgressParser(line); ok {
		p.pct = pct
		return pct, true
	}

	switch p.downloader {
	case command.DownloaderAria:
		ok, p.ariaTotal, p.ariaDone, pct, _ = Aria2OutputParser(line, p.ariaTotal, p.ariaDone, p.pct, consts.DLStatusDownloading)
	case command.DownloaderAxel:
		pct, ok = AxelProgressParser(line)
	case command.DownloaderCurl:
		pct, ok = CurlProgressParser(line)
	case command.DownloaderFFmpeg:
		pct, ok = FFmpegProgressParser(line, p.durationSec)
	}
	if ok {
		p.pct = pct
	}
	return pct, ok
}
