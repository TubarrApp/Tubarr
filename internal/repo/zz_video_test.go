package repo

import (
	"database/sql"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
)

// TestUpdateVideoKeepsMetadata tests that updating a video without loaded metadata keeps its stored metadata.
func TestUpdateVideoKeepsMetadata(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	v := &models.Video{URL: "https://example.com/v/1", ChannelURL: "https://example.com/@a"}
	id, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		t.Helper()
		var m sql.NullString
		if err := cs.db.QueryRow("SELECT "+consts.QVidMetadata+" FROM "+consts.DBVideos+" WHERE id = ?", id).Scan(&m); err != nil {
			t.Fatal(err)
		}
		return m.String
	}

	v.MetadataMap = map[string]any{"title": "A"}
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		t.Fatal(err)
	}
	v.MetadataMap = nil
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got != `{"title":"A"}` {
		t.Errorf("metadata after an update without it: got %q, want it kept", got)
	}

	v.MetadataMap = map[string]any{"title": "B"}
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got != `{"title":"B"}` {
		t.Errorf("metadata after an update with new metadata: got %q, want it replaced", got)
	}
}

// TestUpdateVideoPaths tests that paths are only stored while the row still holds the paths a run started from.
func TestUpdateVideoPaths(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	v := &models.Video{URL: "https://example.com/v/1", ChannelURL: "https://example.com/@a"}
	id, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	v.VideoFilePath, v.JSONFilePath = "/in/v.webm", "/in/v.json"
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		t.Fatal(err)
	}

	paths := func() (string, string) {
		t.Helper()
		var video, json string
		if err := cs.db.QueryRow("SELECT "+consts.QVidVideoPath+", "+consts.QVidJSONPath+" FROM "+consts.DBVideos+" WHERE id = ?", id).Scan(&video, &json); err != nil {
			t.Fatal(err)
		}
		return video, json
	}

	// A newer download changed the row, so the stale run's paths are not stored.
	if updated, err := vs.UpdateVideoPaths(id, "/old/v.webm", "/old/v.json", "/out/stale.mp4", "/out/stale.json"); err != nil || updated {
		t.Errorf("stale update: updated=%v err=%v, want not updated", updated, err)
	}
	if video, json := paths(); video != "/in/v.webm" || json != "/in/v.json" {
		t.Errorf("stale update changed paths to %q, %q", video, json)
	}

	if updated, err := vs.UpdateVideoPaths(id, "/in/v.webm", "/in/v.json", "/out/v.mp4", ""); err != nil || !updated {
		t.Fatalf("matching update: updated=%v err=%v, want updated", updated, err)
	}
	if video, json := paths(); video != "/out/v.mp4" || json != "" {
		t.Errorf("paths = %q, %q; want /out/v.mp4 and empty", video, json)
	}
}

// TestAddVideosDuplicateURL tests that a URL listed twice in one batch is stored once without an error.
func TestAddVideosDuplicateURL(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	videos := []*models.Video{
		{URL: "https://example.com/v/1", ChannelURLID: c.URLModels[0].ID, ChannelURL: c.URLModels[0].URL},
		{URL: "https://example.com/v/1", ChannelURLID: c.URLModels[1].ID, ChannelURL: c.URLModels[1].URL},
	}
	for _, v := range videos {
		v.MarkVideoAsIgnored()
	}
	if _, err := vs.AddVideos(videos, c.ID); err != nil {
		t.Fatalf("AddVideos: %v", err)
	}

	var n int
	if err := cs.db.QueryRow("SELECT COUNT(*) FROM "+consts.DBVideos+" WHERE "+consts.QVidURL+" = ?", "https://example.com/v/1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("stored %d rows, want 1", n)
	}
	if videos[0].ID == 0 || videos[1].ID != videos[0].ID {
		t.Errorf("IDs %d and %d, want both the stored row's", videos[0].ID, videos[1].ID)
	}
}

// TestFinishedVideoLists tests the latest finished and the downloaded video lists, newest first, and the failed Metarr count.
func TestFinishedVideoLists(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	cu := c.URLModels[0]
	for day, v := range []*models.Video{
		{URL: "https://example.com/v/old", Finished: true},
		{URL: "https://example.com/v/mid", Finished: true},
		{URL: "https://example.com/v/ignored", Ignored: true},
		{URL: "https://example.com/v/new", Finished: true, Title: "New"},
		{URL: "https://example.com/v/unfinished"},
	} {
		v.ChannelURL = cu.URL
		id, err := vs.AddVideo(v, c.ID, cu.ID)
		if err != nil {
			t.Fatal(err)
		}
		updated := time.Date(2024, 1, day+1, 0, 0, 0, 0, time.UTC)
		if _, err := cs.db.Exec("UPDATE "+consts.DBVideos+" SET "+consts.QVidUpdatedAt+" = ? WHERE id = ?", updated, id); err != nil {
			t.Fatal(err)
		}
	}
	urls := func(videos []*models.Video) []string {
		got := make([]string, 0, len(videos))
		for _, v := range videos {
			got = append(got, v.URL)
		}
		return got
	}

	latest, err := vs.GetLatestFinishedVideos(t.Context(), c.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/v/new", "https://example.com/v/mid"}; !slices.Equal(urls(latest), want) {
		t.Fatalf("latest finished: got %q, want %q", urls(latest), want)
	}
	if v := latest[0]; v.Title != "New" || v.ChannelURLID != cu.ID || v.ChannelURL != cu.URL || !v.Finished {
		t.Errorf("latest finished: fields not read: %+v", v)
	}
	if n, err := vs.CountFinishedVideos(t.Context(), c.ID); err != nil || n != 3 {
		t.Errorf("finished count: got %d, %v, want 3", n, err)
	}

	// Ignored videos are listed on the Available page instead.
	all, _, err := cs.GetDownloadedVideos(c)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com/v/new", "https://example.com/v/mid", "https://example.com/v/old"}
	if !slices.Equal(urls(all), want) {
		t.Fatalf("downloaded: got %q, want %q", urls(all), want)
	}

	// Only downloaded, not ignored, videos Metarr failed on are counted.
	for u, status := range map[string]consts.MetarrStatus{
		"https://example.com/v/old":     consts.MetarrStatusFailed,
		"https://example.com/v/mid":     consts.MetarrStatusFailed,
		"https://example.com/v/new":     consts.MetarrStatusDone,
		"https://example.com/v/ignored": consts.MetarrStatusFailed,
	} {
		if _, err := cs.db.Exec("UPDATE "+consts.DBVideos+" SET "+consts.QVidMetarrStatus+" = ? WHERE "+consts.QVidURL+" = ?", status, u); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := vs.CountFailedMetarrVideos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[c.ID] != 2 {
		t.Errorf("failed Metarr counts: got %v, want 2 for channel %d", counts, c.ID)
	}
}

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
