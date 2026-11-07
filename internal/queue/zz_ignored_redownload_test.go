package queue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

const filteredVideoURL = "https://example.com/v/filtered-out"

// storeFilteredVideo stores a video an earlier run filtered out.
func storeFilteredVideo(t *testing.T, store contracts.Store, c *models.Channel) {
	t.Helper()
	v := &models.Video{URL: filteredVideoURL, ChannelURLID: c.URLModels[0].ID, ChannelURL: c.URLModels[0].URL, Title: "Filtered out"}
	v.MarkVideoAsIgnored()
	if _, err := store.VideoStore().AddVideos([]*models.Video{v}, c.ID); err != nil {
		t.Fatal(err)
	}
}

// checkOnAvailablePage fails the test unless the filtered out video is on the Available page.
func checkOnAvailablePage(t *testing.T, store contracts.Store, status consts.DownloadStatus) {
	t.Helper()
	available, err := store.VideoStore().GetAvailableVideos(consts.DLStatusAvailable, consts.DLStatusFailed, consts.DLStatusCanceled)
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || available[0].DownloadStatus.Status != status {
		t.Errorf("Available videos = %+v, want the video with status %q", available, status)
	}
}

// TestFilteredVideoCanceledWhileWaiting tests a filtered out video entered by its URL returns to Available if canceled
// before its metadata is saved.
func TestFilteredVideoCanceledWhileWaiting(t *testing.T) {
	store, _, c := newTestChannel(t)
	storeFilteredVideo(t, store, c)
	cu := c.URLModels[0]
	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))

	// The channel's one slot is taken, so the video waits.
	dq.SetLimits(c)
	takeSlot(t, &dq.lanes, c.ID, cu.ID)

	v := &models.Video{URL: filteredVideoURL, ChannelURLID: cu.ID}
	if queued, err := dq.Add(c, Request{Video: v, ByUser: true}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want it queued", len(queued), err)
	}
	if !dq.Cancel(v.ID) {
		t.Fatal("Cancel found no video")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("canceled video still in the queue")
	}
	checkOnAvailablePage(t, store, consts.DLStatusCanceled)
}

// TestFilteredVideoMetadataFails tests a filtered out video entered by its URL returns to Available if its metadata can't
// be fetched.
func TestFilteredVideoMetadataFails(t *testing.T) {
	store, _, c := newTestChannel(t)
	storeFilteredVideo(t, store, c)
	cu := c.URLModels[0]
	cu.ChanURLSettings.Retries = 1

	// yt-dlp fails.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sharedconsts.ProgramYTDLP), []byte("#!/bin/sh\necho 'ERROR: the site is down' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	abstractions.Set(keys.SkipAllWaits, true)
	t.Cleanup(func() { abstractions.Set(keys.SkipAllWaits, nil) })

	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))
	v := &models.Video{URL: filteredVideoURL, ChannelURLID: cu.ID}
	if queued, err := dq.Add(c, Request{Video: v, ByUser: true}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want it queued", len(queued), err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("failed video still in the queue")
	}
	checkOnAvailablePage(t, store, consts.DLStatusFailed)
}
