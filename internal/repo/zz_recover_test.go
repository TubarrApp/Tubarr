package repo

import (
	"database/sql"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
)

// TestRecoverInterrupted tests startup repairs videos left mid-Metarr or mid-download, and leaves every other video alone.
func TestRecoverInterrupted(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	res, err := db.Exec("INSERT INTO channels (name) VALUES ('Test')")
	if err != nil {
		t.Fatal(err)
	}
	chanID, _ := res.LastInsertId()

	type row struct {
		finished, ignored    int
		videoPath            any
		metarrStatus, status string
		errMsg               string
	}
	const stopped = "Tubarr stopped before this video finished"
	cases := map[string]struct{ before, want row }{
		"mid-Metarr":              {row{0, 0, "/v/1.mp4", "done", "Finished", ""}, row{1, 0, "/v/1.mp4", "pending", "Finished", ""}},
		"mid-Metarr, no path":     {row{0, 0, nil, "done", "Finished", ""}, row{0, 0, nil, "done", "Failed", stopped}},
		"mid-Metarr, empty path":  {row{0, 0, "", "done", "Finished", ""}, row{0, 0, "", "done", "Failed", stopped}},
		"queued":                  {row{0, 0, nil, "done", "Queued", ""}, row{0, 0, nil, "done", "Failed", stopped}},
		"downloading":             {row{0, 0, nil, "done", "Downloading", ""}, row{0, 0, nil, "done", "Failed", stopped}},
		"available":               {row{0, 0, nil, "done", "Available", ""}, row{0, 0, nil, "done", "Available", ""}},
		"failed":                  {row{0, 0, nil, "done", "Failed", "yt-dlp error"}, row{0, 0, nil, "done", "Failed", "yt-dlp error"}},
		"canceled":                {row{0, 0, nil, "done", "Canceled", ""}, row{0, 0, nil, "done", "Canceled", ""}},
		"ignored":                 {row{0, 1, nil, "done", "Ignored", ""}, row{0, 1, nil, "done", "Ignored", ""}},
		"finished":                {row{1, 0, "/v/2.mp4", "done", "Finished", ""}, row{1, 0, "/v/2.mp4", "done", "Finished", ""}},
		"finished, Metarr failed": {row{1, 0, "/v/3.mp4", "failed", "Finished", ""}, row{1, 0, "/v/3.mp4", "failed", "Finished", ""}},
	}

	ids := map[string]int64{}
	for name, tc := range cases {
		b := tc.before
		res, err := db.Exec("INSERT INTO videos (channel_id, url, finished, ignored, video_path, metarr_status) VALUES (?, ?, ?, ?, ?, ?)",
			chanID, "https://example.com/"+name, b.finished, b.ignored, b.videoPath, b.metarrStatus)
		if err != nil {
			t.Fatal(err)
		}
		ids[name], _ = res.LastInsertId()
		if _, err := db.Exec("INSERT INTO downloads (video_id, status, error) VALUES (?, ?, ?)", ids[name], b.status, b.errMsg); err != nil {
			t.Fatal(err)
		}
	}

	vs := GetVideoStore(db)
	pending, failed, err := vs.RecoverInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if pending != 1 || failed != 4 {
		t.Errorf("got %d pending, %d failed, want 1 and 4", pending, failed)
	}

	for name, tc := range cases {
		var (
			got       row
			videoPath sql.NullString
			errMsg    sql.NullString
		)
		if err := db.QueryRow("SELECT v.finished, v.ignored, v.video_path, v.metarr_status, d.status, d.error FROM videos v JOIN downloads d ON d.video_id = v.id WHERE v.id = ?",
			ids[name]).Scan(&got.finished, &got.ignored, &videoPath, &got.metarrStatus, &got.status, &errMsg); err != nil {
			t.Fatal(err)
		}
		if videoPath.Valid {
			got.videoPath = videoPath.String
		}
		got.errMsg = errMsg.String
		if got != tc.want {
			t.Errorf("%s: got %+v, want %+v", name, got, tc.want)
		}
	}

	// A second startup finds nothing left to repair.
	if pending, failed, err = vs.RecoverInterrupted(); err != nil || pending != 0 || failed != 0 {
		t.Errorf("second run: got %d pending, %d failed, %v, want nothing", pending, failed, err)
	}

	// Recovered videos are the ones Run Metarr picks up.
	videos, err := vs.GetVideosAwaitingMetarr(chanID, false)
	if err != nil {
		t.Fatal(err)
	}
	var awaiting []int64
	for _, v := range videos {
		awaiting = append(awaiting, v.ID)
	}
	if len(awaiting) != 2 || !slices.Contains(awaiting, ids["mid-Metarr"]) || !slices.Contains(awaiting, ids["finished, Metarr failed"]) {
		t.Errorf("awaiting Metarr: got %v, want the mid-Metarr and failed Metarr videos", awaiting)
	}
}
