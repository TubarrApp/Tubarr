package state

import (
	"context"
	"testing"
)

// TestCrawlLock tests a channel only crawls once at a time, a stop cancels the crawl and shows it as stopping until it
// ends, and a stale unlock leaves a newer crawl locked.
func TestCrawlLock(t *testing.T) {
	ctx, unlock, ok := TryLockCrawl(context.Background(), 1)
	if !ok {
		t.Fatal("first lock failed")
	}
	if _, _, ok := TryLockCrawl(context.Background(), 1); ok {
		t.Fatal("second lock succeeded while held")
	}
	if active, _ := CrawlingChannels(); len(active) != 1 || active[0] != 1 {
		t.Fatalf("crawl not reported active by channel ID, got %v", active)
	}

	if !StopCrawl(1) || ctx.Err() == nil {
		t.Fatal("stop did not cancel the crawl's context")
	}
	if active, stopping := CrawlingChannels(); len(active) != 0 || len(stopping) != 1 {
		t.Fatal("stopped crawl should be reported as stopping until it unlocks")
	}

	unlock()
	if StopCrawl(1) {
		t.Fatal("lock not released")
	}

	// Stale unlock must not release a newer lock.
	_, unlock2, ok := TryLockCrawl(context.Background(), 1)
	if !ok {
		t.Fatal("relock failed")
	}
	unlock()
	if active, _ := CrawlingChannels(); len(active) != 1 {
		t.Fatal("stale unlock released newer lock")
	}
	unlock2()
}

// TestUnlockCancelsContext tests that unlocking cancels the crawl's context.
func TestUnlockCancelsContext(t *testing.T) {
	ctx, unlock, ok := TryLockCrawl(context.Background(), 2)
	if !ok {
		t.Fatal("TryLockCrawl failed on a channel not crawling")
	}
	unlock()
	if ctx.Err() == nil {
		t.Fatal("unlock did not cancel the crawl's context")
	}
}
