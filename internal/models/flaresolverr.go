package models

import (
	"strings"
	"sync"
	"tubarr/internal/domain/command"
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

// YtDLPArgs returns the yt-dlp arguments for using the solution, and its generation for a later Refresh.
//
// Skips any argument already in existing (e.g. a user's own --impersonate).
func (s *FlareSolverrSolution) YtDLPArgs(existing []string) (args []string, gen int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !hasArg(existing, command.UserAgent) {
		args = append(args, command.UserAgent, s.userAgent)
	}
	if !hasArg(existing, command.Impersonate) {
		args = append(args, command.Impersonate, "chrome")
	}
	return args, s.gen
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

// hasArg reports whether args contains flag, either alone or as flag=value.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}
