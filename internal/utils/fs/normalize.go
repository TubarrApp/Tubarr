package utils

import (
	logging "Metarr/internal/utils/logging"
	"regexp"
	"strings"
)

// normalizeFilename removes special characters and normalizes spacing
func NormalizeFilename(filename string, specialChars, extraSpaces *regexp.Regexp) string {

	normalized := strings.ToLower(filename)
	normalized = specialChars.ReplaceAllString(normalized, "")
	normalized = extraSpaces.ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)

	return normalized
}

// trimJsonSuffixes normalizes away common json string suffixes
// e.g. ".info" for yt-dlp outputted JSON files
func TrimMetafileSuffixes(metaBase, videoBase string) string {

	switch {

	case strings.HasSuffix(metaBase, ".info.json"): // FFmpeg
		if !strings.HasSuffix(videoBase, ".info") {
			metaBase = strings.TrimSuffix(metaBase, ".info.json")
		} else {
			metaBase = strings.TrimSuffix(metaBase, ".json")
		}

	case strings.HasSuffix(metaBase, ".metadata.json"): // Angular
		if !strings.HasSuffix(videoBase, ".metadata") {
			metaBase = strings.TrimSuffix(metaBase, ".metadata.json")
		} else {
			metaBase = strings.TrimSuffix(metaBase, ".json")
		}

	case strings.HasSuffix(metaBase, ".model.json"):
		if !strings.HasSuffix(videoBase, ".model") {
			metaBase = strings.TrimSuffix(metaBase, ".model.json")
		} else {
			metaBase = strings.TrimSuffix(metaBase, ".json")
		}

	case strings.HasSuffix(metaBase, ".manifest.cdfd.json"):
		if !strings.HasSuffix(videoBase, ".manifest.cdm") {
			metaBase = strings.TrimSuffix(metaBase, ".manifest.cdfd.json")
		} else {
			metaBase = strings.TrimSuffix(metaBase, ".json")
		}

	default:
		switch {
		case !strings.HasSuffix(videoBase, ".json"): // Edge cases where metafile extension is in the suffix of the video file
			metaBase = strings.TrimSuffix(metaBase, ".json")
		case !strings.HasSuffix(videoBase, ".nfo"):
			metaBase = strings.TrimSuffix(metaBase, ".nfo")
		default:
			logging.PrintD(1, "Common suffix not found for metafile (%s)", metaBase)
		}
	}
	return metaBase
}
