package limits

import (
	"container/list"
	"context"
	"sync"
)

// Semaphore is a counting semaphore whose limit can change while slots are held. A limit of 0 or less is unlimited.
//
// Slots are given out in the order they were asked for.
type Semaphore struct {
	mu      sync.Mutex
	limit   int
	held    int
	waiting list.List // Each waiter's channel, closed when it's given a slot.
}

// NewSemaphore returns a semaphore with the given limit.
func NewSemaphore(l int) *Semaphore {
	return &Semaphore{limit: l}
}

// SetLimit changes the limit. Holders over a lowered limit keep their slots, new acquires wait until under it.
func (s *Semaphore) SetLimit(l int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limit = l
	s.grant()
}

// Acquire blocks until it's given a slot, or ctx is done.
func (s *Semaphore) Acquire(ctx context.Context) error {
	s.mu.Lock()
	if s.waiting.Len() == 0 && s.free() {
		s.held++
		s.mu.Unlock()
		return nil
	}
	ready := make(chan struct{})
	waiter := s.waiting.PushBack(ready)
	s.mu.Unlock()

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		defer s.mu.Unlock()
		select {
		case <-ready: // Given a slot as ctx ended, so it goes to the next waiter.
			s.held--
			s.grant()
		default:
			s.waiting.Remove(waiter)
		}
		return ctx.Err()
	}
}

// Release frees a slot taken by Acquire.
func (s *Semaphore) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held--
	s.grant()
}

// free reports whether a slot is free. Must hold s.mu.
func (s *Semaphore) free() bool {
	return s.limit <= 0 || s.held < s.limit
}

// grant gives free slots to the longest waiting callers. Must hold s.mu.
func (s *Semaphore) grant() {
	for s.waiting.Len() > 0 && s.free() {
		ready, _ := s.waiting.Remove(s.waiting.Front()).(chan struct{})
		s.held++
		close(ready)
	}
}
