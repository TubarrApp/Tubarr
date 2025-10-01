// Package downloaders holds logic specific to external downloaders.
package downloaders

import (
	"strconv"
	"strings"

	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/regex"
	"tubarr/internal/utils/logging"
)

// Aria2OutputParser parses the terminal output from Aria2C.
func Aria2OutputParser(line string, uri string, totalItemCount, downloadedItemCount int, currentPct float64, currentStatus consts.DownloadStatus) (parsedLine bool, totalItemsFound, downloadedItems int, pct float64, status consts.DownloadStatus) {

	// New batch beginning....
	if matches := regex.AriaItemCountCompile().FindStringSubmatch(line); matches != nil {
		count, err := strconv.Atoi(matches[1])
		if err != nil {
			logging.E(0, "Failed to parse Aria2 batch count from line %q: %v", line, err)
			count = 0
		}
		logging.D(1, "New aria2 batch detected: %d item(s)", count)
		return true, count, 0, 0.0, consts.DLStatusDownloading
	}

	// Progress percentages
	switch totalItemCount {
	// "1 item(s)", expect % progress output
	case 1:
		if matches := regex.AriaProgressCompile().FindStringSubmatch(line); len(matches) == 2 {
			if parsedPct, err := strconv.ParseFloat(matches[1], 64); err == nil {
				logging.D(1, "Parsed progress %.1f%% from line: %q", parsedPct, line)
				return true, totalItemCount, 0, parsedPct, consts.DLStatusDownloading
			}
			logging.D(1, "Failed to parse percentage from line: %q", line)
		}
		// Multiple items, track by matching "Download complete" messages against total count.
	default:
		if strings.Contains(strings.ToLower(line), "download complete") {
			downloadedItemCount++

			pct := float64(downloadedItemCount) / float64(totalItemCount) * 100
			if pct > 100.0 {
				pct = 100.0
			}
			return true, totalItemCount, downloadedItemCount, pct, consts.DLStatusDownloading
		}
	}

	// Nothing to parse
	return false, totalItemCount, downloadedItemCount, currentPct, currentStatus
}
