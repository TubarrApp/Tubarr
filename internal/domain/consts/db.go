package consts

// Tables
const (
	DBProgram       = "program"
	DBChannels      = "channels"
	DBVideos        = "videos"
	DBNotifications = "notifications"
)

// Program
const (
	QProgHost      = "host"
	QProgID        = "id"
	QProgHeartbeat = "last_heartbeat"
	QProgProgramID = "program_id"
	QProgPID       = "pid"
	QProgStartedAt = "started_at"
	QProgRunning   = "running"
)

// Channel
const (
	QChanID              = "id"
	QChanURL             = "url"
	QChanName            = "name"
	QChanConcurrency     = "concurrency"
	QChanVDir            = "video_directory"
	QChanJDir            = "json_directory"
	QChanMetarrOutputDir = "metarr_output_directory"
	QChanSettings        = "settings"
	QChanMetarr          = "metarr"
	QChanLastScan        = "last_scan"
	QChanCreatedAt       = "created_at"
	QChanUpdatedAt       = "updated_at"
)

// Videos
const (
	QVidID          = "id"
	QVidChanID      = "channel_id"
	QVidDownloaded  = "downloaded"
	QVidURL         = "url"
	QVidTitle       = "title"
	QVidDescription = "description"
	QVidVDir        = "video_directory"
	QVidJDir        = "json_directory"
	QVidVPath       = "video_path"
	QVidJPath       = "json_path"
	QVidSettings    = "settings"
	QVidMetarr      = "metarr"
	QVidUploadDate  = "upload_date"
	QVidMetadata    = "metadata"
	QVidCreatedAt   = "created_at"
	QVidUpdatedAt   = "updated_at"
)

// Notification
const (
	QNotifyChanID    = "channel_id"
	QNotifyName      = "name"
	QNotifyURL       = "notify_url"
	QNotifyCreatedAt = "created_at"
	QNotifyUpdatedAt = "updated_at"
)

// DLStatus holds constant download status strings.
type DLStatus string

const (
	DLStatusEmpty       DLStatus = ""
	DLStatusPending     DLStatus = "waiting"
	DLStatusDownloading DLStatus = "downloading"
	DLStatusCompleted   DLStatus = "finished"
	DLStatusFailed      DLStatus = "failed"
)
