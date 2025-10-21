package consts

// ValidDBColumns maps all valid database column keys to true for quick lookup.
var ValidDBColumns = map[string]bool{
	// Shared
	QSharedID:        true,
	QSharedChannelID: true,
	QSharedSettings:  true,
	QSharedMetarr:    true,
	QSharedLastScan:  true,
	QSharedCreatedAt: true,
	QSharedUpdatedAt: true,

	// Program keys
	QProgHost:      true,
	QProgHeartbeat: true,
	QProgPID:       true,
	QProgStartedAt: true,
	QProgRunning:   true,

	// Channel keys
	QChanName:            true,
	QChanConcurrency:     true,
	QChanMetarrOutputDir: true,
	QChanMetarrExt:       true,

	// Channel URL keys
	QChanURLURL:      true,
	QChanURLUsername: true,
	QChanURLPassword: true,
	QChanURLLoginURL: true,
	QChanURLIsManual: true,

	// Video keys
	QVidChanURLID:   true,
	QVidFinished:    true,
	QVidTitle:       true,
	QVidDescription: true,
	QVidVideoPath:   true,
	QVidJSONPath:    true,
	QVidUploadDate:  true,
	QVidMetadata:    true,
	QVidDLStatus:    true,

	// Downloads keys
	QDLVidID:  true,
	QDLStatus: true,
	QDLPct:    true,

	// Notification keys
	QNotifyURL:     true,
	QNotifyChanURL: true,
}

// Shared column names for valid DB column name map.
const (
	QSharedID        = "id"
	QSharedChannelID = "channel_id"
	QSharedName      = "name"
	QSharedURL       = "url"
	QSharedLastScan  = "last_scan"
	QSharedCreatedAt = "created_at"
	QSharedUpdatedAt = "updated_at"
	QSharedSettings  = "settings"
	QSharedMetarr    = "metarr"
)

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

// Channel database entry keys.
const (
	QChanID              = "id"
	QChanName            = "name"
	QChanConcurrency     = "concurrency"
	QChanMetarrOutputDir = "metarr_output_directory"
	QChanMetarrExt       = "metarr_ext"
	QChanSettings        = "settings"
	QChanMetarr          = "metarr"
	QChanLastScan        = "last_scan"
	QChanCreatedAt       = "created_at"
	QChanUpdatedAt       = "updated_at"
)

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

// Video database entry keys.
const (
	QVidID          = "id"
	QVidChanID      = "channel_id"
	QVidChanURLID   = "channel_url_id"
	QVidFinished    = "finished"
	QVidURL         = "url"
	QVidTitle       = "title"
	QVidDescription = "description"
	QVidVideoPath   = "video_path"
	QVidJSONPath    = "json_path"
	QVidUploadDate  = "upload_date"
	QVidMetadata    = "metadata"
	QVidDLStatus    = "download_status"
	QVidCreatedAt   = "created_at"
	QVidUpdatedAt   = "updated_at"
)

// Downloads database entry keys.
const (
	QDLVidID     = "video_id"
	QDLStatus    = "status"
	QDLPct       = "percentage"
	QDLCreatedAt = "created_at"
	QDLUpdatedAt = "updated_at"
)

// Notification database entry keys.
const (
	QNotifyChanID    = "channel_id"
	QNotifyName      = "name"
	QNotifyURL       = "notify_url"
	QNotifyChanURL   = "channel_url"
	QNotifyCreatedAt = "created_at"
	QNotifyUpdatedAt = "updated_at"
)

// Misc
const (
	ManualDownloadsCol = "manual-downloads"
)

// DownloadStatus holds constant download status strings.
type DownloadStatus string

// Download status strings.
const (
	DLStatusPending     DownloadStatus = "Pending"
	DLStatusDownloading DownloadStatus = "Downloading"
	DLStatusCompleted   DownloadStatus = "Finished"
	DLStatusFailed      DownloadStatus = "Failed"
)
