package consts

import (
	"time"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// BotActivitySentinel is a sentinel included by the dev in errors for functions to find when
// an error has determined that a domain has blocked crawling due to detecting bot activity.
const BotActivitySentinel = "detected bot activity"

// Bot avoidance delays.
const (
	DefaultStartupStaggerMinutes = 15
	DefaultBotAvoidanceSeconds   = 10
)

// Intervals.
const (
	Interval100ms = 100 * time.Millisecond
)

// Program messages.
const (
	TimeRemainingMsg = sharedconsts.ColorCyan + "Time remaining:" + sharedconsts.ColorReset
)
