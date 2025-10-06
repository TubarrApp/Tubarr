package db

import (
	"database/sql"
	"embed"
	"fmt"
)

//go:embed sql/*.sql
var sqlFiles embed.FS

const (
	channelSQL      = "sql/channels.sql"
	channelURLsSQL  = "sql/channel_urls.sql"
	downloadSQL     = "sql/downloads.sql"
	notificationSQL = "sql/notifications.sql"
	programSQL      = "sql/program.sql"
	videoSQL        = "sql/videos.sql"
)

// initProgramTable initializes the primary program database table.
func initProgramTable(tx *sql.Tx) error {
	return executeSQLFile(tx, programSQL, "programs table")
}

// initChannelsTable intializes channel tables.
func initChannelsTable(tx *sql.Tx) error {
	return executeSQLFile(tx, channelSQL, "channels table")
}

// initChannelURLsTable intializes channel tables.
func initChannelURLsTable(tx *sql.Tx) error {
	return executeSQLFile(tx, channelURLsSQL, "channel urls table")
}

// initNotifyTable initializes notification service tables.
func initNotifyTable(tx *sql.Tx) error {
	return executeSQLFile(tx, notificationSQL, "notifications table")
}

// initVideosTable initializes videos tables.
func initVideosTable(tx *sql.Tx) error {
	return executeSQLFile(tx, videoSQL, "videos table")
}

// initDownloadsTable initializes the download status table for videos.
func initDownloadsTable(tx *sql.Tx) error {
	return executeSQLFile(tx, downloadSQL, "downloads table")
}

// readSQLFile reads the SQL file stored in memory from go:embed.
func readSQLFile(filename string) (string, error) {
	data, err := sqlFiles.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to read SQL file %s: %w", filename, err)
	}
	return string(data), nil
}

// executeSQLFile executes the SQL file stored in memory from go:embed.
func executeSQLFile(tx *sql.Tx, filename, tableName string) error {
	query, err := readSQLFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read SQL file %s: %w", filename, err)
	}
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to execute SQL for %s: %w", tableName, err)
	}
	return nil
}
