package utils

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"sync"
	"time"
)

var ErrorArray []error
var Loggable bool = false
var Logger *log.Logger
var mu sync.Mutex

// Regular expression to match ANSI escape codes
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// SetupLogging creates and/or opens the log file
func SetupLogging(targetDir string, logFile *os.File) error {

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

		var errMsg string
		var info string

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

		var errMsg, info string

		if len(err) != 0 && err != nil {

			var errOut string

			for _, errValue := range err {
				errOut += errValue.Error()
			}

			if tag == "" {
				errMsg = fmt.Sprintf(errOut+"\n", args...)
			} else {
				errMsg = fmt.Sprintf(tag+errOut+"\n", args...)
			}
			Logger.Print(stripAnsiCodes(errMsg))

		} else if len(infoMsg) != 0 && infoMsg != nil {

			var infoOut string

			for _, infoValue := range err {
				infoOut += infoValue.Error()
			}

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
