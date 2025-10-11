package parsing

import (
	"fmt"
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

	b := strings.Builder{}
	b.Grow(10)

	b.WriteString(d[0:4])
	b.WriteByte('-')
	b.WriteString(d[4:6])
	b.WriteByte('-')
	b.WriteString(d[6:8])

	return b.String()
}

// ParseWordDate parses and formats the inputted word date (e.g. Jan 2nd, 2006).
func ParseWordDate(dateString string) (string, error) {
	t, err := dateparse.ParseAny(dateString)
	if err != nil {
		return "", fmt.Errorf("unable to parse date: %s", dateString)
	}
	return t.Format("2006-01-02"), nil
}

// FormatDuration00h00m formats a duration in '10h 15m' format.
func FormatDuration00h00m(d time.Duration) string {
	if d < time.Minute {
		return "less than 1m"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}
