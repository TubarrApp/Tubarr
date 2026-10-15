package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCrawlLock(t *testing.T) {
	ctx, unlock, ok := TryLockCrawl(context.Background(), "chan")
	if !ok {
		t.Fatal("first lock failed")
	}
	if _, _, ok := TryLockCrawl(context.Background(), "chan"); ok {
		t.Fatal("second lock succeeded while held")
	}
	if active, _ := CrawlActivity(); len(active) != 1 {
		t.Fatal("lock not reported active")
	}

	if !StopCrawl("chan") || ctx.Err() == nil {
		t.Fatal("stop did not cancel context")
	}
	if JobParent(ctx).Err() != nil {
		t.Fatal("stop cancelled job parent")
	}
	if active, stopping := CrawlActivity(); len(active) != 0 || len(stopping) != 1 {
		t.Fatal("stopped job should be reported as stopping until it unlocks")
	}

	unlock()
	if CrawlStateActive("chan") || StopCrawl("chan") {
		t.Fatal("lock not released")
	}

	// Stale unlock must not release a newer lock.
	_, unlock2, ok := TryLockCrawl(context.Background(), "chan")
	if !ok {
		t.Fatal("relock failed")
	}
	unlock()
	if !CrawlStateActive("chan") {
		t.Fatal("stale unlock released newer lock")
	}
	unlock2()
}

// TestLockCrawlWaits tests that LockCrawl gets the lock once the running job ends, and gives up if the channel is stopped.
func TestLockCrawlWaits(t *testing.T) {
	_, unlock, _ := TryLockCrawl(context.Background(), "wait")
	got := make(chan error, 1)
	go func() {
		_, unlockWaiter, err := LockCrawl(context.Background(), "wait")
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

	_, unlock, _ = TryLockCrawl(context.Background(), "wait")
	defer unlock()
	go func() {
		_, _, err := LockCrawl(context.Background(), "wait")
		got <- err
	}()
	time.Sleep(50 * time.Millisecond)
	StopCrawl("wait")
	if err := <-got; !errors.Is(err, ErrStoppedWaiting) {
		t.Fatalf("expected ErrStoppedWaiting after a stop, got %v", err)
	}
}
