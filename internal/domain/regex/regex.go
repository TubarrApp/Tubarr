// Package regex compiles and caches various regex expressions.
package regex

import (
	"regexp"
	"sync/atomic"
)

var (
	AnsiEscape,
	DLPercentage,
	ExtraSpaces,
	InvalidChars,
	SpecialChars,
	Aria2FragCountRegex atomic.Pointer[regexp.Regexp]
)

// AnsiEscapeCompile compiles regex for ANSI escape codes
func AnsiEscapeCompile() *regexp.Regexp {
	if rx := AnsiEscape.Load(); rx != nil {
		return rx
	}
	rx := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	AnsiEscape.Store(rx)
	return rx
}

// DLPctCompile compiles regex for parsing current download progress percentage.
func DLPctCompile() *regexp.Regexp {
	if rx := DLPercentage.Load(); rx != nil {
		return rx
	}
	rx := regexp.MustCompile(`\[download\]\s+(\d+\.?\d*)%`)
	DLPercentage.Store(rx)
	return rx
}

// Aria2FragCountCompile compiles regex for grabbing the number of fragments for an Aria2 download.
func Aria2FragCountCompile() *regexp.Regexp {
	if rx := Aria2FragCountRegex.Load(); rx != nil {
		return rx
	}
	rx := regexp.MustCompile(`Downloading (\d+) item`)
	Aria2FragCountRegex.Store(rx)
	return rx
}

// ExtraSpacesCompile compiles regex for extra spaces
func ExtraSpacesCompile() *regexp.Regexp {
	if rx := ExtraSpaces.Load(); rx != nil {
		return rx
	}
	rx := regexp.MustCompile(`\s+`)
	ExtraSpaces.Store(rx)
	return rx
}

// InvalidCharsCompile compiles regex for invalid characters
func InvalidCharsCompile() *regexp.Regexp {
	if rx := InvalidChars.Load(); rx != nil {
		return rx
	}
	rx := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	InvalidChars.Store(rx)
	return rx
}

// SpecialCharsCompile compiles regex for special characters
func SpecialCharsCompile() *regexp.Regexp {
	if rx := AnsiEscape.Load(); rx != nil {
		return rx
	}
	rx := regexp.MustCompile(`[^\w\s-]`)
	AnsiEscape.Store(rx)
	return rx
}
