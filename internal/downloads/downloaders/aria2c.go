// Package downloaders holds logic specific to external downloaders.
package downloaders

import (
	"regexp"
	"strconv"
	"strings"

	"tubarr/internal/domain/consts"
	"tubarr/internal/utils/logging"
)

// Aria2OutputParser parses the terminal output from Aria2C.
func Aria2OutputParser(line string, uri string, currentPct float64, currentStatus consts.DownloadStatus) (parsedLine bool, pct float64, status consts.DownloadStatus) {

	// New batch beginning....
	if strings.Contains(line, "Downloading") && strings.Contains(line, "item") {
		logging.D(1, "New aria2 batch detected: %s", line)
		return true, 0.0, consts.DLStatusDownloading
	}

	// Progress percentages
	re := regexp.MustCompile(`\((\d+(?:\.\d+)?)%\)`)
	if matches := re.FindStringSubmatch(line); len(matches) == 2 {
		if parsedPct, err := strconv.ParseFloat(matches[1], 64); err == nil {
			logging.D(1, "Parsed progress %.1f%% from line: %q", parsedPct, line)
			return true, parsedPct, consts.DLStatusDownloading
		}
		logging.D(1, "Failed to parse percentage from line: %q", line)
	}

	// Final filename printout, command is done
	if strings.HasPrefix(line, "/") {
		line = strings.Split(line, ",progress:")[0]

		logging.I("Checking output filename for video URL %q: %q", uri, line)
		if !strings.HasSuffix(line, ".part") {

			for _, validExt := range consts.AllVidExtensions {
				if strings.HasSuffix(line, validExt) {
					return true, 100.0, consts.DLStatusCompleted
				}
			}
		}
	}

	// Nothing to parse
	return false, currentPct, currentStatus
}
