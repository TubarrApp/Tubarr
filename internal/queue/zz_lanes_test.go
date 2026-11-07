package queue

import (
	"cmp"
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// limitsChannel returns a loaded channel with the given download and Metarr concurrency, and URLs merged with it.
func limitsChannel(id int64, loadedAt time.Time, limit int, urlLimits map[int64]int) *models.Channel {
	c := &models.Channel{
		ID:             id,
		LoadedAt:       loadedAt,
		ChanSettings:   &models.Settings{DownloadConcurrency: limit},
		ChanMetarrArgs: &models.MetarrArgs{MetarrConcurrency: limit},
	}
	for urlID, own := range urlLimits {
		merged := cmp.Or(own, limit)
		c.URLModels = append(c.URLModels, &models.ChannelURL{
			ID:                urlID,
			ChanURLSettings:   &models.Settings{DownloadConcurrency: merged},
			ChanURLMetarrArgs: &models.MetarrArgs{MetarrConcurrency: merged},
		})
	}
	return c
}

// mustAcquire takes a slot for a video of the channel URL, failing if none is free.
func mustAcquire(t *testing.T, l *lanes, channelID, channelURLID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := l.acquire(ctx, channelID, channelURLID); err != nil {
		t.Fatalf("no slot free for channel URL %d: %v", channelURLID, err)
	}
}

// takeSlot takes a slot for a video of the channel URL, held until the test ends.
func takeSlot(t *testing.T, l *lanes, channelID, channelURLID int64) {
	t.Helper()
	mustAcquire(t, l, channelID, channelURLID)
	t.Cleanup(func() { l.release(channelID, channelURLID) })
}

// assertNoSlot fails if a video of the channel URL is given a slot.
func assertNoSlot(t *testing.T, l *lanes, channelID, channelURLID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := l.acquire(ctx, channelID, channelURLID); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			l.release(channelID, channelURLID)
		}
		t.Fatalf("channel URL %d got a slot (error %v), want its lane full", channelURLID, err)
	}
}

// waitingLen returns how many videos are waiting for a slot.
func waitingLen(l *lanes) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.waiting)
}

// startWaiter starts a video of the channel URL waiting for a slot, returning a channel told when it gets one.
func startWaiter(t *testing.T, l *lanes, channelID, channelURLID int64) <-chan error {
	t.Helper()
	before := waitingLen(l)
	got := make(chan error, 1)
	go func() { got <- l.acquire(t.Context(), channelID, channelURLID) }()
	waitFor(t, func() bool { return waitingLen(l) > before || len(got) > 0 })
	if len(got) > 0 {
		t.Fatalf("video of channel URL %d got a slot straight away, want it waiting", channelURLID)
	}
	return got
}

// gotSlot fails unless the waiting video gets a slot.
func gotSlot(t *testing.T, got <-chan error) {
	t.Helper()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("waiting video got error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting video never got a slot")
	}
}

// stillWaiting fails if any of the waiting videos got a slot.
func stillWaiting(t *testing.T, waiters ...<-chan error) {
	t.Helper()
	for i, got := range waiters {
		select {
		case err := <-got:
			t.Fatalf("waiting video %d got a slot (error %v), want it still waiting", i, err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestLaneSharing tests channel URLs share their channel's lane unless they have a limit of their own.
func TestLaneSharing(t *testing.T) {
	var l lanes
	l.setLimits(1, downloadLimits(limitsChannel(1, time.Now(), 1, map[int64]int{10: 0, 11: 2})))

	takeSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 12) // A URL the channel was loaded without shares its lane too.
	takeSlot(t, &l, 1, 11)
	takeSlot(t, &l, 1, 11)
	assertNoSlot(t, &l, 1, 11)
}

// TestLaneLimitsFromSettings tests the lane limits read from a channel's download and Metarr concurrency settings.
func TestLaneLimitsFromSettings(t *testing.T) {
	c := limitsChannel(1, time.Now(), 0, map[int64]int{10: 0, 11: 3})
	if got := downloadLimits(c); got.channel != 1 || got.urls[10] != 0 || got.urls[11] != 3 {
		t.Errorf("downloadLimits = %+v, want the channel at 1, URL 10 sharing its lane and URL 11 at 3", got)
	}
	if got := metarrLimits(c); got.channel != 0 || got.urls[10] != 0 || got.urls[11] != 3 {
		t.Errorf("metarrLimits = %+v, want the channel unlimited, URL 10 sharing its lane and URL 11 at 3", got)
	}
	if got := downloadLimits(limitsChannel(1, time.Now(), 2, map[int64]int{12: 2})); got.urls[12] != 0 {
		t.Errorf("downloadLimits = %+v, want URL 12 with the channel's limit sharing its lane", got)
	}
}

// TestLaneChangedLimitStartsWaitingVideos tests a raised or removed limit gives a waiting video a slot straight away.
func TestLaneChangedLimitStartsWaitingVideos(t *testing.T) {
	loaded := time.Now()
	for name, newLimit := range map[string]int{"raised": 2, "removed": 0} {
		t.Run(name, func(t *testing.T) {
			var l lanes
			l.setLimits(1, metarrLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0})))
			takeSlot(t, &l, 1, 10)
			waiter := startWaiter(t, &l, 1, 10)

			l.setLimits(1, metarrLimits(limitsChannel(1, loaded.Add(time.Second), newLimit, map[int64]int{10: 0})))
			gotSlot(t, waiter)
		})
	}
}

// TestLaneLoweredLimitCountsRunningVideos tests a lowered limit starts no video until fewer run than it allows.
func TestLaneLoweredLimitCountsRunningVideos(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 3, nil)))
	for range 3 {
		mustAcquire(t, &l, 1, 10)
	}
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 1, nil)))

	l.release(1, 10)
	l.release(1, 10)
	assertNoSlot(t, &l, 1, 10)
	l.release(1, 10)
	mustAcquire(t, &l, 1, 10)
}

// TestLaneURLGainsOwnLimit tests a URL given its own limit takes its running video to its own lane.
func TestLaneURLGainsOwnLimit(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0, 11: 0})))
	takeSlot(t, &l, 1, 11)
	other := startWaiter(t, &l, 1, 10)
	same := startWaiter(t, &l, 1, 11)

	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 1, map[int64]int{10: 0, 11: 2})))
	gotSlot(t, other)
	gotSlot(t, same)
	assertNoSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 11)
}

// TestLaneURLLosesOwnLimit tests a URL whose own limit is removed counts its running videos in the channel's lane.
func TestLaneURLLosesOwnLimit(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0, 11: 2})))
	mustAcquire(t, &l, 1, 11)
	mustAcquire(t, &l, 1, 11)
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 1, map[int64]int{10: 0, 11: 0})))

	// Both now run in the channel's lane of 1, so its next video waits until neither does.
	assertNoSlot(t, &l, 1, 10)
	l.release(1, 11)
	assertNoSlot(t, &l, 1, 10)
	l.release(1, 11)
	mustAcquire(t, &l, 1, 10)
}

// TestLaneKeepsNewestLimits tests limits from an older model of a channel never replace newer ones.
func TestLaneKeepsNewestLimits(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 2, nil)))
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(-time.Second), 1, nil)))

	takeSlot(t, &l, 1, 10)
	takeSlot(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 10)
}

// TestLaneLimitsLeavingOutURLs tests limits from a channel loaded with only some URLs, as a crawl's is, keep the rest's.
func TestLaneLimitsLeavingOutURLs(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, map[int64]int{10: 0, 11: 0, 12: 2})))
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 3, map[int64]int{10: 0})))

	// URL 12 keeps its own limit of 2.
	takeSlot(t, &l, 1, 12)
	takeSlot(t, &l, 1, 12)
	assertNoSlot(t, &l, 1, 12)

	// URLs 10 and 11 share the channel's new limit of 3.
	takeSlot(t, &l, 1, 10)
	takeSlot(t, &l, 1, 11)
	takeSlot(t, &l, 1, 11)
	assertNoSlot(t, &l, 1, 10)
}

// TestLaneOrder tests waiting videos get slots in the order they asked, also when a raised limit frees them.
func TestLaneOrder(t *testing.T) {
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 1, nil)))
	mustAcquire(t, &l, 1, 10)
	first := startWaiter(t, &l, 1, 10)
	second := startWaiter(t, &l, 1, 10)
	third := startWaiter(t, &l, 1, 10)

	l.setLimits(1, downloadLimits(limitsChannel(1, loaded.Add(time.Second), 2, nil)))
	gotSlot(t, first)
	stillWaiting(t, second, third)

	l.release(1, 10)
	gotSlot(t, second)
	stillWaiting(t, third)
}

// TestLaneCanceledWait tests a video whose wait ends leaves the line without taking a slot.
func TestLaneCanceledWait(t *testing.T) {
	var l lanes
	l.setLimits(1, downloadLimits(limitsChannel(1, time.Now(), 1, nil)))
	mustAcquire(t, &l, 1, 10)
	assertNoSlot(t, &l, 1, 10)
	if n := waitingLen(&l); n != 0 {
		t.Fatalf("%d videos still waiting after their wait ended", n)
	}

	l.release(1, 10)
	mustAcquire(t, &l, 1, 10)
	l.release(1, 10)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.held) != 0 {
		t.Errorf("slots still held after every release: %v", l.held)
	}
}

// channelLimit returns the channel's limit the lanes hold, or -1 if they hold none.
func channelLimit(l *lanes, channelID int64) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim := l.limits[channelID]; lim != nil {
		return lim.channel
	}
	return -1
}

// abortAndWait aborts the channel's videos in the download queue and waits for them to leave it.
func abortAndWait(t *testing.T, dq *DownloadQueue, channelID int64) {
	t.Helper()
	dq.Abort(channelID)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !dq.Wait(ctx) {
		t.Fatal("aborted videos still in the queue")
	}
}

// TestAddAppliesChannelLimits tests Add gives both queues its channel's limits before its videos ask for slots.
func TestAddAppliesChannelLimits(t *testing.T) {
	store, _, c := newTestChannel(t)
	cu := c.URLModels[0]
	c.ChanMetarrArgs.MetarrConcurrency = 3
	pp := NewPostProcessingQueue(store)
	dq := NewDownloadQueue(t.Context(), store, pp)
	takeSlot(t, &dq.lanes, c.ID, cu.ID)

	if queued, err := dq.Add(c, Request{Video: &models.Video{URL: "https://example.com/v/1", ChannelURLID: cu.ID}}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want 1", len(queued), err)
	}
	if got := channelLimit(&dq.lanes, c.ID); got != 1 {
		t.Errorf("download limit after Add = %d, want the channel's 1", got)
	}
	if got := channelLimit(&pp.lanes, c.ID); got != 3 {
		t.Errorf("Metarr limit after Add = %d, want the channel's 3", got)
	}
	if entries := dq.Entries(); len(entries) != 1 || entries[0].DownloadStatus.Stage != "Waiting to start" {
		t.Errorf("entries = %+v, want the video waiting for the channel's one slot", entries)
	}
	abortAndWait(t, dq, c.ID)
}

// TestAddKeepsNewerLimits tests a channel loaded before its last save, as a long crawl's is, can't bring old limits back.
func TestAddKeepsNewerLimits(t *testing.T) {
	store, _, crawled := newTestChannel(t)
	cu := crawled.URLModels[0]
	dq := NewDownloadQueue(t.Context(), store, NewPostProcessingQueue(store))

	// Saved and applied after the crawl loaded the channel.
	id := strconv.FormatInt(crawled.ID, 10)
	if _, err := store.ChannelStore().UpdateChannelSettingsJSON(consts.QChanID, id, func(s *models.Settings) error {
		s.DownloadConcurrency = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, _, err := store.ChannelStore().GetChannelModel(consts.QChanID, id, true)
	if err != nil {
		t.Fatal(err)
	}
	dq.SetLimits(saved)
	takeSlot(t, &dq.lanes, saved.ID, cu.ID)
	takeSlot(t, &dq.lanes, saved.ID, cu.ID)

	// The crawl adds a video with its older copy.
	if queued, err := dq.Add(crawled, Request{Video: &models.Video{URL: "https://example.com/v/1", ChannelURLID: cu.ID}}); err != nil || len(queued) != 1 {
		t.Fatalf("Add = %d queued, %v; want 1", len(queued), err)
	}
	if got := channelLimit(&dq.lanes, saved.ID); got != 2 {
		t.Errorf("download limit after adding with the older copy = %d, want the saved 2", got)
	}
	abortAndWait(t, dq, saved.ID)
}

// TestLanesConcurrentStress tests limits hold and every slot is released under heavy use while limits change.
func TestLanesConcurrentStress(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Channel 1's limits never change: URLs 10 and 11 share 2 slots, and URL 12 has 3 of its own.
	var l lanes
	loaded := time.Now()
	l.setLimits(1, downloadLimits(limitsChannel(1, loaded, 2, map[int64]int{10: 0, 11: 0, 12: 3})))
	var shared, own atomic.Int32
	running := map[int64]*atomic.Int32{10: &shared, 11: &shared, 12: &own}
	limit := map[int64]int32{10: 2, 11: 2, 12: 3}

	// Channel 2's limits keep changing.
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 300 {
			l.setLimits(2, metarrLimits(limitsChannel(2, loaded.Add(time.Duration(i+1)), i%4, map[int64]int{20: 0, 21: i % 3})))
		}
	})

	for i := range 32 {
		wg.Go(func() {
			for j := range 50 {
				channelID := int64(1)
				channelURLID := int64(10 + (i+j)%3)
				if (i+j)%2 == 0 {
					channelID = 2
					channelURLID = int64(20 + (i+j)%2)
				}
				if err := l.acquire(ctx, channelID, channelURLID); err != nil {
					t.Errorf("acquire failed (deadlock?): %v", err)
					return
				}
				if channelID == 1 {
					if n := running[channelURLID].Add(1); n > limit[channelURLID] {
						t.Errorf("%d videos running in channel URL %d's lane, over its limit of %d", n, channelURLID, limit[channelURLID])
					}
					time.Sleep(time.Microsecond)
					running[channelURLID].Add(-1)
				}
				l.release(channelID, channelURLID)
			}
		})
	}
	wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.held) != 0 || len(l.waiting) != 0 {
		t.Errorf("after every release, %v slots held and %d videos waiting", l.held, len(l.waiting))
	}
}
