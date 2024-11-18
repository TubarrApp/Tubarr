package consts

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

const (
	RedError     string = ColorRed + "[ERROR] " + ColorReset
	GreenSuccess string = ColorGreen + "[SUCCESS] " + ColorReset
	YellowDebug  string = ColorYellow + "[DEBUG] " + ColorReset
	BlueInfo     string = ColorCyan + "[Info] " + ColorReset
)
