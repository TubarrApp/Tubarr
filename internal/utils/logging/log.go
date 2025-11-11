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

// Global logging variables.
var (
	Level    = -1
	Loggable = false
)

// Local logging variables.
var (
	fileLogger zerolog.Logger
	console    = os.Stdout
)

// LogBuilder wraps strings.Builder for logging with automatic pooling.
type logBuilder struct {
	*strings.Builder
}

var logBuilderPool = sync.Pool{
	New: func() any {
		return &logBuilder{
			Builder: &strings.Builder{},
		}
	},
}

// getLogBuilder retrieves a builder from the pool.
func getLogBuilder() *logBuilder {
	lb := logBuilderPool.Get().(*logBuilder)
	lb.Reset()
	return lb
}

// Release returns the builder to the pool.
func (lb *logBuilder) Release() {
	if lb == nil || lb.Builder == nil {
		return
	}

	// Prevent pool bloat from huge messages
	const maxPooledSize = 4096
	if lb.Cap() <= maxPooledSize {
		lb.Reset()
		logBuilderPool.Put(lb)
	}
}

const (
	timeFormat    = "01/02 15:04:05"
	tubarrLogFile = "tubarr.log"

	tagFunc = "[" + consts.ColorDimCyan + "Function:" + consts.ColorReset + " "
	tagFile = " - " + consts.ColorDimCyan + "File:" + consts.ColorReset + " "
	tagLine = " : " + consts.ColorDimCyan + "Line:" + consts.ColorReset + " "
	tagEnd  = "]\n"

	jFunction = "function"
	jFile     = "file"
	jLine     = "line"
)

// logLevel represents different logging levels
type logType int

const (
	logError logType = iota
	logWarn
	logInfo
	logDebug
	logSuccess
	logPrint
)

func init() {
	zerolog.TimeFieldFormat = time.RFC3339
}

// SetupLogging sets up logging for the application.
func SetupLogging(targetDir string) error {
	logfile, err := os.OpenFile(
		filepath.Join(targetDir, tubarrLogFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		consts.PermsLogFile,
	)
	if err != nil {
		return err
	}

	// File logger using zerolog's efficient JSON logging
	fileLogger = zerolog.New(logfile).With().Timestamp().Logger()
	Loggable = true

	b := getLogBuilder()
	defer b.Release()

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
	b := getLogBuilder()
	defer b.Release()

	if caller != nil {
		// Message with caller info - estimate size conservatively
		estimatedSize := len(prefix) + len(msg) +
			len(tagFunc) + len(caller.funcName) +
			len(tagFile) + len(caller.file) +
			len(tagLine) + len(caller.lineStr) +
			len(tagEnd) + 10 // small buffer

		// Only grow if current capacity is insufficient
		if b.Cap() < estimatedSize {
			b.Grow(estimatedSize - b.Len())
		}

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
		estimatedSize := len(prefix) + len(msg) + 1

		if b.Cap() < estimatedSize {
			b.Grow(estimatedSize - b.Len())
		}

		b.WriteString(prefix)
		b.WriteString(msg)
		b.WriteByte('\n')
	}
	return b.String()
}

// logToFile logs to the file logger based on level
func logToFile(level logType, msg string, caller *callerInfo) {
	if !Loggable {
		return
	}

	cleanMsg := regex.AnsiEscapeCompile().ReplaceAllString(msg, "")

	if caller != nil {
		// Log with caller info
		event := getZerologEvent(level).
			Str(jFunction, caller.funcName).
			Str(jFile, caller.file).
			Int(jLine, caller.line)
		event.Msg(cleanMsg)
	} else {
		// Log without caller info
		getZerologEvent(level).Msg(cleanMsg)
	}
}

// getZerologEvent returns the appropriate zerolog event for the level
func getZerologEvent(level logType) *zerolog.Event {
	switch level {
	case logError:
		return fileLogger.Error()
	case logWarn:
		return fileLogger.Warn()
	case logDebug:
		return fileLogger.Debug()
	default:
		return fileLogger.Info()
	}
}

// log is the core logging function that handles all logging operations
func log(level logType, prefix, msg string, withCaller bool, args ...any) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	var caller *callerInfo
	if withCaller {
		c := getCaller(3) // Skip: getCaller, log, and the public function (logging.E / logging.D)
		caller = &c
	}

	logMsg := buildLogMessage(prefix, msg, caller)
	writeToConsole(logMsg)
	logToFile(level, msg, caller)
}

// E logs error messages
func E(msg string, args ...any) {
	log(logError, consts.RedError, msg, true, args...)
}

// S logs success messages
func S(msg string, args ...any) {
	log(logSuccess, consts.GreenSuccess, msg, false, args...)
}

// D logs debug messages
func D(l int, msg string, args ...any) {
	if Level < l {
		return
	}
	log(logDebug, consts.YellowDebug, msg, true, args...)
}

// W logs warning messages
func W(msg string, args ...any) {
	log(logWarn, consts.YellowWarning, msg, false, args...)
}

// I logs info messages
func I(msg string, args ...any) {
	log(logInfo, consts.BlueInfo, msg, false, args...)
}

// P logs plain messages
func P(msg string, args ...any) {
	log(logPrint, "", msg, false, args...)
}
