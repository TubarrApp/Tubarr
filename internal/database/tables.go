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
	profilesSQL       = "sql/profiles.sql"
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

// initProfilesTable initializes the settings profiles table.
func initProfilesTable(tx *sql.Tx) error {
	return executeSQLFile(tx, profilesSQL, "profiles table")
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
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil // Already migrated.
	}
	if _, err := tx.Exec(`UPDATE channel_urls SET last_scan = (SELECT last_scan FROM channels WHERE channels.id = channel_urls.channel_id)`); err != nil {
		return fmt.Errorf("failed to migrate channel URL last scan times: %w", err)
	}
	return nil
}

// migrateAppendOps renames stored "append" meta and filename operations to "suffix", once.
func migrateAppendOps(tx *sql.Tx) error {
	res, err := tx.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES ('migrated-append-ops', 'true')`)
	if err != nil {
		return fmt.Errorf("failed to record the append operation migration: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil // Already migrated.
	}

	// Ops are stored by json.Marshal, so each type is written exactly as matched here.
	const rename = `REPLACE(REPLACE(%[2]s, '"meta_op_type":"append"', '"meta_op_type":"suffix"'), '"filename_op_type":"append"', '"filename_op_type":"suffix"')`
	for _, tc := range [][2]string{{"channels", "metarr"}, {"channel_urls", "metarr"}, {"videos", "overrides"}} {
		query := fmt.Sprintf(`UPDATE %[1]s SET %[2]s = `+rename+` WHERE %[2]s LIKE '%%"append"%%'`, tc[0], tc[1])
		if _, err := tx.Exec(query); err != nil {
			return fmt.Errorf("failed to migrate append operations in %s.%s: %w", tc[0], tc[1], err)
		}
	}
	return nil
}

// migrateNotificationNames rebuilds a notifications table from older versions, which had a name column and allowed
// a notify URL only once per channel, into the current schema.
func migrateNotificationNames(tx *sql.Tx) error {
	has, err := hasColumn(tx, "notifications", "name")
	if err != nil || !has {
		return err
	}

	// The old indexes go with the renamed table, so they're dropped for the new table to create its own.
	for _, stmt := range []string{
		"ALTER TABLE notifications RENAME TO notifications_old",
		"DROP INDEX IF EXISTS idx_notification_channel",
		"DROP INDEX IF EXISTS idx_notification_url",
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to migrate notifications table: %w", err)
		}
	}
	if err := initNotifyTable(tx); err != nil {
		return err
	}
	for _, stmt := range []string{
		`INSERT INTO notifications (id, channel_id, channel_url, notify_url, created_at, updated_at)
			SELECT id, channel_id, COALESCE(channel_url, ''), notify_url, created_at, updated_at FROM notifications_old`,
		"DROP TABLE notifications_old",
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to migrate notifications table: %w", err)
		}
	}
	return nil
}

// addColumnIfMissing adds a column to an existing table (for databases created by older versions).
func addColumnIfMissing(tx *sql.Tx, table, column, definition string) error {
	has, err := hasColumn(tx, table, column)
	if err != nil || has {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("failed to add column %s to table %s: %w", column, table, err)
	}
	return nil
}

// hasColumn reports whether a table has a column.
func hasColumn(tx *sql.Tx, table, column string) (has bool, err error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("failed to read columns of table %s: %w", table, err)
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
			return false, fmt.Errorf("failed to scan columns of table %s: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("failed to read columns of table %s: %w", table, err)
	}
	return false, nil
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
