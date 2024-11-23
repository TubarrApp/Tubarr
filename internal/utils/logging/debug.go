package logging

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
)

const (
	tagBaseLen = 15 + // 01/02 15:04:05(space)
		1 + // "["
		len(consts.ColorBlue) +
		10 + // "Function: "
		len(consts.ColorReset) +
		3 + // " - "
		len(consts.ColorBlue) +
		6 + // "File: "
		len(consts.ColorReset) +
		3 + // " : "
		len(consts.ColorBlue) +
		6 + // "Line: "
		len(consts.ColorReset) +
		2 // "]\n"
)

var (
	Level int = -1 // Pre initialization
)

// Log Error:
//
// Print and log a message of the error type.
func E(l int, format string, args ...interface{}) string {
	if Level < l {
		return ""
	}

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	var b strings.Builder
	b.Grow(len(consts.RedError) + tagBaseLen + len(format) + (len(args) * 32) + len(funcName) + len(file) + line)

	b.WriteString(time.Now().Format("01/02 15:04:05"))
	b.WriteRune(' ')
	b.WriteString(consts.RedError)

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteRune('[')
	b.WriteString(consts.ColorBlue)
	b.WriteString("Function: ")
	b.WriteString(consts.ColorReset)
	b.WriteString(funcName)
	b.WriteString(" - ")
	b.WriteString(consts.ColorBlue)
	b.WriteString("File: ")
	b.WriteString(consts.ColorReset)
	b.WriteString(file)
	b.WriteString(" : ")
	b.WriteString(consts.ColorBlue)
	b.WriteString("Line: ")
	b.WriteString(consts.ColorReset)
	b.WriteString(strconv.Itoa(line))
	b.WriteString("]\n")

	msg := b.String()

	fmt.Print(msg)
	writeLog(msg, l)

	return msg
}

// Log Success:
//
// Print and log a message of the success type.
func S(l int, format string, args ...interface{}) string {
	if Level < l {
		return ""
	}

	var b strings.Builder
	b.Grow(len(consts.GreenSuccess) + len(format) + len(consts.ColorReset) + 1 + (len(args) * 32))

	b.WriteString(time.Now().Format("01/02 15:04:05"))
	b.WriteRune(' ')
	b.WriteString(consts.GreenSuccess)

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteRune('\n')
	msg := b.String()
	fmt.Print(msg)
	writeLog(msg, l)

	return msg
}

// Log Debug:
//
// Print and log a message of the debug type.
func D(l int, format string, args ...interface{}) string {
	if Level < l {
		return ""
	}

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	var b strings.Builder
	b.Grow(len(consts.YellowDebug) + tagBaseLen + len(format) + (len(args) * 32) + len(funcName) + len(file) + line)

	b.WriteString(time.Now().Format("01/02 15:04:05"))
	b.WriteRune(' ')
	b.WriteString(consts.YellowDebug)

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteRune('[')
	b.WriteString(consts.ColorBlue)
	b.WriteString("Function: ")
	b.WriteString(consts.ColorReset)
	b.WriteString(funcName)
	b.WriteString(" - ")
	b.WriteString(consts.ColorBlue)
	b.WriteString("File: ")
	b.WriteString(consts.ColorReset)
	b.WriteString(file)
	b.WriteString(" : ")
	b.WriteString(consts.ColorBlue)
	b.WriteString("Line: ")
	b.WriteString(consts.ColorReset)
	b.WriteString(strconv.Itoa(line))
	b.WriteString("]\n")

	msg := b.String()

	fmt.Print(msg)
	writeLog(msg, l)

	return msg
}

// Log Info:
//
// Print and log a message of the info type.
func I(format string, args ...interface{}) string {

	var b strings.Builder
	b.Grow(len(consts.BlueInfo) + len(format) + len(consts.ColorReset) + 1 + (len(args) * 32))

	b.WriteString(time.Now().Format("01/02 15:04:05"))
	b.WriteRune(' ')
	b.WriteString(consts.BlueInfo)

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteRune('\n')
	msg := b.String()
	fmt.Print(msg)
	writeLog(msg, 0)

	return msg
}

// Log:
//
// Print and log a plain message.
func P(format string, args ...interface{}) string {

	var b strings.Builder
	b.Grow(len(format) + 1 + (len(args) * 32))

	b.WriteString(time.Now().Format("01/02 15:04:05"))
	b.WriteRune(' ')

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteRune('\n')
	msg := b.String()
	fmt.Print(msg)
	writeLog(msg, 0)

	return msg
}
