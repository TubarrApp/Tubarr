package queue

import (
	"io"
	"os"
	"testing"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// TestMain sets up the test environment for the queue package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// TestManualChannelURL tests a channel URL is found by its ID, and an ID the channel doesn't have is refused.
func TestManualChannelURL(t *testing.T) {
	c := &models.Channel{Name: "Cats", URLModels: []*models.ChannelURL{{ID: 4, URL: "https://example.com/@cats"}}}
	if cu, err := ManualChannelURL(nil, c, 4); err != nil || cu != c.URLModels[0] {
		t.Errorf("ManualChannelURL(4) = %v, %v; want the channel's URL", cu, err)
	}
	if _, err := ManualChannelURL(nil, c, 5); err == nil {
		t.Error("ManualChannelURL(5) found a URL the channel doesn't have")
	}
}

// TestLane tests channel URLs share their channel's lane unless they have a limit of their own.
func TestLane(t *testing.T) {
	var l lanes
	c := &models.Channel{ID: 1}
	inherits, own := &models.ChannelURL{ID: 10}, &models.ChannelURL{ID: 11}

	channelLane := l.lane(c, inherits, 2, 2)
	if channelLane == nil || l.lane(c, &models.ChannelURL{ID: 12}, 2, 2) != channelLane {
		t.Error("URLs with the channel's limit don't share its lane")
	}
	if ownLane := l.lane(c, own, 2, 3); ownLane == nil || ownLane == channelLane {
		t.Error("URL with a limit of its own shares the channel's lane")
	}
	if l.lane(c, inherits, 0, 0) != nil {
		t.Error("got a lane with no limit set")
	}
}

// waitFor waits until done reports true.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(time.Millisecond)
	}
}
