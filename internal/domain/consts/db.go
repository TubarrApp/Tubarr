package consts

// Database table names.
const (
	DBProgram       = "program"
	DBChannels      = "channels"
	DBChannelURLs   = "channel_urls"
	DBVideos        = "videos"
	DBDownloads     = "downloads"
	DBNotifications = "notifications"
)

// Program database entry keys.
const (
	QProgHost      = "host"
	QProgID        = "id"
	QProgHeartbeat = "last_heartbeat"
	QProgPID       = "pid"
	QProgStartedAt = "started_at"
	QProgRunning   = "running"
)

// ValidProgramKeys holds valid keys for the program database.
var ValidProgramKeys = map[string]bool{
	QProgHost:      true,
	QProgID:        true,
	QProgHeartbeat: true,
	QProgPID:       true,
	QProgStartedAt: true,
	QProgRunning:   true,
}

// Channel database entry keys.
const (
	QChanID                   = "id"
	QChanName                 = "name"
	QChanConfigFile           = "config_file"
	QChanSettings             = "settings"
	QChanMetarr               = "metarr"
	QChanLastScan             = "last_scan"
	QChanCreatedAt            = "created_at"
	QChanUpdatedAt            = "updated_at"
	QChanNewVideoNotification = "new_video_notification"
)

// ValidChannelKeys holds valid keys for the channel database.
var ValidChannelKeys = map[string]bool{
	QChanID:                   true,
	QChanName:                 true,
	QChanConfigFile:           true,
	QChanSettings:             true,
	QChanMetarr:               true,
	QChanLastScan:             true,
	QChanCreatedAt:            true,
	QChanUpdatedAt:            true,
	QChanNewVideoNotification: true,
}

// Channel URL database entry keys.
const (
	QChanURLID        = "id"
	QChanURLChannelID = "channel_id"
	QChanURLURL       = "url"
	QChanURLUsername  = "username"
	QChanURLPassword  = "password"
	QChanURLLoginURL  = "login_url"
	QChanURLIsManual  = "is_manual"
	QChanURLSettings  = "settings"
	QChanURLMetarr    = "metarr"
	QChanURLLastScan  = "last_scan"
	QChanURLCreatedAt = "created_at"
	QChanURLUpdatedAt = "updated_at"
)

// ValidChannelURLKeys holds valid keys for the channel URL database.
var ValidChannelURLKeys = map[string]bool{
	QChanURLID:        true,
	QChanURLChannelID: true,
	QChanURLURL:       true,
	QChanURLUsername:  true,
	QChanURLPassword:  true,
	QChanURLLoginURL:  true,
	QChanURLIsManual:  true,
	QChanURLSettings:  true,
	QChanURLMetarr:    true,
	QChanURLLastScan:  true,
	QChanURLCreatedAt: true,
	QChanURLUpdatedAt: true,
}

// Video database entry keys.
const (
	QVidID           = "id"
	QVidChanID       = "channel_id"
	QVidChanURLID    = "channel_url_id"
	QVidFinished     = "finished"
	QVidIgnored      = "ignored"
	QVidThumbnailURL = "thumbnail_url"
	QVidURL          = "url"
	QVidTitle        = "title"
	QVidDescription  = "description"
	QVidVideoPath    = "video_path"
	QVidJSONPath     = "json_path"
	QVidUploadDate   = "upload_date"
	QVidMetadata     = "metadata"
	QVidDLStatus     = "download_status"
	QVidDLPercentage = "download_pct"
	QVidCreatedAt    = "created_at"
	QVidUpdatedAt    = "updated_at"
)

// ValidVideoKeys holds valid keys for the video database.
var ValidVideoKeys = map[string]bool{
	QVidID:           true,
	QVidChanID:       true,
	QVidChanURLID:    true,
	QVidFinished:     true,
	QVidIgnored:      true,
	QVidThumbnailURL: true,
	QVidURL:          true,
	QVidTitle:        true,
	QVidDescription:  true,
	QVidVideoPath:    true,
	QVidJSONPath:     true,
	QVidUploadDate:   true,
	QVidMetadata:     true,
	QVidDLStatus:     true,
	QVidDLPercentage: true,
	QVidCreatedAt:    true,
	QVidUpdatedAt:    true,
}

// Downloads database entry keys.
const (
	QDLVidID     = "video_id"
	QDLStatus    = "status"
	QDLPct       = "percentage"
	QDLCreatedAt = "created_at"
	QDLUpdatedAt = "updated_at"
)

// ValidDownloadKeys holds valid keys for the download database.
var ValidDownloadKeys = map[string]bool{
	QDLVidID:     true,
	QDLStatus:    true,
	QDLPct:       true,
	QDLCreatedAt: true,
	QDLUpdatedAt: true,
}

// Notification database entry keys.
const (
	QNotifyChanID    = "channel_id"
	QNotifyName      = "name"
	QNotifyURL       = "notify_url"
	QNotifyChanURL   = "channel_url"
	QNotifyCreatedAt = "created_at"
	QNotifyUpdatedAt = "updated_at"
)

// ValidNotificationKeys holds valid keys for the notification database.
var ValidNotificationKeys = map[string]bool{
	QNotifyChanID:    true,
	QNotifyName:      true,
	QNotifyURL:       true,
	QNotifyChanURL:   true,
	QNotifyCreatedAt: true,
	QNotifyUpdatedAt: true,
}

// Misc
const (
	ManualDownloadsCol = "manual-downloads"
)

// DownloadStatus holds constant download status strings.
type DownloadStatus string

// Download status strings.
const (
	DLStatusQueued      DownloadStatus = "Queued"
	DLStatusDownloading DownloadStatus = "Downloading"
	DLStatusCompleted   DownloadStatus = "Finished"
	DLStatusFailed      DownloadStatus = "Failed"
	DLStatusIgnored     DownloadStatus = "Ignored"
	DLStatusCancelled   DownloadStatus = "Cancelled"
)
