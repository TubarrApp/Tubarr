// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
)

var (
	AnsiEscape,
	DLPercentage,
	ExtraSpaces,
	InvalidChars,
	SpecialChars,
	Aria2FragCountRegex *regexp.Regexp
)

// AnsiEscapeCompile compiles regex for ANSI escape codes
func AnsiEscapeCompile() *regexp.Regexp {
	if AnsiEscape == nil {
		AnsiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	}
	return AnsiEscape
}

func DLPctCompile() *regexp.Regexp {
	if DLPercentage == nil {
		DLPercentage = regexp.MustCompile(`\[download\]\s+(\d+\.?\d*)%`)
	}
	return DLPercentage
}

func Aria2FragCountCompile() *regexp.Regexp {
	if Aria2FragCountRegex == nil {
		Aria2FragCountRegex = regexp.MustCompile(`Downloading (\d+) item`)
	}
	return Aria2FragCountRegex
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
