package logging

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"tubarr/internal/domain/consts"
)

const (
	tagBaseLen = 1 + // "["
		len(consts.ColorBlue) +
		9 + // "Function: "
		len(consts.ColorReset) +
		3 + // " - "
		len(consts.ColorBlue) +
		5 + // "File: "
		len(consts.ColorReset) +
		3 + // " : "
		len(consts.ColorBlue) +
		5 + // "Line: "
		len(consts.ColorReset) +
		2 // "]\n"
)

var (
	Level int = -1 // Pre initialization
	mu    sync.Mutex
)

func E(l int, format string, args ...interface{}) string {
	if l >= Level {
		return ""
	}

	mu.Lock()
	defer mu.Unlock()

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	var b strings.Builder
	b.Grow(len(consts.RedError) + tagBaseLen + len(format) + (len(args) * 32))

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

func S(l int, format string, args ...interface{}) string {
	if l >= Level {
		return ""
	}

	mu.Lock()
	defer mu.Unlock()

	var b strings.Builder
	b.Grow(len(consts.GreenSuccess) + len(format) + len(consts.ColorReset) + len(" \n") + (len(args) * 32))
	b.WriteString(consts.GreenSuccess)

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteString("\n")
	msg := b.String()
	fmt.Print(msg)
	writeLog(msg, l)

	return msg
}

func D(l int, format string, args ...interface{}) string {
	if l >= Level {
		return ""
	}

	mu.Lock()
	defer mu.Unlock()

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	var b strings.Builder
	b.Grow(len(consts.YellowDebug) + tagBaseLen + len(format) + (len(args) * 32))
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

func I(format string, args ...interface{}) string {

	mu.Lock()
	defer mu.Unlock()

	var b strings.Builder
	b.Grow(len(consts.BlueInfo) + len(format) + len(consts.ColorReset) + len(" \n") + (len(args) * 32))
	b.WriteString(consts.BlueInfo)

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteString("\n")
	msg := b.String()
	fmt.Print(msg)
	writeLog(msg, 0)

	return msg
}

func P(format string, args ...interface{}) string {

	mu.Lock()
	defer mu.Unlock()

	var b strings.Builder
	b.Grow(len(format) + len(" \n") + (len(args) * 32))

	// Write formatted message
	if len(args) != 0 && args != nil {
		fmt.Fprintf(&b, format, args...)
	} else {
		b.WriteString(format)
	}

	b.WriteString("\n")
	msg := b.String()
	fmt.Print(msg)
	writeLog(msg, 0)

	return msg
}
