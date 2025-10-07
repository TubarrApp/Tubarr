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
		New: func() any {
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

// logLevel represents different logging levels
type logLevel int

const (
	levelError logLevel = iota
	levelWarn
	levelInfo
	levelDebug
	levelSuccess
	levelPlain
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

	b := builderPool.Get().(*strings.Builder) //nolint:errcheck
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
		E("Encountered error writing to console: %v", err)
	}
}

// callerInfo retrieves caller information for logging
type callerInfo struct {
	funcName string
	file     string
	line     int
	lineStr  string
}

// getCaller gets caller information from the call stack
func getCaller(skip int) callerInfo {
	pc, file, line, _ := runtime.Caller(skip)
	return callerInfo{
		funcName: filepath.Base(runtime.FuncForPC(pc).Name()),
		file:     filepath.Base(file),
		line:     line,
		lineStr:  strconv.Itoa(line),
	}
}

// buildLogMessage constructs a log message with optional caller info
func buildLogMessage(prefix, msg string, caller *callerInfo) string {
	b := builderPool.Get().(*strings.Builder) //nolint:errcheck
	b.Reset()
	defer func() {
		b.Reset()
		builderPool.Put(b)
	}()

	if caller != nil {
		// Message with caller info
		b.Grow(len(prefix) +
			len(msg) +
			1 +
			len(tagFunc) +
			len(caller.funcName) +
			len(tagFile) +
			len(caller.file) +
			len(tagLine) +
			len(caller.lineStr) +
			len(tagEnd))

		b.WriteString(prefix)
		b.WriteString(msg)

		if !strings.HasSuffix(msg, "\n") {
			b.WriteByte(' ')
		}

		b.WriteString(tagFunc)
		b.WriteString(caller.funcName)
		b.WriteString(tagFile)
		b.WriteString(caller.file)
		b.WriteString(tagLine)
		b.WriteString(caller.lineStr)
		b.WriteString(tagEnd)
	} else {
		// Simple message without caller info
		b.Grow(len(prefix) + len(msg) + 1)
		b.WriteString(prefix)
		b.WriteString(msg)
		b.WriteByte('\n')
	}

	return b.String()
}

// logToFile logs to the file logger based on level
func logToFile(level logLevel, msg string, caller *callerInfo) {
	if !Loggable {
		return
	}

	cleanMsg := regex.AnsiEscapeCompile().ReplaceAllString(msg, "")

	if caller != nil {
		// Log with caller info
		event := getZerologEvent(level).
			Str(JFunction, caller.funcName).
			Str(JFile, caller.file).
			Int(JLine, caller.line)
		event.Msg(cleanMsg)
	} else {
		// Log without caller info
		getZerologEvent(level).Msg(cleanMsg)
	}
}

// getZerologEvent returns the appropriate zerolog event for the level
func getZerologEvent(level logLevel) *zerolog.Event {
	switch level {
	case levelError:
		return fileLogger.Error()
	case levelWarn:
		return fileLogger.Warn()
	case levelDebug:
		return fileLogger.Debug()
	default:
		return fileLogger.Info()
	}
}

// log is the core logging function that handles all logging operations
func log(level logLevel, prefix, msg string, withCaller bool, args ...any) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	var caller *callerInfo
	if withCaller {
		c := getCaller(2) // Skip 2 frames: log() and the public function (E, D, W, etc.)
		caller = &c
	}

	logMsg := buildLogMessage(prefix, msg, caller)
	writeToConsole(logMsg)
	logToFile(level, msg, caller)
}

// E logs error messages
func E(msg string, args ...any) {
	log(levelError, consts.RedError, msg, true, args...)
}

// S logs success messages
func S(l int, msg string, args ...any) {
	if Level < l {
		return
	}
	log(levelSuccess, consts.GreenSuccess, msg, false, args...)
}

// D logs debug messages
func D(l int, msg string, args ...any) {
	if Level < l {
		return
	}
	log(levelDebug, consts.YellowDebug, msg, true, args...)
}

// W logs warning messages
func W(msg string, args ...any) {
	log(levelWarn, consts.YellowWarning, msg, true, args...)
}

// I logs info messages
func I(msg string, args ...any) {
	log(levelInfo, consts.BlueInfo, msg, false, args...)
}

// P logs plain messages
func P(msg string, args ...any) {
	log(levelPlain, "", msg, false, args...)
}

// AddToErrorArray adds an error to the error array under lock.
func AddToErrorArray(err error) {
	errorArray = append(errorArray, err)
}

// GetErrorArray returns the error array.
func GetErrorArray() []error {
	return errorArray
}
