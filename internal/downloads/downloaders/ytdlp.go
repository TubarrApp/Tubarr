package downloaders

import (
	"strconv"
	"strings"
	"tubarr/internal/domain/command"
)

// YTDLPProgressParser parses a yt-dlp progress line (see command.ProgressTemplateFmt) into a percentage.
func YTDLPProgressParser(line string) (pct float64, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), command.ProgressMarker)
	if !found {
		return 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) != 5 {
		return 0, false
	}

	// Unknown values are printed as "NA", which parse to 0.
	num := func(s string) float64 {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return f
	}
	downloaded, total, estimate, fragIdx, fragCount := num(fields[0]), num(fields[1]), num(fields[2]), num(fields[3]), num(fields[4])

	switch {
	case total > 0:
		pct = downloaded / total * 100
	case estimate > 0:
		pct = downloaded / estimate * 100
	case fragCount > 0:
		pct = fragIdx / fragCount * 100
	default:
		return 0, false
	}
	return min(pct, 100.0), true
}
