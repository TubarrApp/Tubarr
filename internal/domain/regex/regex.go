// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
	"sync"
)

const (
	ariaItemCountStr = `Downloading\s+(\d+)\s+item`
	ariaProgressStr  = `\((\d+(?:\.\d+)?)%\)`
	axelProgressStr  = `^\[\s*(\d+)%\]`
	curlBarStr       = `#\s*(\d+(?:\.\d+)?)%$`
	wgetDotStr       = `^\s*\d+[KMGT]\s*(\d{1,3})%`
	dlPercentStr     = `\[download\]\s+(\d+\.?\d*)%`
	yearFragmentsStr = `^(\d{4})?y?(\d{1,2})?m?(\d{1,2})?d?$`
)

// Regex expressions, compiled once.
var (
	onceAriaItemCount sync.Once
	onceAriaProgress  sync.Once
	onceAxelProgress  sync.Once
	onceCurlBar       sync.Once
	onceDLPercentage  sync.Once
	onceWgetDot       sync.Once
	onceYearFragments sync.Once

	AriaItemCount *regexp.Regexp
	AriaProgress  *regexp.Regexp
	AxelProgress  *regexp.Regexp
	CurlBar       *regexp.Regexp
	DLPercentage  *regexp.Regexp
	WgetDot       *regexp.Regexp
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

// AxelProgressCompile compiles regex for Axel "[ 45%]" progress lines.
func AxelProgressCompile() *regexp.Regexp {
	onceAxelProgress.Do(func() {
		AxelProgress = regexp.MustCompile(axelProgressStr)
	})
	return AxelProgress
}

// CurlBarCompile compiles regex for curl "-#" progress bars.
func CurlBarCompile() *regexp.Regexp {
	onceCurlBar.Do(func() {
		CurlBar = regexp.MustCompile(curlBarStr)
	})
	return CurlBar
}

// DownloadPercentCompile compiles the regex for handling regular download progress bars.
func DownloadPercentCompile() *regexp.Regexp {
	onceDLPercentage.Do(func() {
		DLPercentage = regexp.MustCompile(dlPercentStr)
	})
	return DLPercentage
}

// WgetDotCompile compiles regex for wget "dot" progress lines.
func WgetDotCompile() *regexp.Regexp {
	onceWgetDot.Do(func() {
		WgetDot = regexp.MustCompile(wgetDotStr)
	})
	return WgetDot
}

// YearFragmentsCompile compiles regex for parsing user inputted dates.
func YearFragmentsCompile() *regexp.Regexp {
	onceYearFragments.Do(func() {
		YearFragments = regexp.MustCompile(yearFragmentsStr)
	})
	return YearFragments
}
