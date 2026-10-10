package models

import (
	"sync"
	"testing"
)

// TestFlareSolverrSolutionRefresh tests that concurrent refreshes after the same stale generation solve only once.
func TestFlareSolverrSolutionRefresh(t *testing.T) {
	var (
		mu     sync.Mutex
		solves int
	)
	s := NewFlareSolverrSolution(FlareSolverrSolve{UserAgent: "UA/1", Gen: 1}, func(staleGen int) (FlareSolverrSolve, error) {
		mu.Lock()
		defer mu.Unlock()
		solves++
		return FlareSolverrSolve{UserAgent: "UA/2", Gen: staleGen + 1}, nil
	}, nil)

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Refresh(1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if solves != 1 {
		t.Errorf("expected 1 solve for 5 refreshes of the same generation, got %d", solves)
	}
	if solve := s.Current(); solve.UserAgent != "UA/2" || solve.Gen != 2 {
		t.Errorf("expected the refreshed user agent and generation, got %+v", solve)
	}
}

// TestIsCloudflareBlock tests recognising yt-dlp errors caused by Cloudflare.
func TestIsCloudflareBlock(t *testing.T) {
	blocked := []string{
		"ERROR: [generic] Unable to download webpage: HTTP Error 403: Forbidden",
		"ERROR: Got HTTP Error 403 caused by Cloudflare anti-bot challenge",
		"<title>Just a moment...</title>",
	}
	for _, out := range blocked {
		if !IsCloudflareBlock(out) {
			t.Errorf("expected %q to be a Cloudflare block", out)
		}
	}
	if IsCloudflareBlock("ERROR: Unsupported URL: https://example.com") {
		t.Error("expected an unsupported URL error not to be a Cloudflare block")
	}
}

// TestFlareSolverrSolutionCurrentNewer tests that Current switches to a newer solve made meanwhile, and keeps its own otherwise.
func TestFlareSolverrSolutionCurrentNewer(t *testing.T) {
	latest := FlareSolverrSolve{UserAgent: "UA/1", Gen: 1}
	s := NewFlareSolverrSolution(latest, nil, func(gen int) (FlareSolverrSolve, bool, error) {
		return latest, latest.Gen > gen, nil
	})

	if got := s.Current(); got.Gen != 1 {
		t.Errorf("expected solve 1, got %+v", got)
	}
	latest = FlareSolverrSolve{UserAgent: "UA/2", Gen: 2}
	if got := s.Current(); got.Gen != 2 || got.UserAgent != "UA/2" {
		t.Errorf("expected to switch to the newer solve 2, got %+v", got)
	}
}
