package validation

import (
	"net/url"
	"strings"
	"tubarr/internal/utils/logging"
)

// parseOp allows users to escape separator characters without messing up 'strings.Split' logic.
func parseOp(op string, desiredSeparator rune) []string {
	var parts []string
	var buf strings.Builder
	escaped := false

	for _, r := range op {
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
	split := parseOp(op, '|')
	if len(split) < 2 {
		return "", op
	}

	u := split[0]

	if _, err := url.ParseRequestURI(u); err != nil {
		logging.E(1, "invalid URL format grabbed as %q. Ignore this if the filter (%q) does not contain a channel URL (format is 'channel URL|filter:ops:go:here')", u, op)
		return "", strings.Join(split[1:], "|")
	}

	return u, strings.Join(split[1:], "|")
}
