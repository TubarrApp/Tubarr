package app

import "testing"

// TestMetarrBusy tests that only the channel's given videos queued for or running in Metarr are counted.
func TestMetarrBusy(t *testing.T) {
	inMetarr := func(videoID int64) *metarrClaim {
		claim := &metarrClaim{videoID: videoID, done: make(chan struct{})}
		claim.inMetarr.Store(true)
		return claim
	}
	claims := map[metarrKey]*metarrClaim{
		{1, "https://a.example/1"}: inMetarr(10),
		{1, "https://a.example/2"}: inMetarr(11),
		{1, "https://a.example/3"}: {videoID: 12, pipeline: true, done: make(chan struct{})}, // Still downloading.
		{2, "https://b.example/1"}: inMetarr(20),
	}
	for k, c := range claims {
		metarrClaims.Store(k, c)
	}
	t.Cleanup(func() {
		for k := range claims {
			metarrClaims.Delete(k)
		}
	})

	tests := []struct {
		name     string
		channel  int64
		videoIDs []int64
		want     int
	}{
		{"one running", 1, []int64{10}, 1},
		{"two running", 1, []int64{10, 11}, 2},
		{"not in Metarr yet", 1, []int64{12}, 0},
		{"other channel's video", 1, []int64{20}, 0},
		{"not held", 1, []int64{99}, 0},
		{"no videos given", 1, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MetarrBusy(tt.channel, tt.videoIDs); got != tt.want {
				t.Errorf("MetarrBusy(%d, %v) = %d, want %d", tt.channel, tt.videoIDs, got, tt.want)
			}
		})
	}
}
