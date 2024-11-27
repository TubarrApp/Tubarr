// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
)

var (
	AnsiEscape   *regexp.Regexp
	ExtraSpaces  *regexp.Regexp
	InvalidChars *regexp.Regexp
	SpecialChars *regexp.Regexp
)

// AnsiEscapeCompile compiles regex for ANSI escape codes
func AnsiEscapeCompile() *regexp.Regexp {
	if AnsiEscape == nil {
		AnsiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	}
	return AnsiEscape
}

// ExtraSpacesCompile compiles regex for extra spaces
func ExtraSpacesCompile() *regexp.Regexp {
	if ExtraSpaces == nil {
		ExtraSpaces = regexp.MustCompile(`\s+`)
	}
	return ExtraSpaces
}

// InvalidCharsCompile compiles regex for invalid characters
func InvalidCharsCompile() *regexp.Regexp {
	if InvalidChars == nil {
		InvalidChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	}
	return InvalidChars
}

// SpecialCharsCompile compiles regex for special characters
func SpecialCharsCompile() *regexp.Regexp {
	if SpecialChars == nil {
		SpecialChars = regexp.MustCompile(`[^\w\s-]`)
	}
	return SpecialChars
}
