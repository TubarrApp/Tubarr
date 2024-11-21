package logging

import (
	"log"
	"strings"
	"time"
	"tubarr/internal/domain/regex"
	"tubarr/internal/domain/setup"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	ErrorArray []error
	Loggable   bool = false
	Logger     *log.Logger

	// Matches ANSI escape codes
	ansiEscape = regex.AnsiEscapeCompile()
)

// SetupLogging creates and/or opens the log file
func SetupLogging() error {

	logFile := &lumberjack.Logger{
		Filename:   setup.LogFilePath, // Log file path
		MaxSize:    1,                 // Max size in MB before rotation
		MaxBackups: 3,                 // Number of backups to retain
		Compress:   true,              // Gzip compression
	}

	// Assign lumberjack logger to standard log output
	Logger = log.New(logFile, "", log.LstdFlags)
	Loggable = true

	Logger.Printf(":\n=========== %v ===========\n\n", time.Now().Format(time.RFC1123Z))
	return nil
}

// Write writes error information to the log file
func writeLog(msg string, level int) {
	// Do not add mutex
	if Loggable && level < 2 {
		if !strings.HasPrefix(msg, "\n") {
			msg += "\n"
		}

		if ansiEscape == nil {
			ansiEscape = regex.AnsiEscapeCompile()
		}

		Logger.Print(ansiEscape.ReplaceAllString(msg, ""))
	}
}
