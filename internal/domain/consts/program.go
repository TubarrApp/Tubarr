package consts

import "time"

// BotActivitySentinel is a sentinel included by the dev in errors for functions to find when
// an error has determined that a domain has blocked crawling due to detecting bot activity.
const BotActivitySentinel = "detected bot activity"

// Bot avoidance delays.
const (
	DefaultStartupStagger    = 30
	DefaultBotAvoidanceDelay = 15
)

// Intervals.
const (
	Interval100ms = 100 * time.Millisecond
)

// Program messages.
const (
	TimeRemainingMsg = ColorCyan + "Time remaining:" + ColorReset
)
