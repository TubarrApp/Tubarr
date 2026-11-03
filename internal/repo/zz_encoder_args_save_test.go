package repo

import (
	"strconv"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// setChannelVideoArgs sets channel "chan"'s encoder args.
func setChannelVideoArgs(cs *ChannelStore, lines ...string) error {
	_, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.TranscodeVideoArgs = lines
		return nil
	})
	return err
}

// TestKnownBadEncoderArgsRefusedOnSave tests each way of saving encoder args refuses those known to be bad.
func TestKnownBadEncoderArgsRefusedOnSave(t *testing.T) {
	cs := newTestChannelStore(t)

	// A new channel, and a new channel's URL.
	c := testChannel(t, "new", "https://example.com/@new")
	c.ChanMetarrArgs.TranscodeVideoArgs = []string{"copy:-crf 20"}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("added a channel with a copy line")
	}
	c = testChannel(t, "new", "https://example.com/@new")
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoArgs: []string{"vp8:-x265-params log-level=error"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("added a channel with a URL's x265 option on VP8")
	}

	// A channel's line every URL replaces, which later URLs would still inherit.
	c = testChannel(t, "new", "https://example.com/@new")
	c.ChanMetarrArgs.TranscodeVideoArgs = []string{"copy:-crf 20"}
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoArgs: []string{"h264:-crf 20"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("added a channel with a copy line its URL replaces")
	}

	// An existing channel.
	cu := addCodecChannel(t, cs, "chan")
	if err := setChannelVideoArgs(cs, "av1:-preset medium"); err == nil {
		t.Error("saved an x264 preset on AV1")
	}
	if err := setChannelVideoArgs(cs, "av1:-preset 8"); err != nil {
		t.Errorf("refused a preset AV1 takes: %v", err)
	}

	// An existing channel's URL.
	cu.ChanURLMetarrArgs.TranscodeVideoArgs = []string{"h264:-x265-params log-level=error"}
	if err := cs.UpdateChannelURLSettings(cu); err == nil {
		t.Error("saved a URL's x265 option on H.264")
	}

	// A new URL on an existing channel.
	existing, found, err := cs.GetChannelModel(consts.QChanName, "chan", false)
	if err != nil || !found {
		t.Fatalf("GetChannelModel = found %t, %v", found, err)
	}
	if _, err := cs.AddChannelURL(existing.ID, &models.ChannelURL{
		URL:               "https://example.com/@other",
		ChanURLMetarrArgs: &models.MetarrArgs{TranscodeVideoArgs: []string{"h264:-x265-params log-level=error"}},
	}, false); err == nil {
		t.Error("added a URL with an x265 option on H.264")
	}

	// A profile a channel already uses, on one without its own args to replace the profile's.
	addCodecChannel(t, cs, "other")
	profile := addTestProfile(t, cs, "used", nil, &models.MetarrArgs{TranscodeVideoArgs: []string{"av1:-preset 8"}})
	if err := cs.SetChannelProfile(consts.QChanName, "other", profile.ID); err != nil {
		t.Fatal(err)
	}
	profile.ProfileMetarrArgs.TranscodeVideoArgs = []string{"av1:-preset p5"}
	if err := cs.UpdateProfile(profile); err == nil {
		t.Error("changed a used profile to an NVENC preset on a channel in software")
	}
}

// TestStoredBadEncoderArgsStillLoad tests that args saved before they were refused still come back from the database, to be
// fixed on the next save.
func TestStoredBadEncoderArgsStillLoad(t *testing.T) {
	cs := newTestChannelStore(t)
	id, err := cs.AddChannel(testChannel(t, "chan", "https://example.com/@a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.db.Exec(`UPDATE channels SET metarr = ? WHERE id = ?`, []byte(`{"metarr_transcode_video_args":["copy:-crf 20"]}`), id); err != nil {
		t.Fatal(err)
	}
	idStr := strconv.FormatInt(id, 10)

	// It loads, with the args there to fix.
	c, found, err := cs.GetChannelModel(consts.QChanID, idStr, true)
	if err != nil || !found {
		t.Fatalf("GetChannelModel = found %t, %v", found, err)
	}
	if c.LoadErr != nil {
		t.Errorf("LoadErr = %v", c.LoadErr)
	}
	if len(c.ChanMetarrArgs.TranscodeVideoArgs) != 1 {
		t.Errorf("args = %v, want the stored line", c.ChanMetarrArgs.TranscodeVideoArgs)
	}
	if chans, _, err := cs.GetAllChannels(true); err != nil || len(chans) != 1 || chans[0].LoadErr != nil {
		t.Errorf("GetAllChannels = %d channels, %v", len(chans), err)
	}

	// Saving Metarr args, until the line goes.
	if err := setChannelOutputExt(cs, "mp4"); err == nil {
		t.Error("saved Metarr args still holding a copy line")
	}
	if err := setChannelVideoArgs(cs); err != nil {
		t.Errorf("refused removing the copy line: %v", err)
	}
	if err := setChannelOutputExt(cs, "mp4"); err != nil {
		t.Errorf("refused Metarr args once fixed: %v", err)
	}
}

// TestProfileEncoderArgsJudgedWithChannelGPU tests a profile alone is only refused for what no GPU could change, with the rest
// judged by the channels using it, as a value bad on software encoders may suit their GPU's.
func TestProfileEncoderArgsJudgedWithChannelGPU(t *testing.T) {
	cs := newTestChannelStore(t)

	// Alone.
	if err := cs.AddProfile(&models.Profile{Name: "copy", ProfileMetarrArgs: &models.MetarrArgs{TranscodeVideoArgs: []string{"copy:-crf 20"}}}); err == nil {
		t.Error("added a profile with a copy line")
	}
	nvenc := addTestProfile(t, cs, "nvenc", nil, &models.MetarrArgs{TranscodeVideoArgs: []string{"av1:-preset p5"}})
	nvenc.ProfileMetarrArgs.TranscodeVideoArgs = []string{"copy:-crf 20"}
	if err := cs.UpdateProfile(nvenc); err == nil {
		t.Error("saved a copy line on a profile")
	}
	nvenc.ProfileMetarrArgs.TranscodeVideoArgs = []string{"av1:-preset p5"}

	// On a channel encoding in software, then one with a GPU.
	addCodecChannel(t, cs, "chan")
	if err := cs.SetChannelProfile(consts.QChanName, "chan", nvenc.ID); err == nil {
		t.Error("gave a channel in software a profile with an NVENC preset")
	}
	if _, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.TranscodeGPU = "cuda"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetChannelProfile(consts.QChanName, "chan", nvenc.ID); err != nil {
		t.Errorf("refused an NVENC preset on a channel with an NVENC GPU: %v", err)
	}

	// The channel's GPU goes while the profile's preset stays.
	if _, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.TranscodeGPU = ""
		return nil
	}); err == nil {
		t.Error("removed the GPU from under a profile's NVENC preset")
	}
}
