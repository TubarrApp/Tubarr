package downloaders

import (
	"strconv"
	"strings"
)

// FFmpegProgressParser parses an ffmpeg "-progress" key=value line into a percentage of the video duration.
func FFmpegProgressParser(line string, durationSec float64) (pct float64, ok bool) {
	key, val, found := strings.Cut(strings.TrimSpace(line), "=")
	if !found {
		return 0, false
	}
	switch key {
	case "progress":
		return 100, val == "end"
	case "out_time_us":
		us, err := strconv.ParseFloat(val, 64)
		if err != nil || us < 0 || durationSec <= 0 {
			return 0, false
		}
		return min(us/1e6/durationSec*100, 100.0), true
	}
	return 0, false
}
