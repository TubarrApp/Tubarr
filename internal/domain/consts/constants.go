// Package consts holds various global, unchanging values.
package consts

// File prefix and suffix
const (
	OldTag  = "_metarrbackup"
	TempTag = "tmp_"
	IDTag   = "id_"
)

// AllVidExtensions is a list of video file extensions.
var AllVidExtensions = map[string]bool{
	".3gp":  true,
	".avi":  true,
	".f4v":  true,
	".flv":  true,
	".m4v":  true,
	".mkv":  true,
	".mov":  true,
	".mp4":  true,
	".mpeg": true,
	".mpg":  true,
	".ogm":  true,
	".ogv":  true,
	".ts":   true,
	".vob":  true,
	".webm": true,
	".wmv":  true,
}

// Op types
const (
	FilterContains = "contains"
	FilterOmits    = "omits"
)

// BotTimeoutMap holds the cooldown times in minutes for popular domains (used if a domain blocks Tubarr).
var BotTimeoutMap = map[string]float64{

	// 2880: 48 hours
	// 1440: 24 hours
	// 720: 12 hours
	// 360: 6 hours
	// 240: 4 hours
	// 180: 3 hours

	"utube.com":  2880,
	"u.be":       2880,
	"itch.tv":    1440,
	"itter.com":  720,
	"x.com":      720,
	"dit.com":    360,
	"imeo.com":   480,
	"motion.com": 360,
	"tok.com":    720,
	"gram.com":   1440,
	"book.com":   1440,
	"cloud.com":  480,
	"camp.com":   240,
	"mable.com":  180,
	"gur.com":    180,
}
