// Package consts holds various global, unchanging values.
package consts

// File prefix and suffix
const (
	OldTag  = "_metarrbackup"
	TempTag = "tmp_"
	IDTag   = "id_"
)

// Webpage tags
var (
	WebDateTags        = []string{"release-date", "upload-date", "date", "date-text", "text-date"}
	WebDescriptionTags = []string{"description", "longdescription", "long-description", "summary", "synopsis", "check-for-urls"}
	WebCreditsTags     = []string{"creator", "uploader", "uploaded-by", "uploaded_by"}
	WebTitleTags       = []string{"video-title", "video-name"}
)

// AllVidExtensions is a list of video file extensions.
var AllVidExtensions = []string{".3gp", ".avi", ".f4v", ".flv", ".m4v", ".mkv",
	".mov", ".mp4", ".mpeg", ".mpg", ".ogm", ".ogv",
	".ts", ".vob", ".webm", ".wmv"}

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

	"youtube.com":     2880,
	"youtu.be":        2880,
	"twitch.tv":       1440,
	"twitter.com":     720,
	"x.com":           720,
	"reddit.com":      360,
	"vimeo.com":       480,
	"dailymotion.com": 360,
	"tiktok.com":      720,
	"instagram.com":   1440,
	"facebook.com":    1440,
	"soundcloud.com":  480,
	"bandcamp.com":    240,
	"streamable.com":  180,
	"imgur.com":       180,
}
