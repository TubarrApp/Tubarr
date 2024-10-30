package domain

import "fmt"

// Colors
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[91m"
	ColorGreen  = "\033[92m"
	ColorYellow = "\033[93m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[96m"
	ColorWhite  = "\033[37m"
)

var RedError string = fmt.Sprintf("%v[ERROR] %v", ColorRed, ColorReset)
var YellowDebug string = fmt.Sprintf("%v[DEBUG] %v", ColorYellow, ColorReset)
var GreenSuccess string = fmt.Sprintf("%v[SUCCESS] %v", ColorGreen, ColorReset)
var BlueInfo string = fmt.Sprintf("%v[Info] %v", ColorCyan, ColorReset)
