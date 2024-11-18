package models

import (
	"encoding/json"
	"time"
)

// Matches the order of the DB table, do not alter
type Channel struct {
	ID   int64  `db:"id"`
	URL  string `db:"url"`
	Name string `db:"name"`
	VDir string `db:"video_directory"`
	JDir string `db:"json_directory"`

	Settings   ChannelSettings `json:"settings" db:"settings"`
	MetarrArgs MetarrArgs      `json:"metarr" db:"metarr"`

	LastScan  time.Time `db:"last_scan"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Video struct {
	ID          int64
	ChannelID   int64                  `db:"channel_id"`
	Downloaded  bool                   `db:"downloaded"`
	VDir        string                 `db:"video_directory"`
	VPath       string                 `db:"video_path"`
	JDir        string                 `db:"json_directory"`
	JPath       string                 `db:"json_path"`
	URL         string                 `db:"url"`
	Title       string                 `db:"title"`
	Description string                 `db:"description"`
	UploadDate  time.Time              `db:"upload_date"`
	MetadataMap map[string]interface{} `db:"-"`

	Channel    *Channel        `db:"-"`
	Settings   ChannelSettings `json:"settings" db:"settings"`
	MetarrArgs MetarrArgs      `json:"metarr" db:"metarr"`

	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Download struct {
	ID           int64           `db:"id"`
	VideoID      int64           `db:"video_id"`
	Status       string          `db:"status"` // pending, downloading, completed, failed
	FilePath     string          `db:"file_path"`
	FileSize     int64           `db:"file_size"`
	StartedAt    time.Time       `db:"started_at"`
	CompletedAt  time.Time       `db:"completed_at"`
	ErrorMessage string          `db:"error_message"`
	SettingsUsed json.RawMessage `db:"settings_used"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`

	VDir  string `db:"video_directory"`
	JDir  string `db:"json_directory"`
	JPath string `db:"json_path"`
	URL   string
}
