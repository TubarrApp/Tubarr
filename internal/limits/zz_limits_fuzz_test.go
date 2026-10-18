package limits

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/logging"
)

func TestMain(m *testing.M) {
	logger.Pl = &logging.ProgramLogger{Console: io.Discard}
	os.Exit(m.Run())
}

// newTestLimiter returns an empty limiter, isolated from the package globals.
func newTestLimiter() *DomainLimiter {
	return &DomainLimiter{kind: "test", sems: make(map[string]chan struct{})}
}

// holdTracker counts holders per semaphore, failing if any exceeds its capacity.
type holdTracker struct {
	counts sync.Map // map[chan struct{}]*atomic.Int32.
}

func (h *holdTracker) enter(t *testing.T, sem chan struct{}) {
	v, _ := h.counts.LoadOrStore(sem, new(atomic.Int32))
	if n := v.(*atomic.Int32).Add(1); int(n) > cap(sem) {
		t.Errorf("%d holders on a semaphore with capacity %d", n, cap(sem))
	}
}

func (h *holdTracker) leave(sem chan struct{}) {
	v, _ := h.counts.Load(sem)
	v.(*atomic.Int32).Add(-1)
}

// FuzzDomainLimiter tests that concurrent acquires never exceed a domain's limit, even while the limit changes, and that
// every acquire completes without deadlocking.
func FuzzDomainLimiter(f *testing.F) {
	f.Add("https://www.youtube.com/@chan", "youtube.com", uint8(1), uint8(8), []byte{0, 1, 2, 3})
	f.Add("https://rumble.com/c/x", "RUMBLE.com", uint8(3), uint8(16), []byte{5, 0, 0, 9})
	f.Add("http://[::1]:80/a", "::1", uint8(2), uint8(4), []byte{})
	f.Add("%zz", "", uint8(0), uint8(2), []byte{1})
	f.Add("https://a.b.example.co.uk/v", "example.co.uk", uint8(4), uint8(12), []byte{255, 7})
	f.Fuzz(func(t *testing.T, rawURL, hostname string, limit, workers uint8, changes []byte) {
		l := newTestLimiter()
		l.Set(hostname, int(limit%5))
		nWorkers := int(workers%16) + 1
		if len(changes) > 8 {
			changes = changes[:8]
		}

		var (
			tracker holdTracker
			wg      sync.WaitGroup
		)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Change the limit concurrently with the acquirers.
		wg.Go(func() {
			for _, c := range changes {
				if c%2 == 0 {
					l.Set(hostname, int(c%5))
				} else {
					l.Init(map[string]int{hostname: int(c % 5)})
				}
			}
		})

		for range nWorkers {
			wg.Go(func() {
				for range 3 {
					sem, err := l.Acquire(ctx, rawURL)
					if err != nil {
						t.Errorf("acquire failed (deadlock?): %v", err)
						return
					}
					if sem == nil {
						continue
					}
					tracker.enter(t, sem)
					time.Sleep(time.Microsecond)
					tracker.leave(sem)
					Release(sem)
				}
			})
		}
		wg.Wait()

		// Every slot is released, including on semaphores replaced mid-flight.
		l.mu.RLock()
		for h, sem := range l.sems {
			if len(sem) != 0 {
				t.Errorf("domain %q has %d slots still held after all releases", h, len(sem))
			}
		}
		l.mu.RUnlock()

		// Lookups use the same hostname form the limits are stored under.
		if h := Hostname(rawURL); h != strings.ToLower(h) {
			t.Errorf("Hostname(%q) = %q is not lowercase", rawURL, h)
		}
	})
}

// TestDomainLimiterEnforcesLimit tests that a full domain blocks until a slot is released, and other domains don't.
func TestDomainLimiterEnforcesLimit(t *testing.T) {
	l := newTestLimiter()
	l.Set("Example.com", 1)

	sem, err := l.Acquire(context.Background(), "https://www.example.com/a")
	if err != nil || sem == nil {
		t.Fatalf("first acquire = (%v, %v), want a slot", sem, err)
	}

	// Full: a second acquire waits until its context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := l.Acquire(ctx, "https://example.com/b"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire on a full domain returned %v, want context.DeadlineExceeded", err)
	}

	// Other domains are unlimited.
	if other, err := l.Acquire(context.Background(), "https://other.org/"); other != nil || err != nil {
		t.Fatalf("acquire on an unlimited domain = (%v, %v), want (nil, nil)", other, err)
	}

	// Releasing unblocks a waiter.
	got := make(chan error, 1)
	go func() {
		s, err := l.Acquire(context.Background(), "https://example.com/c")
		Release(s)
		got <- err
	}()
	Release(sem)
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("waiter got error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter never got a slot after release")
	}

	// Removing the limit makes the domain unlimited.
	l.Set("example.com", 0)
	if s, err := l.Acquire(context.Background(), "https://example.com/d"); s != nil || err != nil {
		t.Fatalf("acquire after removing limit = (%v, %v), want (nil, nil)", s, err)
	}
}

// TestDomainLimiterConcurrentStress hammers every limiter method at once, for running under -race.
func TestDomainLimiterConcurrentStress(t *testing.T) {
	l := newTestLimiter()
	hosts := []string{"youtube.com", "rumble.com", "example.co.uk"}
	urls := []string{"https://www.youtube.com/@a", "https://rumble.com/c/b", "https://x.example.co.uk/v", "https://free.org/"}

	var (
		tracker holdTracker
		wg      sync.WaitGroup
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := range 4 {
		wg.Go(func() {
			for j := range 200 {
				h := hosts[(i+j)%len(hosts)]
				if j%7 == 0 {
					l.Init(map[string]int{h: j%3 + 1})
				} else {
					l.Set(h, j%4)
				}
			}
		})
	}
	for i := range 32 {
		wg.Go(func() {
			for j := range 100 {
				sem, err := l.Acquire(ctx, urls[(i+j)%len(urls)])
				if err != nil {
					t.Errorf("acquire failed (deadlock?): %v", err)
					return
				}
				if sem != nil {
					tracker.enter(t, sem)
					tracker.leave(sem)
				}
				Release(sem)
			}
		})
	}
	wg.Wait()
}
