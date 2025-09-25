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
	WebDateTags        = [...]string{"release-date", "upload-date", "date", "date-text", "text-date"}
	WebDescriptionTags = [...]string{"description", "longdescription", "long-description", "summary", "synopsis", "check-for-urls"}
	WebCreditsTags     = [...]string{"creator", "uploader", "uploaded-by", "uploaded_by"}
	WebTitleTags       = [...]string{"video-title", "video-name"}
)

// Video files
var (
	AllVidExtensions = [...]string{".3gp", ".avi", ".f4v", ".flv", ".m4v", ".mkv",
		".mov", ".mp4", ".mpeg", ".mpg", ".ogm", ".ogv",
		".ts", ".vob", ".webm", ".wmv"}
)

// Op types
const (
	FilterContains = "contains"
	FilterOmits    = "omits"
)
