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

// DownloadStatus holds constant download status strings.
type DownloadStatus string

// Download status strings.
const (
	DLStatusPending     DownloadStatus = "Pending"
	DLStatusDownloading DownloadStatus = "Downloading"
	DLStatusCompleted   DownloadStatus = "Finished"
	DLStatusFailed      DownloadStatus = "Failed"
)
