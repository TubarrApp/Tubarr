// Package errconsts holds constant error messages
package errconsts

// Programs
const (
	YTDLPFailure = "yt-dlp command failed: %w"
)

// File
const (
	ConfigFileUpdateFail = "failed to update from config file %q: %v"
)

// Format errors
const (
	FilterOpsFormatError = "please enter filters in the format 'field:filter_type:value:must_or_any'.\n\ntitle:omits:frogs:must' ignores all videos with frogs in the metatitle.\n\n'title:contains:cat:any','title:contains:dog:any' only includes videos with EITHER cat and dog in the title (use 'must' to require both).\n\n'date:omits:must' omits videos only when the metafile contains a date field"
)
