package repo

import (
	"testing"
	"tubarr/internal/domain/consts"
)

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
