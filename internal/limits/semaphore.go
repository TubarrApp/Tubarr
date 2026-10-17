package limits

import (
	"context"
	"sync"
)

// Semaphore is a counting semaphore whose limit can change while slots are held. A limit of 0 or less is unlimited.
type Semaphore struct {
	mu      sync.Mutex
	limit   int
	held    int
	changed chan struct{} // Closed and replaced when a slot frees or the limit changes, waking waiters.
}

// NewSemaphore returns a semaphore with the given limit.
func NewSemaphore(limit int) *Semaphore {
	return &Semaphore{limit: limit, changed: make(chan struct{})}
}

// SetLimit changes the limit. Holders over a lowered limit keep their slots, new acquires wait until under it.
func (s *Semaphore) SetLimit(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.limit != limit {
		s.limit = limit
		s.wake()
	}
}

// Acquire blocks until a slot is free, or ctx is done.
func (s *Semaphore) Acquire(ctx context.Context) error {
	for {
		s.mu.Lock()
		if s.limit <= 0 || s.held < s.limit {
			s.held++
			s.mu.Unlock()
			return nil
		}
		changed := s.changed
		s.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Release frees a slot taken by Acquire.
func (s *Semaphore) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held--
	s.wake()
}

// wake wakes all waiters to recheck the limit. Must hold s.mu.
func (s *Semaphore) wake() {
	close(s.changed)
	s.changed = make(chan struct{})
}
