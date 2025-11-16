package validation

import (
	"net/url"
	"strings"
	"tubarr/internal/domain/logger"
)

// EscapedSplit allows users to escape separator characters without messing up 'strings.Split' logic.
func EscapedSplit(s string, desiredSeparator rune) []string {
	var parts []string
	var buf strings.Builder
	escaped := false

	for _, r := range s {
		switch {
		case escaped:
			// Always take the next character literally
			buf.WriteRune(r)
			escaped = false
		case r == '\\':
			// Escape next character
			escaped = true
		case r == desiredSeparator:
			// Separator
			parts = append(parts, buf.String())
			buf.Reset()
		default:
			buf.WriteRune(r)
		}
	}
	if escaped {
		// Trailing '\' treated as literal backslash
		buf.WriteRune('\\')
	}

	// Add last segment
	parts = append(parts, buf.String())
	return parts
}

// CheckForOpURL checks if a specific URL is attached to a particular meta operation.
func CheckForOpURL(op string) (chanURL string, ops string) {
	// Check if valid
	split := EscapedSplit(op, '|')
	if len(split) < 2 {
		return "", op
	}
	// Parse URL
	u := split[0]

	if _, err := url.ParseRequestURI(u); err != nil {
		logger.Pl.W("Invalid URL format grabbed as %q. Ignore this if the filter (%q) does not contain a channel URL (format is 'channel URL|filter:ops:go:here')", u, op)
		return "", strings.Join(split[1:], "|")
	}
	return u, strings.Join(split[1:], "|")
}

// DeduplicateSliceEntries removes duplicate entries in slices.
func DeduplicateSliceEntries(input []string) []string {
	if len(input) == 0 {
		return input
	}

	dedupedSlice := make([]string, 0, len(input))
	dedupMap := make(map[string]bool, len(input))

	for _, in := range input {
		if dedupMap[in] {
			logger.Pl.W("Removing duplicate of entry %q", in)
		}
		dedupMap[in] = true
		dedupedSlice = append(dedupedSlice, in)
	}
	return dedupedSlice
}
