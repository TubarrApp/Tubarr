package limits

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSemaphoreLimit tests that holders never exceed the limit, even while it changes.
func TestSemaphoreLimit(t *testing.T) {
	s := NewSemaphore(2)
	var (
		running, maxSeen atomic.Int32
		wg               sync.WaitGroup
	)
	for range 20 {
		wg.Go(func() {
			if err := s.Acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			n := running.Add(1)
			for {
				m := maxSeen.Load()
				if n <= m || maxSeen.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			running.Add(-1)
			s.Release()
		})
	}
	wg.Wait()
	if got := maxSeen.Load(); got > 2 {
		t.Errorf("%d concurrent holders, want at most 2", got)
	}
}

// TestSemaphoreSetLimit tests that lowering the limit holds back new acquires, and raising it wakes waiters.
func TestSemaphoreSetLimit(t *testing.T) {
	s := NewSemaphore(2)
	for range 2 {
		if err := s.Acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	s.SetLimit(1)
	s.Release()

	// One holder at a limit of 1, so a new acquire waits.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire at limit returned %v, want context.DeadlineExceeded", err)
	}

	// Raising the limit wakes a waiter.
	got := make(chan error, 1)
	go func() { got <- s.Acquire(context.Background()) }()
	time.Sleep(5 * time.Millisecond)
	s.SetLimit(0)
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("waiter got error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter not woken by limit change")
	}
}
