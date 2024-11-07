package utils

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	Loggable   bool = false
	Logger     *log.Logger
	ErrorArray []error
	mu         sync.Mutex

	// Regular expression to match ANSI escape codes
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

// SetupLogging creates and/or opens the log file
func SetupLogging(targetDir string) error {

	logFile := &lumberjack.Logger{
		Filename:   filepath.Join(targetDir, "/tubarr.log"), // Log file path
		MaxSize:    1,                                       // Max size in MB before rotation
		MaxBackups: 3,                                       // Number of backups to retain
		Compress:   true,                                    // Gzip compression
	}

	// Assign lumberjack logger to standard log output
	Logger = log.New(logFile, "", log.LstdFlags)
	Loggable = true

	Logger.Printf(":\n=========== %v ===========\n\n", time.Now().Format(time.RFC1123Z))
	return nil
}

// Write writes error information to the log file
func Write(tag, infoMsg string, err error, args ...interface{}) {

	if Loggable {
		mu.Lock()
		defer mu.Unlock()

		var (
			errMsg,
			info string
		)

		if err != nil {
			if tag == "" {
				errMsg = fmt.Sprintf(err.Error()+"\n", args...)
			} else {
				errMsg = fmt.Sprintf(tag+err.Error()+"\n", args...)
			}
			Logger.Print(stripAnsiCodes(errMsg))

		} else if infoMsg != "" {
			if tag == "" {
				info = fmt.Sprintf(infoMsg+"\n", args...)
			} else {
				info = fmt.Sprintf(tag+infoMsg+"\n", args...)
			}
			Logger.Print(stripAnsiCodes(info))
		}
	}
}

// WriteArray writes an array of error information to the log file
func WriteArray(tag string, infoMsg []string, err []error, args ...interface{}) {
	if Loggable {
		mu.Lock()
		defer mu.Unlock()

		var (
			errMsg,
			info string
		)

		var b strings.Builder

		if len(err) != 0 && err != nil {
			b.Grow(len(err) * 50)
			defer b.Reset()

			var errOut string
			for i, errValue := range err {

				b.WriteString(errValue.Error())
				if i != len(err)-2 {
					b.WriteString("; ")
				}
			}
			errOut = b.String()

			if tag == "" {
				errMsg = fmt.Sprintf(errOut+"\n", args...)
			} else {
				errMsg = fmt.Sprintf(tag+errOut+"\n", args...)
			}
			Logger.Print(stripAnsiCodes(errMsg))

			return
		}

		if len(infoMsg) != 0 && infoMsg != nil {
			b.Grow(len(infoMsg) * 50)
			defer b.Reset()

			var infoOut string
			for i, infoValue := range infoMsg {

				b.WriteString(infoValue)
				if i != len(infoMsg)-2 {
					b.WriteString("; ")
				}
			}
			infoOut = b.String()

			if tag == "" {
				info = fmt.Sprintf(infoOut+"\n", args...)
			} else {
				info = fmt.Sprintf(tag+infoOut+"\n", args...)
			}
			Logger.Print(stripAnsiCodes(info))
		}
	}
}

// stripAnsiCodes removes ANSI escape codes from a string
func stripAnsiCodes(input string) string {
	return ansiEscape.ReplaceAllString(input, "")
}
