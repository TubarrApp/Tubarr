// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
	"sync"
)

const (
	ariaItemCountStr = `Downloading\s+(\d+)\s+item`
	ariaProgressStr  = `\((\d+(?:\.\d+)?)%\)`
	dlPercentStr     = `\[download\]\s+(\d+\.?\d*)%`
	yearFragmentsStr = `^(\d{4})?y?(\d{1,2})?m?(\d{1,2})?d?$`
)

// Regex expressions, compiled once.
var (
	onceAriaItemCount sync.Once
	onceAriaProgress  sync.Once
	onceDLPercentage  sync.Once
	onceYearFragments sync.Once

	AriaItemCount *regexp.Regexp
	AriaProgress  *regexp.Regexp
	DLPercentage  *regexp.Regexp
	YearFragments *regexp.Regexp
)

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

// YearFragmentsCompile compiles regex for parsing user inputted dates.
func YearFragmentsCompile() *regexp.Regexp {
	onceYearFragments.Do(func() {
		YearFragments = regexp.MustCompile(yearFragmentsStr)
	})
	return YearFragments
}
