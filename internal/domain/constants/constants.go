package domain

// File prefix and suffix
const (
	OldTag  = "_metarrbackup"
	TempTag = "tmp_"
)

// Webpage tags
var WebDateTags = []string{"release-date", "upload-date", "date", "date-text", "text-date"}
var WebDescriptionTags = []string{"description", "longdescription", "long-description", "summary", "synopsis", "check-for-urls"}
var WebCreditsTags = []string{"creator", "uploader", "uploaded-by", "uploaded_by"}
var WebTitleTags = []string{"video-title", "video-name"}
