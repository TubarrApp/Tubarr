package downloads

import (
	"database/sql"
	"strings"
	"testing"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
)

// blockCheckStore is a download store with a database handle, so Execute's domain block checks run.
type blockCheckStore struct{ nilDBStore }

func (blockCheckStore) GetDB() *sql.DB { return &sql.DB{} }

// TestExecuteBlockedDomainSkipsYTDLP tests that downloads on an already blocked domain fail without running yt-dlp.
func TestExecuteBlockedDomainSkipsYTDLP(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	vars.BlockedDomainsMutex.Lock()
	origBlocked := vars.BlockedDomains
	vars.BlockedDomains = map[string]map[vars.BlockContext]time.Time{
		"blocked.example": {vars.BlockContextUnauth: time.Now()},
	}
	vars.BlockedDomainsMutex.Unlock()
	t.Cleanup(func() {
		vars.BlockedDomainsMutex.Lock()
		vars.BlockedDomains = origBlocked
		vars.BlockedDomainsMutex.Unlock()
	})

	runs := fakeYTDLP(t, "exit 1")
	opts := &Options{MaxRetries: 3}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{}}
	c := &models.Channel{ChanSettings: &models.Settings{}, ChanMetarrArgs: &models.MetarrArgs{}}
	tracker := &DownloadTracker{updates: make(chan models.StatusUpdate, 100), done: make(chan struct{})}
	url := "https://www.blocked.example/v"

	vd, err := NewVideoDownload(t.Context(), &models.Video{URL: url, VideoDir: t.TempDir()}, cu, c, blockCheckStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}
	jd, err := NewJSONDownload(t.Context(), &models.Video{URL: url, JSONDir: t.TempDir()}, cu, c, blockCheckStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}

	if err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "is blocked") {
		t.Errorf("JSON download: got error %v, want a domain block", err)
	}
	if err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "is blocked") {
		t.Errorf("video download: got error %v, want a domain block", err)
	}
	if n := countRuns(t, runs); n != 0 {
		t.Errorf("ran yt-dlp %d times on a blocked domain, want 0", n)
	}
}
