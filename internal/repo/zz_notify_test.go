package repo

import (
	"testing"
	"tubarr/internal/database"
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

// TestMigrateNotificationNames tests a notifications table from older versions, with names, is rebuilt without them,
// keeping its notifications.
func TestMigrateNotificationNames(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		"DROP TABLE notifications",
		`CREATE TABLE notifications (
			id INTEGER PRIMARY KEY,
			channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			channel_url TEXT,
			notify_url TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(channel_id, notify_url)
		)`,
		"CREATE INDEX idx_notification_channel ON notifications(channel_id)",
		"CREATE INDEX idx_notification_url ON notifications(notify_url)",
	} {
		if _, err := cs.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cs.db.Exec(`INSERT INTO notifications (channel_id, name, channel_url, notify_url) VALUES
		(?, 'Plex', '', 'https://plex.example.com'),
		(?, 'Hook', 'https://example.com/@a', 'https://hook.example.com'),
		(?, 'Null URL', NULL, 'https://null.example.com')`, c.ID, c.ID, c.ID); err != nil {
		t.Fatal(err)
	}

	// Starting up again migrates the table.
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrated, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}

	got, err := migrated.GetNotifyURLs(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d notifications after migrating, want 3: %+v", len(got), got)
	}
	var names int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('notifications') WHERE name = 'name'").Scan(&names); err != nil {
		t.Fatal(err)
	}
	if names != 0 {
		t.Error("the name column is still there after migrating")
	}

	// The same notify URL can now be set for another channel URL.
	if err := migrated.AddNotifyURLs(c.ID, []*models.Notification{
		{ChannelURL: "https://example.com/@b", NotifyURL: "https://hook.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if got, err = migrated.GetNotifyURLs(c.ID); err != nil || len(got) != 4 {
		t.Errorf("got %d notifications, want 4 (%v)", len(got), err)
	}
}
