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
	muErr      sync.Mutex
	errorArray = make([]error, 0, 8)
	console    = os.Stdout

	builderPool = sync.Pool{
		New: func() interface{} {
			return new(strings.Builder)
		},
	}
)

const (
	timeFormat         = "01/02 15:04:05"
	tubarrLogFile      = "tubarr.log"
	funcFileLineSingle = "%s%s [%sFunction:%s %s - %sFile:%s %s : %sLine:%s %d]\n"
	funcFileLineMulti  = "%s%s\n[%sFunction:%s %s - %sFile:%s %s : %sLine:%s %d]\n"
	JFunction          = "function"
	JFile              = "file"
	JLine              = "line"
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

	startMsg := fmt.Sprintf("=========== %v ===========\n", time.Now().Format(time.RFC1123Z))
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
func E(l int, format string, args ...interface{}) {
	if Level < l {
		return
	}

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	if len(args) > 0 {
		if err, ok := args[len(args)-1].(error); ok {
			AddToErrorArray(err)
		}
	}

	var funcFileLine string
	msg := fmt.Sprintf(format, args...)
	if strings.HasSuffix(msg, "\n") {
		funcFileLine = funcFileLineMulti
	} else {
		funcFileLine = funcFileLineSingle
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()
	lineStr := strconv.Itoa(line)

	b.Grow(len(funcFileLine) +
		len(consts.RedError) +
		len(msg) +
		len(consts.ColorDimCyan) +
		len(consts.ColorReset) +
		len(funcName) +
		len(consts.ColorDimWhite) +
		len(consts.ColorReset) +
		len(file) +
		len(consts.ColorDimWhite) +
		len(consts.ColorReset) +
		len(lineStr))

	b.WriteString(funcFileLine)
	b.WriteString(consts.RedError)
	b.WriteString(msg)
	b.WriteString(consts.ColorDimCyan)
	b.WriteString(consts.ColorReset)
	b.WriteString(funcName)
	b.WriteString(consts.ColorDimWhite)
	b.WriteString(consts.ColorReset)
	b.WriteString(file)
	b.WriteString(consts.ColorDimWhite)
	b.WriteString(consts.ColorReset)
	b.WriteString(lineStr)

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
func S(l int, format string, args ...interface{}) {
	if Level < l {
		return
	}

	msg := fmt.Sprintf(format, args...)

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
	b.WriteRune('\n')

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Info().Msg(regex.AnsiEscapeCompile().ReplaceAllString(msg, ""))
	}
}

// D logs debug messages.
func D(l int, format string, args ...interface{}) {
	if Level < l {
		return
	}

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	var funcFileLine string

	msg := fmt.Sprintf(format, args...)
	if strings.HasSuffix(msg, "\n") {
		funcFileLine = funcFileLineMulti
	} else {
		funcFileLine = funcFileLineSingle
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()
	lineStr := strconv.Itoa(line)

	b.Grow(len(funcFileLine) +
		len(consts.YellowDebug) +
		len(msg) +
		len(consts.ColorDimCyan) +
		len(consts.ColorReset) +
		len(funcName) +
		len(consts.ColorDimWhite) +
		len(consts.ColorReset) +
		len(file) +
		len(consts.ColorDimWhite) +
		len(consts.ColorReset) +
		len(lineStr))

	b.WriteString(funcFileLine)
	b.WriteString(consts.YellowDebug)
	b.WriteString(msg)
	b.WriteString(consts.ColorDimCyan)
	b.WriteString(consts.ColorReset)
	b.WriteString(funcName)
	b.WriteString(consts.ColorDimWhite)
	b.WriteString(consts.ColorReset)
	b.WriteString(file)
	b.WriteString(consts.ColorDimWhite)
	b.WriteString(consts.ColorReset)
	b.WriteString(lineStr)

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
func W(format string, args ...interface{}) {

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	var funcFileLine string

	msg := fmt.Sprintf(format, args...)
	if strings.HasSuffix(msg, "\n") {
		funcFileLine = funcFileLineMulti
	} else {
		funcFileLine = funcFileLineSingle
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()
	lineStr := strconv.Itoa(line)

	b.Grow(len(funcFileLine) +
		len(consts.YellowWarning) +
		len(msg) +
		len(consts.ColorDimCyan) +
		len(consts.ColorReset) +
		len(funcName) +
		len(consts.ColorDimWhite) +
		len(consts.ColorReset) +
		len(file) +
		len(consts.ColorDimWhite) +
		len(consts.ColorReset) +
		len(lineStr))

	b.WriteString(funcFileLine)
	b.WriteString(consts.YellowWarning)
	b.WriteString(msg)
	b.WriteString(consts.ColorDimCyan)
	b.WriteString(consts.ColorReset)
	b.WriteString(funcName)
	b.WriteString(consts.ColorDimWhite)
	b.WriteString(consts.ColorReset)
	b.WriteString(file)
	b.WriteString(consts.ColorDimWhite)
	b.WriteString(consts.ColorReset)
	b.WriteString(lineStr)

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
func I(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)

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
	b.WriteRune('\n')

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Info().Msg(regex.AnsiEscapeCompile().ReplaceAllString(msg, ""))
	}
}

// P logs plain messages.
func P(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	b.Grow(len(msg) + 1)

	b.WriteString(msg)
	b.WriteRune('\n')

	writeToConsole(b.String())
	if Loggable {
		fileLogger.Info().Msg(msg)
	}
}

// AddToErrorArray adds an error to the error array under lock.
func AddToErrorArray(err error) {
	muErr.Lock()
	if err != nil {
		errorArray = append(errorArray, err)
	}
	muErr.Unlock()
}

// GetErrorArray returns the error array.
func GetErrorArray() []error {
	return errorArray
}
