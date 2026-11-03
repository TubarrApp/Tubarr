package database

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
)

// countVideoColumn returns how many columns of the videos table have the name (0 or 1).
func countVideoColumn(t *testing.T, db *sql.DB, name string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('videos') WHERE name = ?", name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestInitDBDropsVideoMetadata tests that an older database loses its stored video metadata and the space it took,
// keeps everything else about its videos, and is left alone from then on.
func TestInitDBDropsVideoMetadata(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")

	// A new database never has the column.
	db, err := InitDB()
	if err != nil {
		t.Fatal(err)
	}
	if n := countVideoColumn(t, db, "metadata"); n != 0 {
		t.Fatal("a new database has the metadata column")
	}

	// As an older version left it, with several megabytes stored for each video.
	if _, err := db.Exec("ALTER TABLE videos ADD COLUMN metadata JSON"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO channels (id, name) VALUES (1, 'chan')"); err != nil {
		t.Fatal(err)
	}
	const videos = 10
	blob := `{"formats":"` + strings.Repeat("x", 4<<20) + `"}`
	for i := range videos {
		if _, err := db.Exec("INSERT INTO videos (channel_id, url, video_path, metarr_status, metadata) VALUES (1, ?, '/v.mp4', 'failed', ?)", "https://example.com/"+string(rune('a'+i)), blob); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(paths.DBFilePath)
	if err != nil {
		t.Fatal(err)
	}

	// Checked while still open, as the server stays open: closing the last connection would shrink it anyway.
	if db, err = InitDB(); err != nil {
		t.Fatal(err)
	}
	if n := countVideoColumn(t, db, "metadata"); n != 0 {
		t.Error("the metadata column wasn't dropped")
	}
	after, err := os.Stat(paths.DBFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() >= before.Size()/10 {
		t.Errorf("database file went from %d to %d bytes, want the metadata's space back", before.Size(), after.Size())
	}
	var kept int
	if err := db.QueryRow("SELECT COUNT(*) FROM videos WHERE video_path = '/v.mp4' AND metarr_status = 'failed'").Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != videos {
		t.Errorf("%d of %d videos kept their other columns", kept, videos)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Later starts have nothing to do.
	if db, err = InitDB(); err != nil {
		t.Fatalf("opening again after the drop: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
