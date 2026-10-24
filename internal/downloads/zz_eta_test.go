package downloads

import (
	"testing"
	"time"
	"tubarr/internal/domain/consts"
)

// TestETA tests download ETAs from the average rate.
func TestETA(t *testing.T) {
	const id = 9001
	now := time.Now()
	TrackETA(id, consts.DLStatusDownloading, 10, now.Add(-20*time.Second))
	TrackETA(id, consts.DLStatusDownloading, 30, now)

	// 20% in 20 seconds leaves 70% at 1% a second.
	if got := ETA(id, 30); got < 69 || got > 71 {
		t.Errorf("ETA at 30%% = %d, want about 70", got)
	}
	if got := ETA(id, 11); got != -1 {
		t.Errorf("ETA 1%% in = %d, want -1 (too early)", got)
	}

	// A drop (e.g. yt-dlp starting the audio file) restarts the estimate.
	TrackETA(id, consts.DLStatusDownloading, 2, now)
	if got := ETA(id, 2); got != -1 {
		t.Errorf("ETA after restarting = %d, want -1", got)
	}

	TrackETA(id, consts.DLStatusCompleted, 100, now)
	if got := ETA(id, 100); got != -1 {
		t.Errorf("ETA after finishing = %d, want -1", got)
	}

	// Revised size estimates dip the percentage without restarting the estimate.
	const wobbly = 9003
	TrackETA(wobbly, consts.DLStatusDownloading, 23.5, now.Add(-27*time.Second))
	for _, pct := range []float64{25.5, 24.3, 26.4, 25.5, 27.4, 26.4, 28.3, 27.3, 29.2, 28.3, 30.2, 29.1, 53.0, 52.2, 56.0} {
		TrackETA(wobbly, consts.DLStatusDownloading, pct, now)
	}
	if got := ETA(wobbly, 56); got < 35 || got > 38 {
		t.Errorf("ETA through estimate dips = %d, want about 36", got)
	}
	TrackETA(wobbly, consts.DLStatusDownloading, 2, now)
	if got := ETA(wobbly, 2); got != -1 {
		t.Errorf("ETA once the next file starts = %d, want -1", got)
	}

	// Time at 0% before bytes arrive (yt-dlp starting up) isn't counted.
	const slowStart = 9002
	TrackETA(slowStart, consts.DLStatusDownloading, 0, now.Add(-60*time.Second))
	TrackETA(slowStart, consts.DLStatusDownloading, 10, now.Add(-20*time.Second))
	if got := ETA(slowStart, 30); got < 69 || got > 71 {
		t.Errorf("ETA after a slow start = %d, want about 70", got)
	}
}
