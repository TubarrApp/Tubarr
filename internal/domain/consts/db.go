package consts

// Database table names.
const (
	DBProgram        = "program"
	DBChannels       = "channels"
	DBChannelURLs    = "channel_urls"
	DBVideos         = "videos"
	DBDownloads      = "downloads"
	DBNotifications  = "notifications"
	DBBlockedDomains = "blocked_domains"
	DBSettings       = "settings"
)

// Settings database entry keys.
const (
	QSettingsKey = "key"
	QSettingsVal = "value"
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
var ValidProgramKeys = map[string]struct{}{
	QProgHost:      {},
	QProgID:        {},
	QProgHeartbeat: {},
	QProgPID:       {},
	QProgStartedAt: {},
	QProgRunning:   {},
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
	QChanNewVideoURLs         = "new_video_urls"
)

// ValidChannelKeys holds valid keys for the channel database.
var ValidChannelKeys = map[string]struct{}{
	QChanID:                   {},
	QChanName:                 {},
	QChanConfigFile:           {},
	QChanSettings:             {},
	QChanMetarr:               {},
	QChanLastScan:             {},
	QChanCreatedAt:            {},
	QChanUpdatedAt:            {},
	QChanNewVideoNotification: {},
	QChanNewVideoURLs:         {},
}

// ChannelTextColumns holds channel columns which users may set to plain text (the rest are JSON or managed by Tubarr).
var ChannelTextColumns = map[string]struct{}{
	QChanName:       {},
	QChanConfigFile: {},
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
var ValidChannelURLKeys = map[string]struct{}{
	QChanURLID:        {},
	QChanURLChannelID: {},
	QChanURLURL:       {},
	QChanURLUsername:  {},
	QChanURLPassword:  {},
	QChanURLLoginURL:  {},
	QChanURLIsManual:  {},
	QChanURLSettings:  {},
	QChanURLMetarr:    {},
	QChanURLLastScan:  {},
	QChanURLCreatedAt: {},
	QChanURLUpdatedAt: {},
}

// Video database entry keys.
const (
	QVidID           = "id"
	QVidChanID       = "channel_id"
	QVidChanURLID    = "channel_url_id"
	QVidChanURL      = "channel_url"
	QVidFinished     = "finished"
	QVidIgnored      = "ignored"
	QVidThumbnailURL = "thumbnail_url"
	QVidURL          = "url"
	QVidTitle        = "title"
	QVidDescription  = "description"
	QVidVideoPath    = "video_path"
	QVidJSONPath     = "json_path"
	QVidUploadDate   = "upload_date"
	QVidDuration     = "duration"
	QVidMetadata     = "metadata"
	QVidDLStatus     = "download_status"
	QVidDLPercentage = "download_pct"
	QVidMetarrStatus = "metarr_status"
	QVidMetarrError  = "metarr_error"
	QVidOverrides    = "overrides"
	QVidCreatedAt    = "created_at"
	QVidUpdatedAt    = "updated_at"
	QVidChanName     = "channel_name"
)

// ValidVideoKeys holds valid keys for the video database.
var ValidVideoKeys = map[string]struct{}{
	QVidID:           {},
	QVidChanID:       {},
	QVidChanURLID:    {},
	QVidChanURL:      {},
	QVidFinished:     {},
	QVidIgnored:      {},
	QVidThumbnailURL: {},
	QVidURL:          {},
	QVidTitle:        {},
	QVidDescription:  {},
	QVidVideoPath:    {},
	QVidJSONPath:     {},
	QVidUploadDate:   {},
	QVidDuration:     {},
	QVidMetadata:     {},
	QVidDLStatus:     {},
	QVidDLPercentage: {},
	QVidMetarrStatus: {},
	QVidMetarrError:  {},
	QVidOverrides:    {},
	QVidCreatedAt:    {},
	QVidUpdatedAt:    {},
	QVidChanName:     {},
}

// Downloads database entry keys.
const (
	QDLVidID     = "video_id"
	QDLStatus    = "status"
	QDLPct       = "percentage"
	QDLError     = "error"
	QDLCreatedAt = "created_at"
	QDLUpdatedAt = "updated_at"
)

// ValidDownloadKeys holds valid keys for the download database.
var ValidDownloadKeys = map[string]struct{}{
	QDLVidID:     {},
	QDLStatus:    {},
	QDLPct:       {},
	QDLError:     {},
	QDLCreatedAt: {},
	QDLUpdatedAt: {},
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
var ValidNotificationKeys = map[string]struct{}{
	QNotifyChanID:    {},
	QNotifyName:      {},
	QNotifyURL:       {},
	QNotifyChanURL:   {},
	QNotifyCreatedAt: {},
	QNotifyUpdatedAt: {},
}

// Blocked domains database entry keys.
const (
	QBlockedDomain  = "domain"
	QBlockedContext = "context"
	QBlockedAt      = "blocked_at"
)

// ValidBlockedDomainKeys holds valid keys for the blocked domains database.
var ValidBlockedDomainKeys = map[string]struct{}{
	QBlockedDomain:  {},
	QBlockedContext: {},
	QBlockedAt:      {},
}

// Misc
const (
	ManualDownloadsCol = "manual-downloads"
)

// DownloadStatus holds constant download status strings.
type DownloadStatus string

// Download status strings.
const (
	DLStatusAvailable   DownloadStatus = "Available" // Found by a crawl, awaiting the user's choice to download it.
	DLStatusQueued      DownloadStatus = "Queued"
	DLStatusDownloading DownloadStatus = "Downloading"
	DLStatusCompleted   DownloadStatus = "Finished"
	DLStatusFailed      DownloadStatus = "Failed"
	DLStatusIgnored     DownloadStatus = "Ignored"
	DLStatusCanceled    DownloadStatus = "Canceled"
)

// MetarrStatus holds constant Metarr processing status strings.
type MetarrStatus string

// Metarr status strings.
const (
	MetarrStatusPending MetarrStatus = "pending"
	MetarrStatusFailed  MetarrStatus = "failed"
	MetarrStatusDone    MetarrStatus = "done"
)
