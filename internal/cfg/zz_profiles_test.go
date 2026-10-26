package cfg

import (
	"testing"
	"tubarr/internal/app"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
)

// addCLIProfile adds a profile with the given settings.
func addCLIProfile(t *testing.T, cs *repo.ChannelStore, name string, s *models.Settings, m *models.MetarrArgs) *models.Profile {
	t.Helper()
	p := &models.Profile{Name: name, ProfileSettings: s, ProfileMetarrArgs: m}
	if err := cs.AddProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestUpdateChannelSavesOwnSettings tests that update-channel saves the channel's own settings, not its profile's.
func TestUpdateChannelSavesOwnSettings(t *testing.T) {
	cs := newSettingsTestStore(t)
	if _, err := addSettingsChannel(t, cs, "chan", nil, nil); err != nil {
		t.Fatal(err)
	}
	p := addCLIProfile(t, cs, "music", &models.Settings{Retries: 5}, &models.MetarrArgs{OutputExt: "mp4"})
	if err := cs.SetChannelProfile(consts.QChanName, "chan", p.ID); err != nil {
		t.Fatal(err)
	}

	if err := runSettingsCmd(updateChannelSettingsCmd(cs), keys.ChannelName, "chan", []settingsKey{{key: keys.CrawlFreq, val: 45}}); err != nil {
		t.Fatal(err)
	}
	c := savedSettingsChannel(t, cs, "chan")
	if s, m := c.ChanSettings, c.ChanMetarrArgs; s.CrawlFreq != 45 || s.Retries != 0 || m.OutputExt != "" {
		t.Errorf("saved channel: crawl frequency %d, retries %d, output extension %q; want 45 and none of its profile's", s.CrawlFreq, s.Retries, m.OutputExt)
	}
}

// TestAddChannelWithProfiles tests that a channel config file chooses the channel's and its URL's profiles by name.
func TestAddChannelWithProfiles(t *testing.T) {
	cs := newSettingsTestStore(t)
	music := addCLIProfile(t, cs, "music", nil, nil)
	podcasts := addCLIProfile(t, cs, "podcasts", nil, nil)

	const url = "https://example.com/@chan"
	cfg := map[string]any{
		keys.ChannelName: "chan",
		keys.ChannelURLs: []string{url},
		keys.VideoDir:    t.TempDir(),
		keys.Profile:     "music",
		"url-settings":   map[string]any{url: map[string]any{keys.Profile: "podcasts"}},
	}
	input, err := parsing.LoadChannelInputFile(writeSettingsConfig(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AddChannelFromInput(cs, input); err != nil {
		t.Fatal(err)
	}
	c := savedSettingsChannel(t, cs, "chan")
	if c.ProfileID != music.ID || c.URLModels[0].ProfileID != podcasts.ID {
		t.Errorf("channel profile %d and URL profile %d, want %d and %d", c.ProfileID, c.URLModels[0].ProfileID, music.ID, podcasts.ID)
	}

	if _, err := addSettingsChannel(t, cs, "unknown", []settingsKey{{key: keys.Profile, val: "missing"}}, nil); err == nil {
		t.Error("added a channel with a profile that doesn't exist")
	}
}

// TestUpdateChannelProfileWithVideoDir tests that update-channel can switch to a profile with a video directory while
// clearing the channel's own, and back again, but leaves the channel with one.
func TestUpdateChannelProfileWithVideoDir(t *testing.T) {
	cs := newSettingsTestStore(t)
	if _, err := addSettingsChannel(t, cs, "chan", nil, nil); err != nil {
		t.Fatal(err)
	}
	p := addCLIProfile(t, cs, "with folder", &models.Settings{VideoDir: t.TempDir()}, nil)
	update := func(in ...settingsKey) error {
		return runSettingsCmd(updateChannelSettingsCmd(cs), keys.ChannelName, "chan", in)
	}

	if err := update(settingsKey{key: keys.Profile, val: "with folder"}, settingsKey{key: keys.VideoDir, val: ""}); err != nil {
		t.Fatalf("switching to the profile's video directory: %v", err)
	}
	if c := savedSettingsChannel(t, cs, "chan"); c.ProfileID != p.ID || c.ChanSettings.VideoDir != "" {
		t.Fatalf("channel profile %d, video directory %q; want %d and none of its own", c.ProfileID, c.ChanSettings.VideoDir, p.ID)
	}

	if err := update(settingsKey{key: keys.Profile, val: ""}); err == nil {
		t.Error("removed the profile of a channel relying on it for a video directory")
	}
	dir := t.TempDir()
	if err := update(settingsKey{key: keys.Profile, val: ""}, settingsKey{key: keys.VideoDir, val: dir}); err != nil {
		t.Fatalf("switching back to its own video directory: %v", err)
	}
	if c := savedSettingsChannel(t, cs, "chan"); c.ProfileID != 0 || c.ChanSettings.VideoDir != dir {
		t.Errorf("channel profile %d, video directory %q; want none and %q", c.ProfileID, c.ChanSettings.VideoDir, dir)
	}
}

// TestUpdateURLSettingsProfile tests that update-url-settings sets a URL's profile, and clearing all its settings
// clears it.
func TestUpdateURLSettingsProfile(t *testing.T) {
	cs := newSettingsTestStore(t)
	if _, err := addSettingsChannel(t, cs, "chan", nil, nil); err != nil {
		t.Fatal(err)
	}
	p := addCLIProfile(t, cs, "podcasts", nil, nil)

	if err := runSettingsCmd(updateChannelURLSettingsCmd(cs), keys.ChannelName, "chan", []settingsKey{{key: keys.Profile, val: "podcasts"}}); err != nil {
		t.Fatal(err)
	}
	if got := savedSettingsChannel(t, cs, "chan").URLModels[0].ProfileID; got != p.ID {
		t.Errorf("URL profile %d, want %d", got, p.ID)
	}
	if err := runSettingsCmd(updateChannelURLSettingsCmd(cs), keys.ChannelName, "chan", []settingsKey{{key: keys.ClearAllSettings, val: true}}); err != nil {
		t.Fatal(err)
	}
	if got := savedSettingsChannel(t, cs, "chan").URLModels[0].ProfileID; got != 0 {
		t.Errorf("after clearing all settings, URL profile %d, want none", got)
	}
}

// TestProfileCommands tests renaming a profile, and that profiles can't be paused.
func TestProfileCommands(t *testing.T) {
	cs := newSettingsTestStore(t)
	addCLIProfile(t, cs, "music", nil, nil)

	if err := runSettingsCmd(addProfileCmd(cs), keys.ProfileName, "paused", []settingsKey{{key: keys.Pause, val: true}}); err == nil {
		t.Error("added a paused profile")
	}
	if err := runSettingsCmd(editProfileCmd(cs), keys.ProfileName, "music", []settingsKey{{key: keys.ChangeNameTo, val: "songs"}}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := cs.GetProfile(consts.QProfileName, "songs"); !found || err != nil {
		t.Errorf("after renaming, GetProfile(songs) = found %t, %v", found, err)
	}
	if err := runSettingsCmd(deleteProfileCmd(cs), keys.ProfileName, "songs", nil); err != nil {
		t.Errorf("deleting the profile: %v", err)
	}
}
