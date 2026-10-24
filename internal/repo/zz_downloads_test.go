package repo

import (
	"fmt"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestQueueVideo tests which statuses a video the user chose, and a crawled one, can be queued from.
func TestQueueVideo(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs := GetVideoStore(cs.db)
	statusOf := func(id int64) consts.DownloadStatus {
		t.Helper()
		var got consts.DownloadStatus
		if err := cs.db.QueryRow("SELECT "+consts.QDLStatus+" FROM "+consts.DBDownloads+" WHERE "+consts.QDLVidID+" = ?", id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	for _, tc := range []struct {
		status        consts.DownloadStatus
		byUser, crawl bool // Whether a user's and a crawl's request claim it.
	}{
		{consts.DLStatusAvailable, true, false},
		{consts.DLStatusFailed, true, true},
		{consts.DLStatusCanceled, true, false},
		{consts.DLStatusCompleted, true, false},
		{consts.DLStatusIgnored, true, false},
		{consts.DLStatusQueued, false, false},
		{consts.DLStatusDownloading, false, false},
	} {
		for _, byUser := range []bool{true, false} {
			url := fmt.Sprintf("https://example.com/v/%s-%t", tc.status, byUser)
			id, err := vs.AddVideo(&models.Video{URL: url, DownloadStatus: models.DLStatus{Status: tc.status}}, c.ID, c.URLModels[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.crawl
			if byUser {
				want = tc.byUser
			}
			v := &models.Video{URL: url}
			if claimed, err := vs.QueueVideo(c.ID, v, byUser); err != nil || claimed != want || v.ID != id {
				t.Errorf("%s by user %t: claimed %t, %v (ID %d); want %t (ID %d)", tc.status, byUser, claimed, err, v.ID, want, id)
			}
			if want && statusOf(id) != consts.DLStatusQueued {
				t.Errorf("%s by user %t: stored status %q, want Queued", tc.status, byUser, statusOf(id))
			}
		}
	}

	// A URL the channel doesn't have is stored as queued.
	v := &models.Video{URL: "https://example.com/v/new", ChannelURLID: c.URLModels[0].ID, Title: "New"}
	if claimed, err := vs.QueueVideo(c.ID, v, false); err != nil || !claimed || v.ID == 0 || statusOf(v.ID) != consts.DLStatusQueued {
		t.Errorf("new URL: claimed %t, %v (ID %d); want it stored as queued", claimed, err, v.ID)
	}

	// Only a video in the channel can be claimed by its ID.
	if claimed, err := vs.QueueVideo(c.ID+1, &models.Video{ID: v.ID}, true); err != nil || claimed {
		t.Errorf("other channel's claim: %t, %v; want refused", claimed, err)
	}
}

func TestNormalizeDownloadStatus(t *testing.T) {
	tests := []struct {
		name       string
		pct        float64
		status     consts.DownloadStatus
		wantPct    float64
		wantStatus consts.DownloadStatus
	}{
		{"queued kept", 0, consts.DLStatusQueued, 0, consts.DLStatusQueued},
		{"downloading kept", 42.5, consts.DLStatusDownloading, 42.5, consts.DLStatusDownloading},
		{"canceled kept", 10, consts.DLStatusCanceled, 10, consts.DLStatusCanceled},
		{"full stream stays downloading", 100, consts.DLStatusDownloading, 100, consts.DLStatusDownloading},
		{"over clamps", 120, consts.DLStatusDownloading, 100, consts.DLStatusDownloading},
		{"completed forces full", 40, consts.DLStatusCompleted, 100, consts.DLStatusCompleted},
		{"negative clamps", -5, consts.DLStatusFailed, 0, consts.DLStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct, status := tt.pct, tt.status
			normalizeDownloadStatus(&pct, &status, 1)
			if pct != tt.wantPct || status != tt.wantStatus {
				t.Errorf("got (%v, %q), want (%v, %q)", pct, status, tt.wantPct, tt.wantStatus)
			}
		})
	}
}
