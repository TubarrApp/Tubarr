package repo

import (
	"database/sql"
	"testing"
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
