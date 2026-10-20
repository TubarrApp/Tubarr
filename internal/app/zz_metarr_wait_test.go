package app

import (
	"testing"
	"time"
)

// TestWaitMetarr tests shutdown waits for videos in Metarr to be released, up to the timeout, and not for ones still downloading.
func TestWaitMetarr(t *testing.T) {
	hold := func(t *testing.T, url string, inMetarr bool) (metarrKey, *metarrClaim) {
		t.Helper()
		key := metarrKey{channelID: 1, url: url}
		claim := &metarrClaim{done: make(chan struct{})}
		claim.inMetarr.Store(inMetarr)
		metarrClaims.Store(key, claim)
		t.Cleanup(func() { metarrClaims.Delete(key) })
		return key, claim
	}

	t.Run("waits for release", func(t *testing.T) {
		key, claim := hold(t, "https://example.com/v/1", true)
		go func() {
			time.Sleep(50 * time.Millisecond)
			releaseMetarr(key, claim)
		}()
		start := time.Now()
		WaitMetarr(5 * time.Second)
		select {
		case <-claim.done:
		default:
			t.Fatal("returned before the run was released")
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("waited %v after the run was released at 50ms", d)
		}
	})

	t.Run("gives up at timeout", func(t *testing.T) {
		hold(t, "https://example.com/v/2", true)
		start := time.Now()
		WaitMetarr(100 * time.Millisecond)
		if d := time.Since(start); d < 100*time.Millisecond || d > 2*time.Second {
			t.Fatalf("waited %v, want the 100ms timeout", d)
		}
	})

	t.Run("skips downloads", func(t *testing.T) {
		hold(t, "https://example.com/v/3", false)
		start := time.Now()
		WaitMetarr(5 * time.Second)
		if d := time.Since(start); d > time.Second {
			t.Fatalf("waited %v on a video not yet in Metarr", d)
		}
	})
}
