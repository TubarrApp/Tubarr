// Package logging handles the printing and writing of debug and log messages.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/regex"

	"github.com/rs/zerolog"
)

var (
	Level      int  = -1
	Loggable   bool = false
	fileLogger zerolog.Logger
	errorArray = make([]error, 0, 8)
	console    = os.Stdout

	builderPool = sync.Pool{
		New: func() interface{} {
			return new(strings.Builder)
		},
	}
)

const (
	timeFormat    = "01/02 15:04:05"
	tubarrLogFile = "tubarr.log"

	tagFunc = "[" + consts.ColorDimCyan + "Function:" + consts.ColorReset + " "
	tagFile = " - " + consts.ColorDimCyan + "File:" + consts.ColorReset + " "
	tagLine = " : " + consts.ColorDimCyan + "Line:" + consts.ColorReset + " "
	tagEnd  = "]\n"

	JFunction = "function"
	JFile     = "file"
	JLine     = "line"
)

func init() {
	zerolog.TimeFieldFormat = time.RFC3339
}

// SetupLogging sets up logging for the application.
func SetupLogging(targetDir string) error {
	logfile, err := os.OpenFile(
		filepath.Join(targetDir, tubarrLogFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}

	// File logger using zerolog's efficient JSON logging
	fileLogger = zerolog.New(logfile).With().Timestamp().Logger()
	Loggable = true

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.WriteString("=========== ")
	b.WriteString(time.Now().Format(time.RFC1123Z))
	b.WriteString(" ===========")
	b.WriteByte('\n')

	startMsg := b.String()
	writeToConsole(startMsg)
	fileLogger.Info().Msg(regex.AnsiEscapeCompile().ReplaceAllString(startMsg, ""))

	return nil
}

// writeToConsole writes messages to console without using zerolog (zerolog parses JSON, inefficient).
func writeToConsole(msg string) {
	timestamp := time.Now().Format(timeFormat)
	if _, err := fmt.Fprintf(console, "%s%s%s %s", consts.ColorBrightBlack, timestamp, consts.ColorReset, msg); err != nil {
		E(0, "Encountered error writing to console: %v", err)
	}
}

// E logs error messages, and appends to the global error array.
func E(l int, msg string, args ...interface{}) {
	if Level < l {
		return
	}

	pc, file, line, _ := runtime.Caller(1)
	lineStr := strconv.Itoa(line)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.Grow(len(consts.RedError) +
		len(msg) +
		1 +
		len(tagFunc) +
		len(funcName) +
		len(tagFile) +
		len(file) +
		len(tagLine) +
		len(lineStr) +
		len(tagEnd))

	b.WriteString(consts.RedError)
	b.WriteString(msg)

	if !strings.HasSuffix(msg, "\n") {
		b.WriteByte(' ')
	}

	b.WriteString(tagFunc)
	b.WriteString(funcName)
	b.WriteString(tagFile)
	b.WriteString(file)
	b.WriteString(tagLine)
	b.WriteString(lineStr)
	b.WriteString(tagEnd)

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Error().
			Str(JFunction, funcName).
			Str(JFile, file).
			Int(JLine, line).
			Msg(regex.AnsiEscapeCompile().ReplaceAllString(msg, ""))
	}
}

// S logs success messages.
func S(l int, msg string, args ...interface{}) {
	if Level < l {
		return
	}

	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.Grow(len(consts.GreenSuccess) +
		len(msg) + 1)

	b.WriteString(consts.GreenSuccess)
	b.WriteString(msg)
	b.WriteByte('\n')

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Info().Msg(regex.AnsiEscapeCompile().ReplaceAllString(msg, ""))
	}
}

// D logs debug messages.
func D(l int, msg string, args ...interface{}) {
	if Level < l {
		return
	}

	pc, file, line, _ := runtime.Caller(1)
	lineStr := strconv.Itoa(line)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.Grow(len(consts.YellowDebug) +
		len(msg) +
		1 +
		len(tagFunc) +
		len(funcName) +
		len(tagFile) +
		len(file) +
		len(tagLine) +
		len(lineStr) +
		len(tagEnd))

	b.WriteString(consts.YellowDebug)
	b.WriteString(msg)

	if !strings.HasSuffix(msg, "\n") {
		b.WriteByte(' ')
	}

	b.WriteString(tagFunc)
	b.WriteString(funcName)
	b.WriteString(tagFile)
	b.WriteString(file)
	b.WriteString(tagLine)
	b.WriteString(lineStr)
	b.WriteString(tagEnd)

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Debug().
			Str(JFunction, funcName).
			Str(JFile, file).
			Int(JLine, line).
			Msg(regex.AnsiEscapeCompile().ReplaceAllString(msg, ""))
	}
}

// W logs debug messages.
func W(msg string, args ...interface{}) {

	pc, file, line, _ := runtime.Caller(1)
	lineStr := strconv.Itoa(line)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.Grow(len(consts.YellowWarning) +
		len(msg) +
		1 +
		len(tagFunc) +
		len(funcName) +
		len(tagFile) +
		len(file) +
		len(tagLine) +
		len(lineStr) +
		len(tagEnd))

	b.WriteString(consts.YellowWarning)
	b.WriteString(msg)

	if !strings.HasSuffix(msg, "\n") {
		b.WriteByte(' ')
	}

	b.WriteString(tagFunc)
	b.WriteString(funcName)
	b.WriteString(tagFile)
	b.WriteString(file)
	b.WriteString(tagLine)
	b.WriteString(lineStr)
	b.WriteString(tagEnd)

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Warn().
			Str(JFunction, funcName).
			Str(JFile, file).
			Int(JLine, line).
			Msg(regex.AnsiEscapeCompile().ReplaceAllString(msg, ""))
	}
}

// I logs info messages.
func I(msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.Grow(len(consts.BlueInfo) +
		len(msg) + 1)

	b.WriteString(consts.BlueInfo)
	b.WriteString(msg)
	b.WriteByte('\n')

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Info().Msg(regex.AnsiEscapeCompile().ReplaceAllString(msg, ""))
	}
}

// P logs plain messages.
func P(msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.Grow(len(msg) + 1)

	b.WriteString(msg)
	b.WriteByte('\n')

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Info().Msg(msg)
	}
}

// AddToErrorArray adds an error to the error array under lock.
func AddToErrorArray(err error) {
	errorArray = append(errorArray, err)
}

// GetErrorArray returns the error array.
func GetErrorArray() []error {
	return errorArray
}
