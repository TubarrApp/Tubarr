package models

import (
	"strings"
	"sync"
)

// FlareSolverrSolution holds the user agent of a channel URL's FlareSolverr solve, for yt-dlp to use. Safe for concurrent use.
//
// The solve's cookies are in the channel URL's cookie file. Each solve has a higher generation than the last.
type FlareSolverrSolution struct {
	mu        sync.Mutex
	userAgent string
	gen       int
	resolve   func(staleGen int) (userAgent string, gen int, err error)
}

// NewFlareSolverrSolution returns a solution solved with userAgent, which resolve solves again (rewriting the cookie file).
func NewFlareSolverrSolution(userAgent string, gen int, resolve func(staleGen int) (userAgent string, gen int, err error)) *FlareSolverrSolution {
	return &FlareSolverrSolution{userAgent: userAgent, gen: gen, resolve: resolve}
}

// Current returns the solution's user agent, and its generation for a later Refresh.
func (s *FlareSolverrSolution) Current() (userAgent string, gen int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userAgent, s.gen
}

// Refresh solves through FlareSolverr again, unless the solution has already been refreshed since generation staleGen.
func (s *FlareSolverrSolution) Refresh(staleGen int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.gen > staleGen {
		return nil
	}
	userAgent, gen, err := s.resolve(staleGen)
	if err != nil {
		return err
	}
	s.userAgent, s.gen = userAgent, gen
	return nil
}

// IsCloudflareBlock reports whether yt-dlp output looks like a Cloudflare block, which a fresh solve may get past.
func IsCloudflareBlock(output string) bool {
	o := strings.ToLower(output)
	return strings.Contains(o, "cloudflare") || strings.Contains(o, "just a moment") || strings.Contains(o, "403")
}
