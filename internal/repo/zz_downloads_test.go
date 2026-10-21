package repo

import (
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestMarkDownloadCanceled tests that only queued or downloading downloads are marked canceled.
func TestMarkDownloadCanceled(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	vs, ds := GetVideoStore(cs.db), GetDownloadStore(cs.db)
	for _, tc := range []struct {
		status consts.DownloadStatus
		marked bool
	}{
		{consts.DLStatusQueued, true},
		{consts.DLStatusDownloading, true},
		{consts.DLStatusCompleted, false},
		{consts.DLStatusFailed, false},
	} {
		v := &models.Video{URL: "https://example.com/v/" + string(tc.status), DownloadStatus: models.DLStatus{Status: tc.status}}
		id, err := vs.AddVideo(v, c.ID, c.URLModels[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if marked, err := ds.MarkDownloadCanceled(t.Context(), id); err != nil || marked != tc.marked {
			t.Errorf("%s: marked %t, %v; want %t", tc.status, marked, err, tc.marked)
		}

		want := tc.status
		if tc.marked {
			want = consts.DLStatusCanceled
		}
		var got consts.DownloadStatus
		if err := cs.db.QueryRow("SELECT "+consts.QDLStatus+" FROM "+consts.DBDownloads+" WHERE "+consts.QDLVidID+" = ?", id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: stored status %q, want %q", tc.status, got, want)
		}
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
