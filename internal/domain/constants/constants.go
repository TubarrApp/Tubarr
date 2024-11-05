package domain

// File prefix and suffix
const (
	OldTag  = "_metarrbackup"
	TempTag = "tmp_"
)

// Webpage tags
var (
	WebDateTags        = []string{"release-date", "upload-date", "date", "date-text", "text-date"}
	WebDescriptionTags = []string{"description", "longdescription", "long-description", "summary", "synopsis", "check-for-urls"}
	WebCreditsTags     = []string{"creator", "uploader", "uploaded-by", "uploaded_by"}
	WebTitleTags       = []string{"video-title", "video-name"}
)

// Video files
var (
	AllVidExtensions = []string{".3gp", ".avi", ".f4v", ".flv", ".m4v", ".mkv",
		".mov", ".mp4", ".mpeg", ".mpg", ".ogm", ".ogv",
		".ts", ".vob", ".webm", ".wmv"}
)
