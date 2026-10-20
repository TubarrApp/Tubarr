package repo

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestAddChannelKeepsCodecOnlyURLSettings tests that a URL override setting only preferred codecs is saved with the channel.
func TestAddChannelKeepsCodecOnlyURLSettings(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.URLModels[0].ChanURLSettings = &models.Settings{
		YTDLPPreferredVideoCodecs: []string{"av1"},
		YTDLPPreferredAudioCodecs: []string{"opus"},
	}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 {
		t.Fatalf("got %d URLs, want 1", len(urls))
	}
	s := urls[0].ChanURLSettings
	if !slices.Equal(s.YTDLPPreferredVideoCodecs, []string{"av1"}) || !slices.Equal(s.YTDLPPreferredAudioCodecs, []string{"opus"}) {
		t.Errorf("URL's preferred codecs not saved, got video %v, audio %v", s.YTDLPPreferredVideoCodecs, s.YTDLPPreferredAudioCodecs)
	}
}

// TestURLSettingsSavedOnlyIfLoadable tests that URL settings loading would reject are refused when saved.
func TestURLSettingsSavedOnlyIfLoadable(t *testing.T) {
	bad := &models.MetarrArgs{OutputExt: "xyz"}

	cs := newTestChannelStore(t)
	c := testChannel(t, "added", "https://example.com/@a")
	c.URLModels[0].ChanURLMetarrArgs = bad
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("AddChannelWithDetails saved a URL Metarr output extension the channel can't load")
	}

	c = testChannel(t, "updated", "https://example.com/@b")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ChanURLMetarrArgs = bad
	if err := cs.UpdateChannelURLSettings(urls[0]); err == nil {
		t.Error("UpdateChannelURLSettings saved a URL Metarr output extension the channel can't load")
	}
}

// TestURLSettingsSaveLowercasesExtensions tests that a URL's output extensions are saved lowercase.
func TestURLSettingsSaveLowercasesExtensions(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ChanURLSettings = &models.Settings{YtdlpOutputExt: "MKV"}
	urls[0].ChanURLMetarrArgs = &models.MetarrArgs{OutputExt: "MP4"}
	if err := cs.UpdateChannelURLSettings(urls[0]); err != nil {
		t.Fatal(err)
	}
	var settingsJSON, metarrJSON string
	query := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s = ?", consts.QChanURLSettings, consts.QChanURLMetarr, consts.DBChannelURLs, consts.QChanURLID)
	if err := cs.db.QueryRow(query, urls[0].ID).Scan(&settingsJSON, &metarrJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(settingsJSON, `"mkv"`) || !strings.Contains(metarrJSON, `"mp4"`) {
		t.Errorf("extensions not saved lowercase, got settings %s, Metarr args %s", settingsJSON, metarrJSON)
	}
}
