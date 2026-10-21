package metarr

import (
	"strconv"
	"strings"
	"sync"
)

// Progress is a running Metarr transcode's latest FFmpeg progress.
type Progress struct {
	Percent float64 `json:"percent"` // -1 when the video's duration is unknown.
	Frame   int64   `json:"frame"`
	FPS     float64 `json:"fps"`
	Speed   string  `json:"speed"`
}

// progressByVideo holds running videos' latest progress, by video ID.
var progressByVideo sync.Map

// VideoProgress returns a running video's latest transcode progress, or nil if it has none.
func VideoProgress(videoID int64) *Progress {
	p, ok := progressByVideo.Load(videoID)
	if !ok {
		return nil
	}
	progress, _ := p.(Progress)
	return &progress
}

// parseProgress reads a Metarr progress line, e.g. "metarr progress: pct=25.0 frame=120 fps=30.0 speed=1.52x".
func parseProgress(line string) (p Progress, ok bool) {
	rest, ok := strings.CutPrefix(line, "metarr progress: ")
	if !ok {
		return p, false
	}
	p.Percent = -1
	for field := range strings.FieldsSeq(rest) {
		key, val, _ := strings.Cut(field, "=")
		switch key {
		case "pct":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				p.Percent = f
			}
		case "frame":
			p.Frame, _ = strconv.ParseInt(val, 10, 64)
		case "fps":
			p.FPS, _ = strconv.ParseFloat(val, 64)
		case "speed":
			p.Speed = val
		}
	}
	return p, true
}
