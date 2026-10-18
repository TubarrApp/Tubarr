package repo

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestMissingFilterFileDoesNotBlockChannels tests a channel whose filter file disappears still loads and can be paused,
// and doesn't stop other channels loading, while setting a missing file is still rejected.
func TestMissingFilterFileDoesNotBlockChannels(t *testing.T) {
	cs := newTestChannelStore(t)
	if _, err := cs.AddChannel(testChannel(t, "Healthy", "https://example.com/@a")); err != nil {
		t.Fatal(err)
	}
	filterFile := filepath.Join(t.TempDir(), "filters.txt")
	if err := os.WriteFile(filterFile, []byte("title:contains:cat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := testChannel(t, "Uses filter file", "https://example.com/@b")
	c.ChanSettings.FilterFile = filterFile
	id, err := cs.AddChannel(c)
	if err != nil {
		t.Fatal(err)
	}
	idStr := strconv.FormatInt(id, 10)

	if err := os.Remove(filterFile); err != nil {
		t.Fatal(err)
	}
	if chans, _, err := cs.GetAllChannels(true); err != nil || len(chans) != 2 {
		t.Fatalf("GetAllChannels = %d channels, %v; want both", len(chans), err)
	}
	if _, found, err := cs.GetChannelModel(consts.QChanID, idStr, true); !found || err != nil {
		t.Fatalf("GetChannelModel = found %v, %v; want it loaded so it can be fixed", found, err)
	}

	// Unrelated updates (e.g. pausing) work, but setting a missing path doesn't.
	if _, err := cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
		s.Paused = true
		return nil
	}); err != nil {
		t.Errorf("pausing the channel = %v, want it to work", err)
	}
	if _, err := cs.UpdateChannelSettingsJSON(consts.QChanID, idStr, func(s *models.Settings) error {
		s.FilterFile = filepath.Join(t.TempDir(), "also-missing.txt")
		return nil
	}); err == nil {
		t.Error("setting a missing filter file succeeded, want it rejected")
	}
}
