// Package templates holds constants for templating elements.
package templates

const (
	ChannelID   = "channel_id"
	ChannelName = "channel_name"
	ChannelURL  = "channel_url"
)

const (
	VideoID    = "video_id"
	VideoTitle = "video_title"
	VideoURL   = "video_url"
)

const (
	MetDay   = "day"
	MetMonth = "month"
	MetYear  = "year"
)

const (
	MetAuthor   = "author"
	MetDirector = "director"
)

const (
	MetDomain = "domain"
)

var TemplateMap = map[string]bool{
	ChannelID: true, ChannelName: true, ChannelURL: true,
	VideoID: true, VideoTitle: true, VideoURL: true,
	MetDay: true, MetMonth: true, MetYear: true,
	MetAuthor: true, MetDirector: true,
	MetDomain: true,
}
