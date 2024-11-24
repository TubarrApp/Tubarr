package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/regex"

	"github.com/rs/zerolog"
)

var (
	Level      int  = -1
	Loggable   bool = false
	logger     zerolog.Logger
	ErrorArray []error
	mu         sync.Mutex
	ansiEscape = regex.AnsiEscapeCompile()
)

const (
	metarrLogFile = "metarr.log"
)

func init() {
	zerolog.TimeFieldFormat = time.RFC3339
	logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
}

// SetupLogging creates and/or opens the log file
func SetupLogging(targetDir string) error {
	logfile, err := os.OpenFile(
		filepath.Join(targetDir, metarrLogFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}

	// Console writer with colors
	console := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "01/02 15:04:05",
		NoColor:    false,
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.MessageFieldName,
		},
	}

	// File writer without colors and level prefixes
	fileWriter := zerolog.ConsoleWriter{
		Out:        logfile,
		NoColor:    true,
		TimeFormat: "01/02 15:04:05",
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.MessageFieldName,
		},
	}

	// Remove the level prefixes
	console.FormatLevel = func(i interface{}) string {
		return ""
	}
	fileWriter.FormatLevel = func(i interface{}) string {
		return ""
	}

	// Strip ANSI codes from file output
	fileWriter.FormatMessage = func(i interface{}) string {
		if ansiEscape != nil {
			return ansiEscape.ReplaceAllString(fmt.Sprintf("%v", i), "")
		}
		return fmt.Sprintf("%v", i)
	}

	multi := zerolog.MultiLevelWriter(console, fileWriter)
	logger = zerolog.New(multi).With().Timestamp().Logger()

	Loggable = true
	logger.Info().Msgf("=========== %v ===========", time.Now().Format(time.RFC1123Z))

	return nil
}

// E logs error messages.
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

	msg := fmt.Sprintf(format, args...)
	logger.Error().Msg(fmt.Sprintf("%s%s[Function: %s - File: %s : Line: %d]",
		consts.RedError,
		msg,
		funcName,
		file,
		line,
	))
}

// S logs success messages.
func S(l int, format string, args ...interface{}) {
	if Level < l {
		return
	}
	msg := fmt.Sprintf(format, args...)
	logger.Info().Msg(fmt.Sprintf("%s%s", consts.GreenSuccess, msg))
}

// D logs debug messages.
func D(l int, format string, args ...interface{}) {
	if Level < l {
		return
	}

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())

	msg := fmt.Sprintf(format, args...)
	logger.Debug().Msg(fmt.Sprintf("%s%s[Function: %s - File: %s : Line: %d]",
		consts.YellowDebug,
		msg,
		funcName,
		file,
		line,
	))
}

// I logs info messages.
func I(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.Info().Msg(fmt.Sprintf("%s%s", consts.BlueInfo, msg))
}

// P logs plain messages.
func P(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.Info().Msg(msg)
}
