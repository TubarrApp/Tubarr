package validation

import (
	"errors"
	"strings"
)

// parseOp allows users to escape colons without messing up 'strings.Split' logic.
func parseOp(op string) ([]string, error) {
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

		case r == ':':
			// Separator
			parts = append(parts, buf.String())
			buf.Reset()

		default:
			buf.WriteRune(r)
		}
	}

	if escaped {
		return nil, errors.New("invalid escape at end of string")
	}

	// Add last segment
	parts = append(parts, buf.String())

	return parts, nil
}
