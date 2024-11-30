// Package logging handles the printing and writing of debug and log messages.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	mu         sync.Mutex
	ErrorArray = make([]error, 0, 8)
	ansiEscape = regex.AnsiEscapeCompile()
	console    = os.Stdout
)

const (
	timeFormat         = "01/02 15:04:05"
	tubarrLogFile      = "tubarr.log"
	funcFileLineSingle = "%s%s [%sFunction:%s %s - %sFile:%s %s : %sLine:%s %d]\n"
	funcFileLineMulti  = "%s%s[%sFunction:%s %s - %sFile:%s %s : %sLine:%s %d]\n\n"
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
	if ansiEscape != nil {
		fileLogger.Info().Msg(ansiEscape.ReplaceAllString(startMsg, ""))
	}

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
			mu.Lock()
			ErrorArray = append(ErrorArray, err)
			mu.Unlock()
		}
	}

	var funcFileLine string
	msg := fmt.Sprintf(format, args...)
	if strings.HasSuffix(msg, "\n") {
		funcFileLine = funcFileLineMulti
	} else {
		funcFileLine = funcFileLineSingle
	}

	consoleMsg := fmt.Sprintf(funcFileLine,
		consts.RedError,
		msg,
		consts.ColorDimCyan,
		consts.ColorReset,
		funcName,
		consts.ColorDimWhite,
		consts.ColorReset,
		file,
		consts.ColorDimWhite,
		consts.ColorReset,
		line,
	)

	// Console output with colors
	writeToConsole(consoleMsg)

	// File output with JSON logging and no colors
	if Loggable {
		fileLogger.Error().
			Str(JFunction, funcName).
			Str(JFile, file).
			Int(JLine, line).
			Msg(ansiEscape.ReplaceAllString(msg, ""))
	}
}

// S logs success messages.
func S(l int, format string, args ...interface{}) {
	if Level < l {
		return
	}

	msg := fmt.Sprintf(format, args...)
	consoleMsg := fmt.Sprintf("%s%s\n", consts.GreenSuccess, msg)

	writeToConsole(consoleMsg)
	if Loggable {
		fileLogger.Info().Msg(ansiEscape.ReplaceAllString(msg, ""))
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

	consoleMsg := fmt.Sprintf(funcFileLine,
		consts.YellowDebug,
		msg,
		consts.ColorDimCyan,
		consts.ColorReset,
		funcName,
		consts.ColorDimCyan,
		consts.ColorReset,
		file,
		consts.ColorDimCyan,
		consts.ColorReset,
		line,
	)

	writeToConsole(consoleMsg)
	if Loggable {
		fileLogger.Debug().
			Str(JFunction, funcName).
			Str(JFile, file).
			Int(JLine, line).
			Msg(ansiEscape.ReplaceAllString(msg, ""))
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

	consoleMsg := fmt.Sprintf(funcFileLine,
		consts.YellowWarning,
		msg,
		consts.ColorDimCyan,
		consts.ColorReset,
		funcName,
		consts.ColorDimCyan,
		consts.ColorReset,
		file,
		consts.ColorDimCyan,
		consts.ColorReset,
		line,
	)

	writeToConsole(consoleMsg)
	if Loggable {
		fileLogger.Warn().
			Str(JFunction, funcName).
			Str(JFile, file).
			Int(JLine, line).
			Msg(ansiEscape.ReplaceAllString(msg, ""))
	}
}

// I logs info messages.
func I(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	consoleMsg := fmt.Sprintf("%s%s\n", consts.BlueInfo, msg)

	writeToConsole(consoleMsg)
	if Loggable {
		fileLogger.Info().Msg(ansiEscape.ReplaceAllString(msg, ""))
	}
}

// ICarriage logs info messages in carriage return format.
func ICarriage(format string, args ...interface{}) {

	msg := fmt.Sprintf(format, args...)
	consoleMsg := fmt.Sprintf("\r%s%s\r", consts.BlueInfo, msg)

	writeToConsole(consoleMsg)
	if Loggable {
		fileLogger.Info().Msg(ansiEscape.ReplaceAllString(msg, ""))
	}
}

// P logs plain messages.
func P(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	consoleMsg := fmt.Sprintf("%s\n", msg)

	writeToConsole(consoleMsg)
	if Loggable {
		fileLogger.Info().Msg(msg)
	}
}
