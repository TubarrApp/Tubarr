package consts

// Metadata JSON keys
const (
	MetadataTitle    = "title"
	MetadataDesc     = "description"
	MetadataDate     = "release_date"
	MetadataVideoURL = "direct_video_url"
)

// HTML identifier strings
const (
	HTMLCensoredTitle    = "#episode-container .episode-title"
	HTMLCensoredDesc     = "#about .raised-content"
	HTMLCensoredDate     = "#about time"
	HTMLCensoredVideoURL = "a.dropdown-item[href$='.mp4']"
)
