package parsing

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
)

// HyphenateYyyyMmDd simply hyphenates yyyy-mm-dd date values for display.
func HyphenateYyyyMmDd(d string) string {
	d = strings.ReplaceAll(d, " ", "")
	d = strings.ReplaceAll(d, "-", "")
	if len(d) < 8 {
		return d
	}

	return d[0:4] + "-" + d[4:6] + "-" + d[6:8]
}

// ParseWordDate parses and formats the inputted word date (e.g. Jan 2nd, 2006).
func ParseWordDate(dateString string) (string, error) {
	t, err := dateparse.ParseAny(dateString)
	if err != nil {
		return "", fmt.Errorf("unable to parse date: %s", dateString)
	}
	return t.Format("2006-01-02"), nil
}

// ParseDuration parses a duration in seconds, as "1:02:03", "62:03", "3723", or ISO 8601 "P1DT1H2M3S", returning 0 if invalid.
func ParseDuration(s string) int {
	s = strings.TrimSpace(s)
	if secs, err := strconv.ParseFloat(s, 64); err == nil {
		return max(int(secs), 0)
	}
	if rest, ok := strings.CutPrefix(strings.ToUpper(s), "P"); ok {
		// Days come before the "T", and the time after it.
		days := 0
		if d, after, found := strings.Cut(rest, "D"); found {
			n, err := strconv.Atoi(d)
			if err != nil || n < 0 {
				return 0
			}
			days = n
			rest = after
		}
		secs := 0
		if rest = strings.TrimPrefix(rest, "T"); rest != "" {
			d, err := time.ParseDuration(strings.ToLower(rest))
			if err != nil || d < 0 {
				return 0
			}
			secs = int(d.Seconds())
		}
		return days*24*60*60 + secs
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}
