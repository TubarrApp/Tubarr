package logging

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"

	"github.com/spf13/viper"
)

var (
	Level int = -1 // Pre initialization
	mu    sync.Mutex
)

func E(l int, format string, args ...interface{}) string {

	mu.Lock()
	defer mu.Unlock()
	var msg string

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())
	tag := fmt.Sprintf("["+consts.ColorBlue+"Function:"+consts.ColorReset+" %s - "+consts.ColorBlue+"File:"+consts.ColorReset+" %s : "+consts.ColorBlue+"Line:"+consts.ColorReset+" %d] ", funcName, file, line)

	if Level < 0 {
		Level = viper.GetInt(keys.DebugLevel)
	}
	if l <= viper.GetInt(keys.DebugLevel) {

		if len(args) != 0 && args != nil {
			msg = fmt.Sprintf(consts.RedError+format+" "+tag+"\n", args...)
		} else {
			msg = fmt.Sprintf(consts.RedError + format + " " + tag + "\n")
		}
		fmt.Print(msg)
		writeLog(msg, l)
	}

	return msg
}

func S(l int, format string, args ...interface{}) string {

	mu.Lock()
	defer mu.Unlock()
	var msg string

	if Level < 0 {
		Level = viper.GetInt(keys.DebugLevel)
	}
	if l <= viper.GetInt(keys.DebugLevel) {

		if len(args) != 0 && args != nil {
			msg = fmt.Sprintf(consts.GreenSuccess+format+" \n", args...)
		} else {
			msg = fmt.Sprintf(consts.GreenSuccess + format + " \n")
		}
		fmt.Print(msg)
		writeLog(msg, l)
	}
	return msg
}

func D(l int, format string, args ...interface{}) string {

	mu.Lock()
	defer mu.Unlock()
	var msg string

	pc, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	funcName := filepath.Base(runtime.FuncForPC(pc).Name())
	tag := fmt.Sprintf("["+consts.ColorBlue+"Function:"+consts.ColorReset+" %s - "+consts.ColorBlue+"File:"+consts.ColorReset+" %s : "+consts.ColorBlue+"Line:"+consts.ColorReset+" %d] ", funcName, file, line)

	if Level < 0 {
		Level = viper.GetInt(keys.DebugLevel)
	}
	if l <= viper.GetInt(keys.DebugLevel) && l != 0 { // Debug messages don't appear by default

		if len(args) != 0 && args != nil {
			msg = fmt.Sprintf(consts.YellowDebug+format+" "+tag+"\n", args...)
		} else {
			msg = fmt.Sprintf(consts.YellowDebug + format + " " + tag + "\n")
		}
		fmt.Print(msg)
		writeLog(msg, l)
	}
	return msg
}

func I(format string, args ...interface{}) string {

	mu.Lock()
	defer mu.Unlock()
	var msg string

	if len(args) != 0 && args != nil {
		msg = fmt.Sprintf(consts.BlueInfo+format+"\n", args...)
	} else {
		msg = fmt.Sprintf(consts.BlueInfo + format + "\n")
	}
	fmt.Print(msg)
	writeLog(msg, 0)

	return msg
}

func P(format string, args ...interface{}) string {

	mu.Lock()
	defer mu.Unlock()
	var msg string

	if len(args) != 0 && args != nil {
		msg = fmt.Sprintf(format+"\n", args...)
	} else {
		msg = fmt.Sprintf(format + "\n")
	}
	fmt.Print(msg)
	writeLog(msg, 0)

	return msg
}
