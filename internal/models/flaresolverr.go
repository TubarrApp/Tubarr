package models

import (
	"strings"
	"sync"
	"tubarr/internal/domain/logger"
)

// FlareSolverrSolve is what a FlareSolverr solve gives yt-dlp: its browser's user agent and request headers.
type FlareSolverrSolve struct {
	UserAgent string
	Headers   [][2]string // In the browser's order (nil if they couldn't be copied).
	Gen       int         // Higher for each solve.
}

// FlareSolverrSolution holds a channel URL's latest FlareSolverr solve, for yt-dlp to use. Safe for concurrent use.
//
// The solve's cookies are in the channel URL's cookie file.
type FlareSolverrSolution struct {
	mu      sync.Mutex
	solve   FlareSolverrSolve
	resolve func(staleGen int) (FlareSolverrSolve, error)
	newer   func(gen int) (solve FlareSolverrSolve, ok bool, err error)
}

// NewFlareSolverrSolution returns a solution holding solve.
//
// resolve solves again, and newer returns any newer solve made meanwhile (e.g. by Tubarr's own requests) without
// solving. Both rewrite the cookie file with the solve they return. newer may be nil.
func NewFlareSolverrSolution(solve FlareSolverrSolve, resolve func(staleGen int) (FlareSolverrSolve, error), newer func(gen int) (FlareSolverrSolve, bool, error)) *FlareSolverrSolution {
	return &FlareSolverrSolution{solve: solve, resolve: resolve, newer: newer}
}

// Current returns the latest solve, switching to any newer one made since.
func (s *FlareSolverrSolution) Current() FlareSolverrSolve {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.newer != nil {
		solve, ok, err := s.newer(s.solve.Gen)
		if err != nil {
			logger.Pl.W("Could not switch to a newer FlareSolverr solve: %v", err)
		} else if ok {
			s.solve = solve
		}
	}
	return s.solve
}

// Refresh solves through FlareSolverr again, unless the solution has already been refreshed since generation staleGen.
func (s *FlareSolverrSolution) Refresh(staleGen int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.solve.Gen > staleGen {
		return nil
	}
	solve, err := s.resolve(staleGen)
	if err != nil {
		return err
	}
	s.solve = solve
	return nil
}

// IsCloudflareBlock reports whether yt-dlp output looks like a Cloudflare block, which a fresh solve may get past.
func IsCloudflareBlock(output string) bool {
	o := strings.ToLower(output)
	return strings.Contains(o, "cloudflare") || strings.Contains(o, "just a moment") || strings.Contains(o, "403")
}
