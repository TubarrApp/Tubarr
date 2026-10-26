package repo

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// addTestProfile adds a profile with the given settings.
func addTestProfile(t *testing.T, cs *ChannelStore, name string, s *models.Settings, m *models.MetarrArgs) *models.Profile {
	t.Helper()
	p := &models.Profile{Name: name, ProfileSettings: s, ProfileMetarrArgs: m}
	if err := cs.AddProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestProfileSettingsPrecedence tests that a URL's own settings win, then its profile's, its channel's, and the
// channel's profile's.
func TestProfileSettingsPrecedence(t *testing.T) {
	cs := newTestChannelStore(t)
	profileFilter := models.Filters{Field: "title", FilterType: "omits", Value: "dog", MustAny: "must"}
	channelFilter := models.Filters{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}
	chanProfile := addTestProfile(t, cs, "channel profile",
		&models.Settings{Retries: 3, CrawlFreq: 60, Filters: []models.Filters{profileFilter}},
		&models.MetarrArgs{OutputExt: "mp4", RenameStyle: "spaces"})
	urlProfile := addTestProfile(t, cs, "URL profile", &models.Settings{Retries: 5}, &models.MetarrArgs{OutputExt: "mkv"})

	c := testChannel(t, "chan", "https://example.com/@profiled", "https://example.com/@own")
	c.ProfileID = chanProfile.ID
	c.ChanSettings.Retries = 4
	c.ChanSettings.Filters = []models.Filters{channelFilter}
	c.ChanMetarrArgs.RenameStyle = "underscores"
	c.URLModels[0].ProfileID = urlProfile.ID
	c.URLModels[1].ChanURLSettings = &models.Settings{Retries: 7}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)

	loaded, _, err := cs.GetChannelModel(consts.QChanID, id, true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	type got struct {
		retries, crawlFreq int
		ext, rename        string
	}
	check := func(name string, s *models.Settings, m *models.MetarrArgs, want got) {
		t.Helper()
		if g := (got{s.Retries, s.CrawlFreq, m.OutputExt, m.RenameStyle}); g != want {
			t.Errorf("%s: got %+v, want %+v", name, g, want)
		}
	}
	check("channel", loaded.ChanSettings, loaded.ChanMetarrArgs, got{4, 60, "mp4", "underscores"})
	for _, cu := range loaded.URLModels {
		switch cu.URL {
		case "https://example.com/@profiled":
			check("URL with a profile", cu.ChanURLSettings, cu.ChanURLMetarrArgs, got{5, 60, "mkv", "underscores"})
		case "https://example.com/@own":
			check("URL with its own retries", cu.ChanURLSettings, cu.ChanURLMetarrArgs, got{7, 60, "mp4", "underscores"})
		}
	}
	if want := []models.Filters{profileFilter, channelFilter}; !reflect.DeepEqual(loaded.ChanSettings.Filters, want) {
		t.Errorf("channel filters = %+v, want the profile's then its own", loaded.ChanSettings.Filters)
	}

	// Loaded as saved, the channel has only its own settings.
	saved, _, err := cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ChanSettings.CrawlFreq != 0 || saved.ChanMetarrArgs.OutputExt != "" {
		t.Errorf("saved channel settings include its profile's: crawl frequency %d, output extension %q", saved.ChanSettings.CrawlFreq, saved.ChanMetarrArgs.OutputExt)
	}
	if saved.Profile == nil || saved.Profile.Name != "channel profile" {
		t.Errorf("channel loaded with profile %+v, want %q", saved.Profile, "channel profile")
	}
}

// TestProfileOpsAdded tests that a profile's ops are added to those of the channel or URL using it, as a URL's are to
// its channel's: the nearer op wins where both set the same thing, and the nearer move ops are checked first.
func TestProfileOpsAdded(t *testing.T) {
	cs := newTestChannelStore(t)
	dir := t.TempDir()
	catFilter := models.Filters{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}
	profileTitle := models.MetaOps{Field: "title", OpType: "prefix", OpValue: "[P] "}
	profileArtist := models.MetaOps{Field: "artist", OpType: "set", OpValue: "Profile Band"}
	channelArtist := models.MetaOps{Field: "artist", OpType: "set", OpValue: "Channel Band"}
	urlAlbum := models.MetaOps{Field: "album", OpType: "set", OpValue: "URL Album"}
	profilePrefix := models.FilenameOps{OpType: "prefix", OpValue: "[P] "}
	channelSuffix := models.FilenameOps{OpType: "suffix", OpValue: " [C]"}
	profileFiltered := models.FilteredMetaOps{Filters: []models.Filters{catFilter}, MetaOps: []models.MetaOps{{Field: "genre", OpType: "set", OpValue: "Cats"}}}
	profileMove := models.MetaFilterMoveOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}}, OutputDir: dir}
	channelMove := models.MetaFilterMoveOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "dog", MustAny: "must"}}, OutputDir: dir}

	chanProfile := addTestProfile(t, cs, "ops profile",
		&models.Settings{MetaFilterMoveOps: []models.MetaFilterMoveOps{profileMove}},
		&models.MetarrArgs{MetaOps: []models.MetaOps{profileTitle, profileArtist}, FilenameOps: []models.FilenameOps{profilePrefix}, FilteredMetaOps: []models.FilteredMetaOps{profileFiltered}})
	urlProfile := addTestProfile(t, cs, "URL ops profile", nil, &models.MetarrArgs{MetaOps: []models.MetaOps{urlAlbum}})

	c := testChannel(t, "ops chan", "https://example.com/@ops")
	c.ProfileID = chanProfile.ID
	c.ChanSettings.MetaFilterMoveOps = []models.MetaFilterMoveOps{channelMove}
	c.ChanMetarrArgs.MetaOps = []models.MetaOps{channelArtist}
	c.ChanMetarrArgs.FilenameOps = []models.FilenameOps{channelSuffix}
	c.URLModels[0].ProfileID = urlProfile.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	check := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", what, got, want)
		}
	}
	m := loaded.ChanMetarrArgs
	check("channel meta ops", m.MetaOps, []models.MetaOps{profileTitle, channelArtist})
	check("channel filename ops", m.FilenameOps, []models.FilenameOps{profilePrefix, channelSuffix})
	check("channel filtered meta ops", m.FilteredMetaOps, []models.FilteredMetaOps{profileFiltered})
	check("channel move ops", loaded.ChanSettings.MetaFilterMoveOps, []models.MetaFilterMoveOps{channelMove, profileMove})

	// The URL's profile's ops are added to the channel's, which hold the channel profile's.
	cu := loaded.URLModels[0]
	check("URL meta ops", cu.ChanURLMetarrArgs.MetaOps, []models.MetaOps{profileTitle, channelArtist, urlAlbum})
	check("URL filename ops", cu.ChanURLMetarrArgs.FilenameOps, []models.FilenameOps{profilePrefix, channelSuffix})
}

// TestChannelVideoDirFromProfile tests that a channel can use its profile's video directory, but must have one.
func TestChannelVideoDirFromProfile(t *testing.T) {
	cs := newTestChannelStore(t)
	withDir := addTestProfile(t, cs, "with folder", &models.Settings{VideoDir: filepath.Join(t.TempDir(), "{{channel_name}}")}, nil)
	without := addTestProfile(t, cs, "without folder", nil, nil)

	c := testChannel(t, "no folder", "https://example.com/@a")
	c.ChanSettings.VideoDir = ""
	c.ProfileID = without.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); !errors.Is(err, contracts.ErrInvalidChannel) {
		t.Errorf("adding a channel without a video directory, its own or its profile's: %v", err)
	}

	c = testChannel(t, "profile folder", "https://example.com/@b")
	c.ChanSettings.VideoDir = ""
	c.ProfileID = withDir.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatalf("adding a channel using its profile's video directory: %v", err)
	}
	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
	if err != nil {
		t.Fatal(err)
	}
	if s := loaded.ChanSettings; s.VideoDir != withDir.ProfileSettings.VideoDir || s.JSONDir != s.VideoDir {
		t.Errorf("video directory %q, JSON directory %q, want the profile's video directory for both", s.VideoDir, s.JSONDir)
	}

	// It can't lose the profile's video directory.
	if err := cs.SetChannelProfile(consts.QChanID, strconv.FormatInt(c.ID, 10), without.ID); err == nil {
		t.Error("switched a channel without a video directory to a profile without one")
	}
	withDir.ProfileSettings.VideoDir = ""
	if err := cs.UpdateProfile(withDir); err == nil {
		t.Error("cleared the video directory of a profile a channel uses for one")
	}
}

// TestProfileAndVideoDirChangedTogether tests that switching to a profile with a video directory and clearing the
// channel's own in one transaction works in either order.
func TestProfileAndVideoDirChangedTogether(t *testing.T) {
	for _, profileFirst := range []bool{true, false} {
		cs := newTestChannelStore(t)
		p := addTestProfile(t, cs, "with folder", &models.Settings{VideoDir: t.TempDir()}, nil)
		if err := cs.AddChannelWithDetails(testChannel(t, "chan", "https://example.com/@a"), nil, nil); err != nil {
			t.Fatal(err)
		}
		setProfile := func(tcs contracts.ChannelStore) error {
			return tcs.SetChannelProfile(consts.QChanName, "chan", p.ID)
		}
		clearVideoDir := func(tcs contracts.ChannelStore) error {
			_, err := tcs.UpdateChannelSettingsJSON(consts.QChanName, "chan", func(s *models.Settings) error {
				s.VideoDir = ""
				return nil
			})
			return err
		}
		steps := []func(contracts.ChannelStore) error{setProfile, clearVideoDir}
		if !profileFirst {
			slices.Reverse(steps)
		}

		err := cs.WithTx(func(tcs contracts.ChannelStore) error {
			for _, step := range steps {
				if err := step(tcs); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Errorf("profile first %t: %v", profileFirst, err)
		}
	}
}

// TestDeleteProfileInUse tests that a profile can't be deleted while a channel or URL uses it.
func TestDeleteProfileInUse(t *testing.T) {
	cs := newTestChannelStore(t)
	p := addTestProfile(t, cs, "shared", nil, nil)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ProfileID = p.ID
	c.URLModels[0].ProfileID = p.ID
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	err := cs.DeleteProfile(consts.QProfileName, "shared")
	if !errors.Is(err, contracts.ErrInvalidProfile) || !strings.Contains(err.Error(), `channel "chan"`) || !strings.Contains(err.Error(), "@a") {
		t.Errorf("DeleteProfile = %v, want it refused naming the channel and its URL", err)
	}

	// Once neither uses it, it's deleted.
	if err := cs.SetChannelProfile(consts.QChanName, "chan", 0); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ProfileID = 0
	if err := cs.UpdateChannelURLSettings(urls[0]); err != nil {
		t.Fatal(err)
	}
	if err := cs.DeleteProfile(consts.QProfileName, "shared"); err != nil {
		t.Errorf("deleting an unused profile: %v", err)
	}
	if _, found, err := cs.GetProfile(consts.QProfileName, "shared"); found || err != nil {
		t.Errorf("after deleting, GetProfile = found %t, %v", found, err)
	}
}

// TestProfileNamesUnique tests that each profile needs a name no other profile has.
func TestProfileNamesUnique(t *testing.T) {
	cs := newTestChannelStore(t)
	addTestProfile(t, cs, "music", nil, nil)
	other := addTestProfile(t, cs, "podcasts", nil, nil)

	for _, name := range []string{"music", " "} {
		if err := cs.AddProfile(&models.Profile{Name: name}); !errors.Is(err, contracts.ErrInvalidProfile) {
			t.Errorf("adding a profile named %q: %v", name, err)
		}
	}
	other.Name = "music"
	if err := cs.UpdateProfile(other); !errors.Is(err, contracts.ErrInvalidProfile) {
		t.Errorf("renaming a profile to another's name: %v", err)
	}
	other.Name = "talks"
	if err := cs.UpdateProfile(other); err != nil {
		t.Errorf("renaming a profile: %v", err)
	}
}

// TestUnusableProfileMarksChannel tests that a channel whose profile is missing, or whose URL's profile has invalid
// saved settings, loads with an error.
func TestUnusableProfileMarksChannel(t *testing.T) {
	cs := newTestChannelStore(t)
	p := addTestProfile(t, cs, "broken", nil, nil)
	missing := testChannel(t, "missing profile", "https://example.com/@a")
	invalid := testChannel(t, "invalid URL profile", "https://example.com/@b")
	invalid.URLModels[0].ProfileID = p.ID
	for _, c := range []*models.Channel{missing, invalid} {
		if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	// A missing profile, and one with a value a newer version stopped accepting.
	if _, err := cs.db.Exec(`UPDATE channels SET profile_id = 999 WHERE id = ?`, missing.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.db.Exec(`UPDATE profiles SET settings = ? WHERE id = ?`, `{"ytdlp_output_ext":"bogus"}`, p.ID); err != nil {
		t.Fatal(err)
	}

	for _, merged := range []bool{true, false} {
		chans, _, err := cs.GetAllChannels(merged)
		if err != nil || len(chans) != 2 {
			t.Fatalf("GetAllChannels(%t) = %d channels, %v; want both", merged, len(chans), err)
		}
		for _, c := range chans {
			if c.LoadErr == nil {
				t.Errorf("merged %t: channel %q loaded without an error", merged, c.Name)
			}
		}
	}
	if got, _, err := cs.GetProfile(consts.QProfileID, strconv.FormatInt(p.ID, 10)); err != nil || got.LoadErr == nil {
		t.Errorf("GetProfile = %v, LoadErr nil; want the profile loaded with its error so it can be fixed", err)
	}
}

// TestProfileOutputExtCheckedWithUsers tests that a profile's output container is checked against the codecs of the
// channels and URLs using it.
func TestProfileOutputExtCheckedWithUsers(t *testing.T) {
	cs := newTestChannelStore(t)
	cu := addCodecChannel(t, cs, "chan")
	webm := addTestProfile(t, cs, "webm", nil, &models.MetarrArgs{OutputExt: "webm"})
	mp4 := addTestProfile(t, cs, "mp4", nil, &models.MetarrArgs{OutputExt: "mp4"})

	if err := cs.SetChannelProfile(consts.QChanName, "chan", webm.ID); err == nil {
		t.Error("gave a channel with an h264 URL codec a webm profile")
	}
	cu.ProfileID = webm.ID
	if err := cs.UpdateChannelURLSettings(cu); err == nil {
		t.Error("gave a URL with an h264 codec a webm profile")
	}

	if err := cs.SetChannelProfile(consts.QChanName, "chan", mp4.ID); err != nil {
		t.Fatalf("giving the channel an mp4 profile: %v", err)
	}
	mp4.ProfileMetarrArgs.OutputExt = "webm"
	if err := cs.UpdateProfile(mp4); err == nil {
		t.Error("changed a used profile's output to webm under an h264 URL codec")
	}
}

// TestConfigReloadSetsProfiles tests that reloading a channel's config file sets the profiles it names for the channel
// and its URL.
func TestConfigReloadSetsProfiles(t *testing.T) {
	cs := newTestChannelStore(t)
	music := addTestProfile(t, cs, "music", nil, nil)
	podcasts := addTestProfile(t, cs, "podcasts", nil, nil)
	c := testChannel(t, "chan", "https://example.com/@a")
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	c.ChannelConfigFile = writeChannelConfig(t, strings.Join([]string{
		"profile: music",
		"url-settings:",
		"  'https://example.com/@a':",
		"    profile: podcasts",
	}, "\n")+"\n")
	if err := cs.UpdateChannelFromConfig(c); err != nil {
		t.Fatal(err)
	}
	saved, _, err := cs.GetChannelModel(consts.QChanName, "chan", false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ProfileID != music.ID || saved.URLModels[0].ProfileID != podcasts.ID {
		t.Errorf("channel profile %d and URL profile %d, want %d and %d", saved.ProfileID, saved.URLModels[0].ProfileID, music.ID, podcasts.ID)
	}

	c.ChannelConfigFile = writeChannelConfig(t, "profile: missing\n")
	if err := cs.UpdateChannelFromConfig(c); err == nil {
		t.Error("reloaded a config file naming a profile that doesn't exist")
	}
}

// TestConfigReloadSavesOwnSettings tests that reloading a channel's config file saves only its own settings, and
// leaves the channel filled in from its profile.
func TestConfigReloadSavesOwnSettings(t *testing.T) {
	cs := newTestChannelStore(t)
	p := addTestProfile(t, cs, "music", &models.Settings{Retries: 5}, &models.MetarrArgs{OutputExt: "mp4"})
	added := testChannel(t, "chan", "https://example.com/@a")
	added.ProfileID = p.ID
	if err := cs.AddChannelWithDetails(added, nil, nil); err != nil {
		t.Fatal(err)
	}
	c, _, err := cs.GetChannelModel(consts.QChanName, "chan", true)
	if err != nil {
		t.Fatal(err)
	}

	c.ChannelConfigFile = writeChannelConfig(t, "crawl-freq: 30\n")
	if err := cs.UpdateChannelFromConfig(c); err != nil {
		t.Fatal(err)
	}
	if s, m := c.ChanSettings, c.ChanMetarrArgs; s.CrawlFreq != 30 || s.Retries != 5 || m.OutputExt != "mp4" {
		t.Errorf("reloaded channel: crawl frequency %d, retries %d, output extension %q; want 30, and its profile's 5 and mp4", s.CrawlFreq, s.Retries, m.OutputExt)
	}
	saved, _, err := cs.GetChannelModel(consts.QChanName, "chan", false)
	if err != nil {
		t.Fatal(err)
	}
	if s, m := saved.ChanSettings, saved.ChanMetarrArgs; s.CrawlFreq != 30 || s.Retries != 0 || m.OutputExt != "" {
		t.Errorf("saved channel: crawl frequency %d, retries %d, output extension %q; want 30 and none of its profile's", s.CrawlFreq, s.Retries, m.OutputExt)
	}
}
