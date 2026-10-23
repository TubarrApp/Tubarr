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

// MaxCrawlDLRetries is how many crawls retry a failed video on auto-download URLs before it's left for the user to retry.
const MaxCrawlDLRetries = 3

// Intervals.
const (
	Interval100ms = 100 * time.Millisecond
)

// Program messages.
const (
	TimeRemainingMsg = sharedconsts.ColorCyan + "Time remaining:" + sharedconsts.ColorReset
)

// YtdlpCodecRegex contains translations for codecs into YT-DLP regex strings.
var YtdlpCodecRegex = map[string]string{
	// Video.
	sharedconsts.VCodecAV1:   "^av0?1",
	sharedconsts.VCodecH264:  "^([hx]264|avc)",
	sharedconsts.VCodecHEVC:  "^([hx]265|he?vc?)",
	sharedconsts.VCodecMPEG2: "^(mpeg2|mp2v)",
	sharedconsts.VCodecVP8:   "^vp0?8",
	sharedconsts.VCodecVP9:   "^vp0?9",
	// Audio.
	sharedconsts.ACodecAAC:    "^(aac|mp4a)",
	sharedconsts.ACodecAC3:    "^ac-?3",
	sharedconsts.ACodecALAC:   "^alac",
	sharedconsts.ACodecDTS:    "^dts",
	sharedconsts.ACodecEAC3:   "^e-?a?c-?3",
	sharedconsts.ACodecFLAC:   "^flac",
	sharedconsts.ACodecMP2:    "^mp2",
	sharedconsts.ACodecMP3:    "^mp3",
	sharedconsts.ACodecOpus:   "^opus",
	sharedconsts.ACodecPCM:    "^pcm",
	sharedconsts.ACodecTrueHD: "^truehd",
	sharedconsts.ACodecVorbis: "^vorbis",
	sharedconsts.ACodecWAV:    "^(wav|pcm)",
}
