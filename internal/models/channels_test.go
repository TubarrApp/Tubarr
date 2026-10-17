package models

import "testing"

// TestScheduledCrawlFreq tests the web scheduler uses the default for unset frequencies, not every check.
func TestScheduledCrawlFreq(t *testing.T) {
	for freq, want := range map[int]int{-1: DefaultCrawlFreq, 0: DefaultCrawlFreq, 1: 1, 90: 90} {
		c := &Channel{ChanSettings: &Settings{CrawlFreq: freq}}
		if got := c.ScheduledCrawlFreq(); got != want {
			t.Errorf("ScheduledCrawlFreq() with frequency %d = %d, want %d", freq, got, want)
		}
	}
}
