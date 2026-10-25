package models

// Notification holds notification data for channels.
type Notification struct {
	ChannelID  int64  `json:"-"`
	ChannelURL string `json:"channel_url"` // Blank for every channel URL.
	NotifyURL  string `json:"notify_url"`
}
