package repo

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"tubarr/internal/app"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
)

const (
	redownloadChannelURL = "https://example.com/@a"
	redownloadVideoURL   = "https://example.com/v/1"
)

// redownloadStores returns stores on a fresh database with one channel and channel URL.
func redownloadStores(t *testing.T) (*ChannelStore, *VideoStore, *DownloadStore, *models.Channel, int64) {
	t.Helper()
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
	if res, err = db.Exec("INSERT INTO channel_urls (channel_id, url) VALUES (?, ?)", chanID, redownloadChannelURL); err != nil {
		t.Fatal(err)
	}
	cuID, _ := res.LastInsertId()
	cs, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return cs, GetVideoStore(db), GetDownloadStore(db), &models.Channel{ID: chanID, Name: "Test"}, cuID
}

// TestIgnoredVideoReturnsToAvailable tests an ignored video chosen again shows on the Available page if it then fails or
// is canceled before its metadata is saved, rather than on no page at all.
func TestIgnoredVideoReturnsToAvailable(t *testing.T) {
	for _, status := range []consts.DownloadStatus{consts.DLStatusFailed, consts.DLStatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			_, vs, ds, c, cuID := redownloadStores(t)
			v := &models.Video{ChannelURLID: cuID, ChannelURL: redownloadChannelURL, URL: redownloadVideoURL, Metadata: models.VideoMetadata{Title: "One"}}
			v.MarkVideoAsIgnored()
			if _, err := vs.AddVideos([]*models.Video{v}, c.ID); err != nil {
				t.Fatal(err)
			}

			chosen := &models.Video{URL: redownloadVideoURL, ChannelURLID: cuID, ChannelURL: redownloadChannelURL}
			if claimed, err := vs.QueueVideo(c.ID, chosen, true); err != nil || !claimed {
				t.Fatalf("QueueVideo = %v, %v", claimed, err)
			}
			if err := ds.UpdateDownloadStatus(context.Background(), models.StatusUpdate{VideoID: chosen.ID, VideoURL: chosen.URL, Status: status, Error: errors.New("boom")}); err != nil {
				t.Fatal(err)
			}

			available, err := vs.GetAvailableVideos(app.AvailableStatuses...)
			if err != nil {
				t.Fatal(err)
			}
			if len(available) != 1 || available[0].DownloadStatus.Status != status {
				t.Errorf("Available videos = %+v, want the video with status %q", available, status)
			}
		})
	}
}

// TestRestartRecoversRedownload tests a video re-downloaded when Tubarr was killed can be downloaded again after a restart.
func TestRestartRecoversRedownload(t *testing.T) {
	for _, tc := range []struct {
		name, column string
		status       consts.DownloadStatus
	}{
		{"finished", consts.QVidFinished, consts.DLStatusCompleted},
		{"ignored", consts.QVidIgnored, consts.DLStatusIgnored},
		{"available", "", consts.DLStatusAvailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, vs, _, c, cuID := redownloadStores(t)
			db := cs.GetDB()
			videos := []*models.Video{{ChannelURLID: cuID, ChannelURL: redownloadChannelURL, URL: redownloadVideoURL, Metadata: models.VideoMetadata{Title: "One"}}}
			if _, err := vs.AddAvailableVideos(videos, c.ID); err != nil {
				t.Fatal(err)
			}
			if tc.column != "" {
				if _, err := db.Exec("UPDATE videos SET " + tc.column + " = 1"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("UPDATE downloads SET status = ?", tc.status); err != nil {
					t.Fatal(err)
				}
			}
			if claimed, err := vs.QueueVideo(c.ID, &models.Video{URL: redownloadVideoURL}, true); err != nil || !claimed {
				t.Fatalf("QueueVideo = %v, %v", claimed, err)
			}

			if _, _, err := vs.RecoverInterrupted(); err != nil {
				t.Fatal(err)
			}
			var status consts.DownloadStatus
			if err := db.QueryRow("SELECT status FROM downloads").Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != consts.DLStatusFailed {
				t.Errorf("status after restart = %q, want %q", status, consts.DLStatusFailed)
			}
			if claimed, err := vs.QueueVideo(c.ID, &models.Video{URL: redownloadVideoURL}, true); err != nil || !claimed {
				t.Errorf("QueueVideo after restart = %v, %v, want it claimed", claimed, err)
			}
		})
	}
}

// TestRestartKeepsFinishedDownloads tests finished downloads aren't marked failed by the restart recovery.
func TestRestartKeepsFinishedDownloads(t *testing.T) {
	cs, vs, _, c, cuID := redownloadStores(t)
	db := cs.GetDB()
	videos := []*models.Video{{ChannelURLID: cuID, ChannelURL: redownloadChannelURL, URL: redownloadVideoURL, Metadata: models.VideoMetadata{Title: "One"}}}
	if _, err := vs.AddAvailableVideos(videos, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE videos SET finished = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE downloads SET status = ?", consts.DLStatusCompleted); err != nil {
		t.Fatal(err)
	}

	if _, _, err := vs.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	var status consts.DownloadStatus
	if err := db.QueryRow("SELECT status FROM downloads").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != consts.DLStatusCompleted {
		t.Errorf("status after restart = %q, want %q", status, consts.DLStatusCompleted)
	}
}
