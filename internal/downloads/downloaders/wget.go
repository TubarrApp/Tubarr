package downloaders

import (
	"strconv"
	"tubarr/internal/domain/regex"
)

// WgetProgressParser parses a wget "dot" progress line, e.g. "   384K 14% 1.14M 3s", into a percentage.
func WgetProgressParser(line string) (pct float64, ok bool) {
	matches := regex.WgetDotCompile().FindStringSubmatch(line)
	if len(matches) != 2 {
		return 0, false
	}
	p, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}
	return min(p, 100.0), true
}
