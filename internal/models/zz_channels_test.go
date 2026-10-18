package models

import (
	"slices"
	"testing"
	"time"
)

// TestDueURLs tests URLs are due on their own crawl frequency, or the channel's, and that the web scheduler uses the
// default for an unset frequency rather than every check.
func TestDueURLs(t *testing.T) {
	now := time.Now()
	ago := func(minutes int) time.Time { return now.Add(-time.Duration(minutes) * time.Minute) }
	names := func(cus []*ChannelURL) []string {
		var out []string
		for _, cu := range cus {
			out = append(out, cu.URL)
		}
		return out
	}

	c := &Channel{
		ChanSettings: &Settings{CrawlFreq: 60},
		URLModels: []*ChannelURL{
			{URL: "own-due", LastScan: ago(20), ChanURLSettings: &Settings{CrawlFreq: 15}},
			{URL: "own-waiting", LastScan: ago(20), ChanURLSettings: &Settings{CrawlFreq: 45}},
			{URL: "channel-waiting", LastScan: ago(20), ChanURLSettings: &Settings{}},
			{URL: "channel-due", LastScan: ago(61)},
			{URL: "paused", LastScan: ago(600), ChanURLSettings: &Settings{Paused: true}},
			{URL: "manual", LastScan: ago(600), IsManual: true},
		},
	}
	due, next := c.DueURLs(now, 0, true)
	if got := names(due); !slices.Equal(got, []string{"own-due", "channel-due"}) {
		t.Errorf("due URLs = %v, want own-due and channel-due", got)
	}
	if next != 25*time.Minute {
		t.Errorf("next due in %s, want 25m (own-waiting)", next)
	}
	if due, _ := c.DueURLs(now, 10*time.Minute, true); len(due) != 0 {
		t.Errorf("with 10 minutes of jitter, due URLs = %v, want none", names(due))
	}

	unset := &Channel{ChanSettings: &Settings{CrawlFreq: 0}, URLModels: []*ChannelURL{{URL: "u", LastScan: ago(5)}}}
	if due, next := unset.DueURLs(now, 0, true); len(due) != 0 || next != time.Duration(DefaultCrawlFreq-5)*time.Minute {
		t.Errorf("scheduled with no frequency: due %v, next %s; want none, by the default frequency", names(due), next)
	}
	if due, _ := unset.DueURLs(now, 0, false); len(due) != 1 {
		t.Error("terminal run with no frequency didn't crawl the URL")
	}
}
