package repo

import (
	"testing"
	"tubarr/internal/models"
)

// TestDeleteNotifyURLsByURLAndName tests notifications are deleted by URL or by name, leaving the rest.
func TestDeleteNotifyURLsByURLAndName(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, []*models.Notification{
		{NotifyURL: "https://one.example.com", Name: "one"},
		{NotifyURL: "https://two.example.com", Name: "two"},
		{NotifyURL: "https://three.example.com", Name: "three"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := cs.DeleteNotifyURLs(c.ID, []string{"https://one.example.com"}, []string{"two"}); err != nil {
		t.Fatal(err)
	}
	left, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name != "three" {
		names := make([]string, 0, len(left))
		for _, n := range left {
			names = append(names, n.Name)
		}
		t.Errorf("got notifications %v left, want [three]", names)
	}
}
