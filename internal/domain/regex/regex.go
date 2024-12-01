// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
	"sync"
)

var (
	onceAnsiEscape, onceDLPercentage, onceExtraSpaces, onceInvalidChars, onceSpecialChars, onceAria2FragCount sync.Once
	AnsiEscape, DLPercentage, ExtraSpaces, InvalidChars, SpecialChars, Aria2FragCountRegex                    *regexp.Regexp
)

// AnsiEscapeCompile compiles regex for ANSI escape codes.
func AnsiEscapeCompile() *regexp.Regexp {
	onceAnsiEscape.Do(func() {
		AnsiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	})
	return AnsiEscape
}

// DLPctCompile compiles the regex for handling regular download progress bars.
func DLPctCompile() *regexp.Regexp {
	onceDLPercentage.Do(func() {
		DLPercentage = regexp.MustCompile(`\[download\]\s+(\d+\.?\d*)%`)
	})
	return DLPercentage
}

// Aria2FragCountCompile compiles the Aria2C fragment count regex.
func Aria2FragCountCompile() *regexp.Regexp {
	onceAria2FragCount.Do(func() {
		Aria2FragCountRegex = regexp.MustCompile(`Downloading (\d+) item`)
	})
	return Aria2FragCountRegex
}

// ExtraSpacesCompile compiles regex for extra spaces.
func ExtraSpacesCompile() *regexp.Regexp {
	onceExtraSpaces.Do(func() {
		ExtraSpaces = regexp.MustCompile(`\s+`)
	})
	return ExtraSpaces
}

// InvalidCharsCompile compiles regex for invalid characters.
func InvalidCharsCompile() *regexp.Regexp {
	onceInvalidChars.Do(func() {
		InvalidChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	})
	return InvalidChars
}

// SpecialCharsCompile compiles regex for special characters.
func SpecialCharsCompile() *regexp.Regexp {
	onceSpecialChars.Do(func() {
		SpecialChars = regexp.MustCompile(`[^\w\s-]`)
	})
	return SpecialChars
}
