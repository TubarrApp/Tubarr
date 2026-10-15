package downloaders

import (
	"strconv"
	"tubarr/internal/domain/regex"
)

// AxelProgressParser parses an Axel progress line, e.g. "[ 45%]  .......... [ 931.0KB/s]", into a percentage.
func AxelProgressParser(line string) (pct float64, ok bool) {
	matches := regex.AxelProgressCompile().FindStringSubmatch(line)
	if len(matches) != 2 {
		return 0, false
	}
	p, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}
	return min(p, 100.0), true
}
