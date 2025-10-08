// Package templates holds constants for templating elements.
package templates

// Template tags for channel elements.
const (
	ChannelID   = "channel_id"
	ChannelName = "channel_name"
	ChannelURL  = "channel_url"
)

// Template tags for individual video elements.
const (
	VideoID    = "video_id"
	VideoTitle = "video_title"
	VideoURL   = "video_url"
)

// Template tags for date metadata.
const (
	MetDay   = "day"
	MetMonth = "month"
	MetYear  = "year"
)

// Template tags for cast metadata.
const (
	MetAuthor   = "author"
	MetDirector = "director"
)

// Template tags for URL metadata.
const (
	MetDomain = "domain"
)

// TemplateMap contains the different template tags available.
//
// E.g. ChannelName 'true' as '{{channel_name}}' is replaced with a Channel.Name variable.
var TemplateMap = map[string]bool{
	ChannelID: true, ChannelName: true, ChannelURL: true,
	VideoID: true, VideoTitle: true, VideoURL: true,
	MetDay: true, MetMonth: true, MetYear: true,
	MetAuthor: true, MetDirector: true,
	MetDomain: true,
}
