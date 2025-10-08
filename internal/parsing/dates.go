package parsing

import (
	"fmt"
	"strings"

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
