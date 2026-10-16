package repo

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
	"tubarr/internal/contracts"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
)

// newTestChannelStore returns a channel store on a fresh database.
func newTestChannelStore(t *testing.T) *ChannelStore {
	t.Helper()
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Close() })
	cs, err := GetChannelStore(db.DB)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// testChannel returns a valid channel with the given URLs.
func testChannel(t *testing.T, name string, urls ...string) *models.Channel {
	t.Helper()
	c := &models.Channel{
		Name:           name,
		ChanSettings:   &models.Settings{VideoDir: t.TempDir()},
		ChanMetarrArgs: &models.MetarrArgs{},
	}
	for _, u := range urls {
		c.URLModels = append(c.URLModels, &models.ChannelURL{URL: u})
	}
	return c
}

// TestWithTxRollsBack tests that WithTx undoes every write, including nested transactions, when fn fails.
func TestWithTxRollsBack(t *testing.T) {
	cs := newTestChannelStore(t)
	errStop := errors.New("stop")

	err := cs.WithTx(func(tcs contracts.ChannelStore) error {
		id, err := tcs.AddChannel(testChannel(t, "rolled back", "https://example.com/@a"))
		if err != nil {
			return err
		}
		if err := tcs.AddNotifyURLs(id, []*models.Notification{{NotifyURL: "https://notify.example.com"}}); err != nil {
			return err
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("expected fn's error, got %v", err)
	}
	if cs.channelExists(consts.QChanName, "rolled back") {
		t.Error("channel added in a failed transaction was kept")
	}
	var notifications int
	if err := cs.db.QueryRow("SELECT COUNT(*) FROM " + consts.DBNotifications).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications != 0 {
		t.Errorf("nested transaction's notification was kept, got %d", notifications)
	}

	if err := cs.WithTx(func(tcs contracts.ChannelStore) error {
		_, err := tcs.AddChannel(testChannel(t, "committed", "https://example.com/@b"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !cs.channelExists(consts.QChanName, "committed") {
		t.Error("channel added in a successful transaction is missing")
	}
}

// TestAddChannelWithDetailsRollsBack tests that a channel isn't added when its logins can't be.
func TestAddChannelWithDetailsRollsBack(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	err := cs.AddChannelWithDetails(c, map[string]*models.ChannelAccessDetails{"https://example.com/@other": {Username: "bob"}}, nil)
	if !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Fatalf("expected ErrInvalidChannel for a login on a URL not in the channel, got %v", err)
	}
	if cs.channelExists(consts.QChanName, "chan") {
		t.Error("channel was added although its logins failed")
	}
}

// TestAddNotifyURLsFailureReleasesTx tests that a failed AddNotifyURLs rolls back, leaving the database writable.
func TestAddNotifyURLsFailureReleasesTx(t *testing.T) {
	cs := newTestChannelStore(t)
	id, err := cs.AddChannel(testChannel(t, "chan", "https://example.com/@a"))
	if err != nil {
		t.Fatal(err)
	}

	if err := cs.AddNotifyURLs(id, []*models.Notification{{NotifyURL: "https://notify.example.com"}, {}}); err == nil {
		t.Fatal("expected an error for a notification without a URL")
	}
	notifications, err := cs.GetNotifyURLs(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 0 {
		t.Errorf("failed AddNotifyURLs kept %d notifications", len(notifications))
	}
	if err := cs.UpdateLastScan(id); err != nil {
		t.Fatalf("database not writable after a failed AddNotifyURLs: %v", err)
	}
}

// TestAddAuthPerURL tests that AddAuth sets each URL's login on that URL only.
func TestAddAuthPerURL(t *testing.T) {
	cs := newTestChannelStore(t)
	id, err := cs.AddChannel(testChannel(t, "chan", "https://example.com/@a", "https://example.com/@b"))
	if err != nil {
		t.Fatal(err)
	}

	if err := cs.AddAuth(id, map[string]*models.ChannelAccessDetails{
		"https://example.com/@a": {Username: "alice", Password: "secret", LoginURL: "https://example.com/login"},
	}); err != nil {
		t.Fatal(err)
	}
	if user, _, _, err := cs.GetAuth(id, "https://example.com/@a"); err != nil || user != "alice" {
		t.Errorf("URL a: got user %q, %v, want alice", user, err)
	}
	if user, _, _, err := cs.GetAuth(id, "https://example.com/@b"); err != nil || user != "" {
		t.Errorf("URL b: got user %q, %v, want none", user, err)
	}

	err = cs.AddAuth(id, map[string]*models.ChannelAccessDetails{"https://example.com/@other": {Username: "bob"}})
	if !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Errorf("expected ErrInvalidChannel for a URL not in the channel, got %v", err)
	}
}
