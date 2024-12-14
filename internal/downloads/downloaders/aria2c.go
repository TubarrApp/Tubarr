// Package downloaders holds logic specific to external downloaders.
package downloaders

import (
	"regexp"
	"strconv"
	"strings"

	"tubarr/internal/utils/logging"
)

// Aria2OutputParser parses the terminal output from Aria2C.
func Aria2OutputParser(line string, url string, totalFrags, completedFrags int) (totFrags, completeFrags int, pct float64, err error) {

	if strings.Contains(line, "Downloading") && strings.Contains(line, "item(s)") {
		logging.D(2, "Parsing Aria fragment count line: %v", line)

		if matches := regexp.MustCompile(`Downloading (\d+) item`).FindStringSubmatch(line); len(matches) > 1 {
			if total, err := strconv.Atoi(matches[1]); err == nil {
				totalFrags = total
				logging.I("%d total fragments to download for URL %q", totalFrags, url)
			}
		}
	}

	if strings.Contains(line, "Download complete:") {
		completedFrags++
		logging.D(3, "Completed fragment %d of %d for URL %q", completedFrags, totalFrags, url)
	}

	dlPct := (float64(completedFrags) / float64(totalFrags)) * 100

	if dlPct > 100.0 {
		dlPct = 100.0
	} else if dlPct < 0.0 {
		dlPct = 0.0
	}

	if totalFrags > 0 {
		return totalFrags, completedFrags, dlPct, nil
	}
	return 0, 0, 0.0, nil
}
