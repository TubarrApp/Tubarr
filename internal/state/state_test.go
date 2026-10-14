package state

import (
	"context"
	"testing"
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
