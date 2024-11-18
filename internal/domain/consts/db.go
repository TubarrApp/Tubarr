package consts

// Tables
const (
	DBChannels  = "channels"
	DBVideos    = "videos"
	DBDownloads = "downloads"
)

// Channel
const (
	QChanID        = "id"
	QChanURL       = "url"
	QChanName      = "name"
	QChanVDir      = "video_directory"
	QChanJDir      = "json_directory"
	QChanSettings  = "settings"
	QChanMetarr    = "metarr"
	QChanLastScan  = "last_scan"
	QChanCreatedAt = "created_at"
	QChanUpdatedAt = "updated_at"
)

// Videos
const (
	QVidChanID      = "channel_id"
	QVidDownloaded  = "downloaded"
	QVidURL         = "url"
	QVidTitle       = "title"
	QVidDescription = "description"
	QVidVDir        = "video_directory"
	QVidJDir        = "json_directory"
	QVidSettings    = "settings"
	QVidMetarr      = "metarr"
	QVidUploadDate  = "upload_date"
	QVidMetadata    = "metadata"
	QVidCreatedAt   = "created_at"
	QVidUpdatedAt   = "updated_at"
)

// DL status
const (
	DLStatusPending     = "pending"
	DLStatusDownloading = "downloading"
	DLStatusCompleted   = "finished"
	DLStatusFailed      = "failed"
)
