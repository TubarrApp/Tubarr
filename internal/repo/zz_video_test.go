package repo

import (
	"database/sql"
	"slices"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
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

// TestFinishedVideoLists tests the latest finished and the downloaded or ignored video lists, newest first.
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

	all, _, err := cs.GetDownloadedOrIgnoredVideos(c)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com/v/new", "https://example.com/v/ignored", "https://example.com/v/mid", "https://example.com/v/old"}
	if !slices.Equal(urls(all), want) {
		t.Fatalf("downloaded or ignored: got %q, want %q", urls(all), want)
	}
	if !all[1].Ignored {
		t.Errorf("downloaded or ignored: ignored video read as not ignored")
	}
}
