package consts

// Tables
const (
	DBProgram       = "program"
	DBChannels      = "channels"
	DBChannelURLs   = "channel_urls"
	DBVideos        = "videos"
	DBDownloads     = "downloads"
	DBNotifications = "notifications"
)

// Program
const (
	QProgHost      = "host"
	QProgID        = "id"
	QProgHeartbeat = "last_heartbeat"
	QProgPID       = "pid"
	QProgStartedAt = "started_at"
	QProgRunning   = "running"
)

// Channel
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

// Channel URLs
const (
	QChanURLsID        = "id"
	QChanURLsChannelID = "channel_id"
	QChanURLsURL       = "url"
	QChanURLsUsername  = "username"
	QChanURLsPassword  = "password"
	QChanURLsLoginURL  = "login_url"
	QChanURLsLastScan  = "last_scan"
	QChanURLsCreatedAt = "created_at"
	QChanURLsUpdatedAt = "updated_at"
)

// Videos
const (
	QVidID          = "id"
	QVidChanID      = "channel_id"
	QVidFinished    = "finished"
	QVidURL         = "url"
	QVidTitle       = "title"
	QVidDescription = "description"
	QVidVideoPath   = "video_path"
	QVidJSONPath    = "json_path"
	QVidSettings    = "settings"
	QVidMetarr      = "metarr"
	QVidUploadDate  = "upload_date"
	QVidMetadata    = "metadata"
	QVidDLStatus    = "download_status"
	QVidCreatedAt   = "created_at"
	QVidUpdatedAt   = "updated_at"
)

// Downloads
const (
	QDLVidID     = "video_id"
	QDLStatus    = "status"
	QDLPct       = "percentage"
	QDLCreatedAt = "created_at"
	QDLUpdatedAt = "updated_at"
)

// Notification
const (
	QNotifyChanID    = "channel_id"
	QNotifyName      = "name"
	QNotifyURL       = "notify_url"
	QNotifyCreatedAt = "created_at"
	QNotifyUpdatedAt = "updated_at"
)

// DownloadStatus holds constant download status strings.
type DownloadStatus string

const (
	DLStatusPending     DownloadStatus = "Pending"
	DLStatusDownloading DownloadStatus = "Downloading"
	DLStatusCompleted   DownloadStatus = "Finished"
	DLStatusFailed      DownloadStatus = "Failed"
)
