// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
	"sync"
)

const (
	ansiEscapeStr    = `\x1b\[[0-9;]*m`
	dlPercentStr     = `\[download\]\s+(\d+\.?\d*)%`
	ariaFragCountStr = `Downloading (\d+) item`
	extraSpacesStr   = `\s+`
	invalidCharsStr  = `[<>:"/\\|?*\x00-\x1F]`
	specialCharsStr  = `[^\w\s-]`
	yearFragmentsStr = `(?:(\d{4})y)?(?:(\d{1,2})m)?(?:(\d{1,2})d)?`
)

var (
	onceAnsiEscape, onceDLPercentage, onceExtraSpaces, onceInvalidChars, onceSpecialChars, onceAria2FragCount, onceYearFragments sync.Once
	AnsiEscape, DLPercentage, ExtraSpaces, InvalidChars, SpecialChars, Aria2FragCountRegex, YearFragments                        *regexp.Regexp
)

// AnsiEscapeCompile compiles regex for ANSI escape codes.
func AnsiEscapeCompile() *regexp.Regexp {
	onceAnsiEscape.Do(func() {
		AnsiEscape = regexp.MustCompile(ansiEscapeStr)
	})
	return AnsiEscape
}

// Aria2FragCountCompile compiles the Aria2C fragment count regex.
func Aria2FragCountCompile() *regexp.Regexp {
	onceAria2FragCount.Do(func() {
		Aria2FragCountRegex = regexp.MustCompile(ariaFragCountStr)
	})
	return Aria2FragCountRegex
}

// DownloadPercentCompile compiles the regex for handling regular download progress bars.
func DownloadPercentCompile() *regexp.Regexp {
	onceDLPercentage.Do(func() {
		DLPercentage = regexp.MustCompile(dlPercentStr)
	})
	return DLPercentage
}

// ExtraSpacesCompile compiles regex for extra spaces.
func ExtraSpacesCompile() *regexp.Regexp {
	onceExtraSpaces.Do(func() {
		ExtraSpaces = regexp.MustCompile(extraSpacesStr)
	})
	return ExtraSpaces
}

// InvalidCharsCompile compiles regex for invalid characters.
func InvalidCharsCompile() *regexp.Regexp {
	onceInvalidChars.Do(func() {
		InvalidChars = regexp.MustCompile(invalidCharsStr)
	})
	return InvalidChars
}

// SpecialCharsCompile compiles regex for special characters.
func SpecialCharsCompile() *regexp.Regexp {
	onceSpecialChars.Do(func() {
		SpecialChars = regexp.MustCompile(specialCharsStr)
	})
	return SpecialChars
}

// YearFragmentsCompile compiles regex for parsing user inputted dates.
func YearFragmentsCompile() *regexp.Regexp {
	onceYearFragments.Do(func() {
		YearFragments = regexp.MustCompile(yearFragmentsStr)
	})
	return YearFragments
}
