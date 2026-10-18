package database

import (
	"database/sql"
	"embed"
	"fmt"
)

//go:embed sql/*.sql
var sqlFiles embed.FS

const (
	blockedDomainsSQL = "sql/blocked_domains.sql"
	channelSQL        = "sql/channels.sql"
	channelURLsSQL    = "sql/channel_urls.sql"
	downloadSQL       = "sql/downloads.sql"
	notificationSQL   = "sql/notifications.sql"
	programSQL        = "sql/program.sql"
	settingsSQL       = "sql/settings.sql"
	videoSQL          = "sql/videos.sql"
	webSessionsSQL    = "sql/web_sessions.sql"
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

// initBlockedDomainsTable initializes the blocked domains table.
func initBlockedDomainsTable(tx *sql.Tx) error {
	return executeSQLFile(tx, blockedDomainsSQL, "blocked domains table")
}

// initSettingsTable initializes the global settings table.
func initSettingsTable(tx *sql.Tx) error {
	return executeSQLFile(tx, settingsSQL, "settings table")
}

// initWebSessionsTable initializes the web interface login sessions table.
func initWebSessionsTable(tx *sql.Tx) error {
	return executeSQLFile(tx, webSessionsSQL, "web sessions table")
}

// migrateURLLastScan sets each channel URL's last crawl to its channel's, once.
func migrateURLLastScan(tx *sql.Tx) error {
	res, err := tx.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES ('migrated-url-last-scan', 'true')`)
	if err != nil {
		return fmt.Errorf("failed to record the channel URL last scan migration: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err // Already migrated.
	}
	if _, err := tx.Exec(`UPDATE channel_urls SET last_scan = (SELECT last_scan FROM channels WHERE channels.id = channel_urls.channel_id)`); err != nil {
		return fmt.Errorf("failed to migrate channel URL last scan times: %w", err)
	}
	return nil
}

// addColumnIfMissing adds a column to an existing table (for databases created by older versions).
func addColumnIfMissing(tx *sql.Tx, table, column, definition string) (err error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("failed to read columns of table %s: %w", table, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	for rows.Next() {
		var (
			cid, notNull, pk int
			name, colType    string
			defaultVal       sql.NullString
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("failed to scan columns of table %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read columns of table %s: %w", table, err)
	}

	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("failed to add column %s to table %s: %w", column, table, err)
	}
	return nil
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
