package limits

import (
	"context"
	"errors"
	"slices"
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

// TestSemaphoreOrder tests that waiters are given slots in the order they asked for them.
func TestSemaphoreOrder(t *testing.T) {
	s := NewSemaphore(1)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		order []int
		wg    sync.WaitGroup
	)
	for i := range 5 {
		wg.Go(func() {
			if err := s.Acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			s.Release()
		})
		waitForWaiters(t, s, i+1)
	}
	s.Release()
	wg.Wait()
	if !slices.Equal(order, []int{0, 1, 2, 3, 4}) {
		t.Errorf("slots given in the order %v, want 0 to 4", order)
	}
}

// TestSemaphoreCanceledWaiter tests that a waiter which gives up leaves the line, and the freed slot goes to the next.
func TestSemaphoreCanceledWaiter(t *testing.T) {
	s := NewSemaphore(1)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() { canceled <- s.Acquire(ctx) }()
	waitForWaiters(t, s, 1)
	next := make(chan error, 1)
	go func() { next <- s.Acquire(context.Background()) }()
	waitForWaiters(t, s, 2)

	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter got %v, want context.Canceled", err)
	}
	s.Release()
	select {
	case err := <-next:
		if err != nil {
			t.Fatalf("next waiter got error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("next waiter not given the freed slot")
	}
}

// waitForWaiters waits until n callers are waiting for a slot.
func waitForWaiters(t *testing.T, s *Semaphore, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		got := s.waiting.Len()
		s.mu.Unlock()
		if got == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d callers waiting, want %d", got, n)
		}
		time.Sleep(time.Millisecond)
	}
}
