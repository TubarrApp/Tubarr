package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCrawlLock(t *testing.T) {
	ctx, unlock, ok := TryLockCrawl(context.Background(), 1)
	if !ok {
		t.Fatal("first lock failed")
	}
	if _, _, ok := TryLockCrawl(context.Background(), 1); ok {
		t.Fatal("second lock succeeded while held")
	}
	if active, _ := CrawlActivity(); len(active) != 1 || active[0] != 1 {
		t.Fatalf("lock not reported active by channel ID, got %v", active)
	}

	if !StopQueue(1) || QueueCtx(ctx).Err() == nil {
		t.Fatal("stop queue did not cancel queue context")
	}
	if ctx.Err() != nil {
		t.Fatal("stop queue canceled active context")
	}
	select {
	case <-StopRequested(ctx):
	default:
		t.Fatal("stop queue did not signal stop requested")
	}
	if active, stopping := CrawlActivity(); len(active) != 0 || len(stopping) != 1 {
		t.Fatal("stopped job should be reported as stopping until it unlocks")
	}

	if !AbortAll(1) || ctx.Err() == nil {
		t.Fatal("abort did not cancel active context")
	}
	if JobParent(ctx).Err() != nil {
		t.Fatal("abort canceled job parent")
	}

	unlock()
	if CrawlStateActive(1) || AbortAll(1) {
		t.Fatal("lock not released")
	}

	// Stale unlock must not release a newer lock.
	_, unlock2, ok := TryLockCrawl(context.Background(), 1)
	if !ok {
		t.Fatal("relock failed")
	}
	unlock()
	if !CrawlStateActive(1) {
		t.Fatal("stale unlock released newer lock")
	}
	unlock2()
}

// TestUnlockLeavesStopOpen tests that unlocking cancels the job's contexts without signalling a stop.
func TestUnlockLeavesStopOpen(t *testing.T) {
	ctx, unlock, ok := TryLockCrawl(context.Background(), 2)
	if !ok {
		t.Fatal("TryLockCrawl failed on an unlocked channel")
	}
	unlock()
	if ctx.Err() == nil || QueueCtx(ctx).Err() == nil {
		t.Fatal("unlock did not cancel job contexts")
	}
	select {
	case <-StopRequested(ctx):
		t.Fatal("unlock signalled stop requested")
	default:
	}
}

// TestLockCrawlWaits tests that LockCrawl gets the lock once the running job ends, and gives up if the channel is stopped.
func TestLockCrawlWaits(t *testing.T) {
	_, unlock, _ := TryLockCrawl(context.Background(), 3)
	got := make(chan error, 1)
	go func() {
		_, unlockWaiter, err := LockCrawl(context.Background(), 3)
		if err == nil {
			unlockWaiter()
		}
		got <- err
	}()
	time.Sleep(50 * time.Millisecond)
	unlock()
	if err := <-got; err != nil {
		t.Fatalf("expected the lock once the job ended, got %v", err)
	}

	_, unlock, _ = TryLockCrawl(context.Background(), 3)
	defer unlock()
	go func() {
		_, _, err := LockCrawl(context.Background(), 3)
		got <- err
	}()
	time.Sleep(50 * time.Millisecond)
	StopQueue(3)
	if err := <-got; !errors.Is(err, ErrStoppedWaiting) {
		t.Fatalf("expected ErrStoppedWaiting after a stop, got %v", err)
	}
}
