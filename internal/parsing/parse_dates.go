package parsing

import (
	"fmt"

	"github.com/araddon/dateparse"
)

// ParseWordDate parses and formats the inputted word date (e.g. Jan 2nd, 2006).
func ParseWordDate(dateString string) (string, error) {

	t, err := dateparse.ParseAny(dateString)
	if err != nil {
		return "", fmt.Errorf("unable to parse date: %s", dateString)
	}

	return t.Format("2006-01-02"), nil
}
