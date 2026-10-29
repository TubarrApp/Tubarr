package repo

import (
	"testing"
	"tubarr/internal/models"
)

// TestDeleteNotifyURLsByURL tests notifications are deleted by URL, for every channel URL, leaving the rest.
func TestDeleteNotifyURLsByURL(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b")
	if err := cs.AddChannelWithDetails(c, nil, []*models.Notification{
		{NotifyURL: "https://one.example.com"},
		{ChannelURL: "https://example.com/@a", NotifyURL: "https://two.example.com"},
		{ChannelURL: "https://example.com/@b", NotifyURL: "https://two.example.com"},
		{NotifyURL: "https://three.example.com"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := cs.DeleteNotifyURLs(c.ID, []string{"https://one.example.com", "https://two.example.com"}); err != nil {
		t.Fatal(err)
	}
	left, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].NotifyURL != "https://three.example.com" {
		t.Errorf("got notifications %+v left, want only three.example.com", left)
	}
}

// TestNotifyURLForSeveralChannelURLs tests one notify URL can be set for several channel URLs, and for all of them.
func TestNotifyURLForSeveralChannelURLs(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b")
	want := []*models.Notification{
		{NotifyURL: "https://hook.example.com"},
		{ChannelURL: "https://example.com/@a", NotifyURL: "https://hook.example.com"},
		{ChannelURL: "https://example.com/@b", NotifyURL: "https://hook.example.com"},
	}
	if err := cs.AddChannelWithDetails(c, nil, want); err != nil {
		t.Fatal(err)
	}
	// Adding one again changes nothing.
	if err := cs.AddNotifyURLs(c.ID, want[1:2]); err != nil {
		t.Fatal(err)
	}

	got, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d notifications, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || (g.ChannelURL == w.ChannelURL && g.NotifyURL == w.NotifyURL)
		}
		if !found {
			t.Errorf("notification %+v is missing from %+v", w, got)
		}
	}
}
