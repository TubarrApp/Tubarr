package repo

import (
	"testing"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// addCodecChannel adds a channel whose one URL transcodes video to h264.
func addCodecChannel(t *testing.T, cs *ChannelStore, name string) *models.ChannelURL {
	t.Helper()
	c := testChannel(t, name, "https://example.com/@"+name)
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	return urls[0]
}

// setChannelOutputExt sets channel "chan"'s Metarr output extension.
func setChannelOutputExt(cs contracts.ChannelStore, ext string) error {
	_, err := cs.UpdateChannelMetarrArgsJSON(consts.QChanName, "chan", func(m *models.MetarrArgs) error {
		m.OutputExt = ext
		return nil
	})
	return err
}

// TestAddChannelRefusesCodecsContainerCantHold tests that a URL codec its channel's output container can't hold is refused.
func TestAddChannelRefusesCodecsContainerCantHold(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ChanMetarrArgs.OutputExt = "webm"
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err == nil {
		t.Error("AddChannelWithDetails saved an h264 URL codec under a webm channel")
	}
}

// TestChannelOutputExtRefusedOverURLCodecs tests that a channel output container its URLs' codecs can't go in is refused.
func TestChannelOutputExtRefusedOverURLCodecs(t *testing.T) {
	cs := newTestChannelStore(t)
	addCodecChannel(t, cs, "chan")
	if err := setChannelOutputExt(cs, "webm"); err == nil {
		t.Error("saved a webm channel output with an h264 URL codec")
	}
	if err := setChannelOutputExt(cs, "mp4"); err != nil {
		t.Errorf("refused an mp4 channel output with an h264 URL codec: %v", err)
	}
}

// TestURLCodecRefusedOverChannelOutputExt tests that a URL codec its channel's output container can't hold is refused when updated.
func TestURLCodecRefusedOverChannelOutputExt(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ChanMetarrArgs.OutputExt = "webm"
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	urls[0].ChanURLMetarrArgs = &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}
	if err := cs.UpdateChannelURLSettings(urls[0]); err == nil {
		t.Error("UpdateChannelURLSettings saved an h264 URL codec under a webm channel")
	}
}

// TestChannelAndURLChangesJudgedTogether tests that channel and URL changes saved in one transaction are checked
// once both are written, in either order.
func TestChannelAndURLChangesJudgedTogether(t *testing.T) {
	for _, channelFirst := range []bool{true, false} {
		cs := newTestChannelStore(t)
		cu := addCodecChannel(t, cs, "chan")
		err := cs.WithTx(func(tcs contracts.ChannelStore) error {
			cu.ChanURLMetarrArgs = nil
			if channelFirst {
				if err := setChannelOutputExt(tcs, "webm"); err != nil {
					return err
				}
				return tcs.UpdateChannelURLSettings(cu)
			}
			if err := tcs.UpdateChannelURLSettings(cu); err != nil {
				return err
			}
			return setChannelOutputExt(tcs, "webm")
		})
		if err != nil {
			t.Errorf("channel first %t: refused a webm channel output saved with its URL's h264 codec removed: %v", channelFirst, err)
		}
	}
}
