package downloaders

import (
	"strconv"
	"strings"
	"tubarr/internal/domain/regex"
)

// CurlProgressParser parses a curl progress meter line (default meter, or "-#" bar) into a percentage.
func CurlProgressParser(line string) (pct float64, ok bool) {
	// Default meter, e.g. " 23 5120k   23 1183k    0     0   452k      0  0:00:11  0:00:02  0:00:09  451k".
	if fields := strings.Fields(line); len(fields) == 12 {
		if p, err := strconv.Atoi(fields[0]); err == nil && p >= 0 && p <= 100 {
			return float64(p), true
		}
		return 0, false
	}

	// Progress bar, e.g. "#########                    23.4%".
	if matches := regex.CurlBarCompile().FindStringSubmatch(strings.TrimSpace(line)); len(matches) == 2 {
		if p, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return min(p, 100.0), true
		}
	}
	return 0, false
}
