package limits

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/network"

	"github.com/TubarrApp/gocommon/logging"
)

// ******************************** Limits ********************************************************************************************

func TestMain(m *testing.M) {
	logger.Pl = &logging.ProgramLogger{Console: io.Discard}
	os.Exit(m.Run())
}

// newTestLimiter returns an empty limiter, isolated from the package globals.
func newTestLimiter() *DomainLimiter {
	return &DomainLimiter{kind: "test", sems: make(map[string]*Semaphore)}
}

// FuzzDomainLimiter tests that concurrent acquires never exceed an unchanging limit, and that every acquire completes
// without deadlocking, even while the limit changes.
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

		// Holders over a lowered limit keep their slots, so only an unchanging limit on the URL's domain is a hard cap.
		fixedLimit := 0
		if len(changes) == 0 && network.Domain(rawURL) == network.NormalizeDomain(hostname) {
			fixedLimit = int(limit % 5)
		}

		var (
			running atomic.Int32
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
					if n := running.Add(1); fixedLimit > 0 && int(n) > fixedLimit {
						t.Errorf("%d holders with a limit of %d", n, fixedLimit)
					}
					time.Sleep(time.Microsecond)
					running.Add(-1)
					sem.Release()
				}
			})
		}
		wg.Wait()

		// Every slot is released.
		l.mu.Lock()
		for h, sem := range l.sems {
			sem.mu.Lock()
			if sem.held != 0 {
				t.Errorf("domain %q has %d slots still held after all releases", h, sem.held)
			}
			sem.mu.Unlock()
		}
		l.mu.Unlock()

		// Lookups use the same hostname form the limits are stored under.
		if h := network.Domain(rawURL); h != strings.ToLower(h) {
			t.Errorf("Domain(%q) = %q is not lowercase", rawURL, h)
		}
	})
}

// TestDomainLimiterEnforcesLimit tests that a full domain blocks until a slot is released, and other domains don't.
func TestDomainLimiterEnforcesLimit(t *testing.T) {
	l := newTestLimiter()
	l.Set("Example.com", 1)

	sem, err := l.Acquire(context.Background(), "https://www.example.com/a")
	if err != nil {
		t.Fatalf("first acquire returned %v, want a slot", err)
	}

	// Full: a second acquire waits until its context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := l.Acquire(ctx, "https://example.com/b"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire on a full domain returned %v, want context.DeadlineExceeded", err)
	}

	// Other domains are unlimited.
	acquireShort(t, l, "https://other.org/")
	acquireShort(t, l, "https://other.org/")

	// Releasing unblocks a waiter.
	got := make(chan error, 1)
	go func() {
		s, err := l.Acquire(context.Background(), "https://example.com/c")
		if err == nil {
			s.Release()
		}
		got <- err
	}()
	sem.Release()
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
	acquireShort(t, l, "https://example.com/d")
	acquireShort(t, l, "https://example.com/e")
}

// acquireShort takes a slot that must be free right now, failing rather than waiting for one.
func acquireShort(t *testing.T, l *DomainLimiter, rawURL string) *Semaphore {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sem, err := l.Acquire(ctx, rawURL)
	if err != nil {
		t.Fatalf("acquire %q returned %v, want a slot", rawURL, err)
	}
	return sem
}

// assertFull fails if rawURL's domain gives out a slot.
func assertFull(t *testing.T, l *DomainLimiter, rawURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := l.Acquire(ctx, rawURL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire on a full domain returned %v, want context.DeadlineExceeded", err)
	}
}

// TestDomainLimiterSetWakesWaiters tests that a raised or removed limit gives waiting callers a slot straight away.
func TestDomainLimiterSetWakesWaiters(t *testing.T) {
	for name, newLimit := range map[string]int{"raised": 2, "removed": 0} {
		t.Run(name, func(t *testing.T) {
			l := newTestLimiter()
			l.Set("example.com", 1)
			holder := acquireShort(t, l, "https://example.com/a")
			defer holder.Release()

			got := make(chan error, 1)
			go func() {
				s, err := l.Acquire(context.Background(), "https://example.com/b")
				if err == nil {
					s.Release()
				}
				got <- err
			}()
			waitForWaiters(t, holder, 1)

			// The first download still holds its slot, so only the new limit can let the waiter in.
			l.Set("example.com", newLimit)
			select {
			case err := <-got:
				if err != nil {
					t.Fatalf("waiter got error %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("waiter never got a slot after the limit changed")
			}
		})
	}
}

// TestDomainLimiterSetCountsHolders tests that changing or re-saving a limit never hands out extra slots.
func TestDomainLimiterSetCountsHolders(t *testing.T) {
	const rawURL = "https://example.com/a"

	t.Run("same limit saved again", func(t *testing.T) {
		l := newTestLimiter()
		l.Set("example.com", 1)
		holder := acquireShort(t, l, rawURL)
		defer holder.Release()
		l.Set("example.com", 1)
		assertFull(t, l, rawURL)
	})

	t.Run("lowered limit", func(t *testing.T) {
		l := newTestLimiter()
		l.Set("example.com", 3)
		held := []*Semaphore{acquireShort(t, l, rawURL), acquireShort(t, l, rawURL), acquireShort(t, l, rawURL)}
		l.Set("example.com", 1)

		// No new slots until the holders are under the new limit.
		held[0].Release()
		held[1].Release()
		assertFull(t, l, rawURL)
		held[2].Release()
		acquireShort(t, l, rawURL).Release()
	})

	t.Run("first limit on a busy domain", func(t *testing.T) {
		l := newTestLimiter()
		holder := acquireShort(t, l, rawURL)
		defer holder.Release()
		l.Set("example.com", 1)
		assertFull(t, l, rawURL)
	})
}

// TestGlobalLimitsWakeWaiters tests that raising or removing a global limit gives waiting callers a slot straight away.
func TestGlobalLimitsWakeWaiters(t *testing.T) {
	for name, global := range map[string]struct {
		init func(int)
		sem  *Semaphore
	}{
		"downloads":       {SetGlobalDownloadLimit, GlobalDownloads},
		"crawls":          {SetGlobalCrawlLimit, GlobalCrawls},
		"post-processing": {SetGlobalPostProcessLimit, GlobalPostProcessing},
	} {
		for change, newLimit := range map[string]int{"raised": 2, "removed": 0} {
			t.Run(name+" "+change, func(t *testing.T) {
				global.init(1)
				t.Cleanup(func() { global.init(0) })
				if err := AcquireSlot(context.Background(), global.sem, name, "holder"); err != nil {
					t.Fatal(err)
				}
				defer global.sem.Release()

				got := make(chan error, 1)
				go func() {
					err := AcquireSlot(context.Background(), global.sem, name, "waiter")
					if err == nil {
						global.sem.Release()
					}
					got <- err
				}()
				waitForWaiters(t, global.sem, 1)

				global.init(newLimit)
				select {
				case err := <-got:
					if err != nil {
						t.Fatalf("waiter got error %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("waiter never got a slot after the limit changed")
				}
			})
		}
	}
}

// TestAcquireSlotCanceled tests that a canceled wait returns the context's error and takes no slot.
func TestAcquireSlotCanceled(t *testing.T) {
	sem := NewSemaphore(1)
	if err := AcquireSlot(context.Background(), sem, "test", "holder"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := AcquireSlot(ctx, sem, "test", "waiter"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled wait returned %v, want context.DeadlineExceeded", err)
	}
	sem.Release()
	if !sem.TryAcquire() {
		t.Fatal("slot not free after the holder released and the waiter gave up")
	}
}

// TestDomainLimiterUnparseableURL tests that a URL with no usable hostname is never held up.
func TestDomainLimiterUnparseableURL(t *testing.T) {
	l := newTestLimiter()
	l.Set("example.com", 1)
	defer acquireShort(t, l, "%zz").Release()
	defer acquireShort(t, l, "%zz").Release()
}

// TestDomainLimiterConcurrentStress hammers every limiter method at once, for running under -race.
func TestDomainLimiterConcurrentStress(t *testing.T) {
	l := newTestLimiter()
	hosts := []string{"youtube.com", "rumble.com", "example.co.uk"}
	urls := []string{"https://www.youtube.com/@a", "https://rumble.com/c/b", "https://x.example.co.uk/v", "https://free.org/"}

	var wg sync.WaitGroup
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
				sem.Release()
			}
		})
	}
	wg.Wait()
}

// ******************************** Semaphore *****************************************************************************************

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
