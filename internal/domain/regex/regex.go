// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
	"sync"
)

const (
	ansiEscapeStr    = `\x1b\[[0-9;]*m`
	ariaItemCountStr = `Downloading\s+(\d+)\s+item`
	ariaProgressStr  = `\((\d+(?:\.\d+)?)%\)`
	dlPercentStr     = `\[download\]\s+(\d+\.?\d*)%`
	extraSpacesStr   = `\s+`
	invalidCharsStr  = `[<>:"/\\|?*\x00-\x1F]`
	specialCharsStr  = `[^\w\s-]`
	yearFragmentsStr = `(?:(\d{4})y)?(?:(\d{1,2})m)?(?:(\d{1,2})d)?`
)

// Regex expressions, compiled once.
var (
	onceAnsiEscape    sync.Once
	onceAriaItemCount sync.Once
	onceAriaProgress  sync.Once
	onceDLPercentage  sync.Once
	onceExtraSpaces   sync.Once
	onceInvalidChars  sync.Once
	onceSpecialChars  sync.Once
	onceYearFragments sync.Once

	AnsiEscape    *regexp.Regexp
	AriaItemCount *regexp.Regexp
	AriaProgress  *regexp.Regexp
	DLPercentage  *regexp.Regexp
	ExtraSpaces   *regexp.Regexp
	InvalidChars  *regexp.Regexp
	SpecialChars  *regexp.Regexp
	YearFragments *regexp.Regexp
)

// AnsiEscapeCompile compiles regex for ANSI escape codes.
func AnsiEscapeCompile() *regexp.Regexp {
	onceAnsiEscape.Do(func() {
		AnsiEscape = regexp.MustCompile(ansiEscapeStr)
	})
	return AnsiEscape
}

// AriaItemCountCompile compiles regex for Aria2 item counts.
func AriaItemCountCompile() *regexp.Regexp {
	onceAriaItemCount.Do(func() {
		AriaItemCount = regexp.MustCompile(ariaItemCountStr)
	})
	return AriaItemCount
}

// AriaProgressCompile compiles regex for Aria percentage strings.
func AriaProgressCompile() *regexp.Regexp {
	onceAriaProgress.Do(func() {
		AriaProgress = regexp.MustCompile(ariaProgressStr)
	})
	return AriaProgress
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
