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
	s := NewFlareSolverrSolution("UA/1", 1, func(staleGen int) (string, int, error) {
		mu.Lock()
		defer mu.Unlock()
		solves++
		return "UA/2", staleGen + 1, nil
	})

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
	if userAgent, gen := s.Current(); userAgent != "UA/2" || gen != 2 {
		t.Errorf("expected the refreshed user agent and generation, got %q (gen %d)", userAgent, gen)
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
