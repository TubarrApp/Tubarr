package repo

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestReplaceInheritedLists tests that a channel's replace tick leaves out its profile's list and file, and a URL's
// leaves out the channel's but keeps its own profile's, while unticked lists are still added together.
func TestReplaceInheritedLists(t *testing.T) {
	cs := newTestChannelStore(t)
	opsFile := filepath.Join(t.TempDir(), "profile-meta-ops.txt")
	if err := os.WriteFile(opsFile, []byte("title:set:From Profile File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profileFilter := models.Filters{Field: "title", FilterType: "omits", Value: "dog", MustAny: "must"}
	channelFilter := models.Filters{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}
	profileOp := models.MetaOps{Field: "title", OpType: "prefix", OpValue: "[P] "}
	channelOp := models.MetaOps{Field: "artist", OpType: "set", OpValue: "Channel Band"}
	urlProfileOp := models.MetaOps{Field: "album", OpType: "set", OpValue: "URL Profile Album"}
	urlOp := models.MetaOps{Field: "genre", OpType: "set", OpValue: "URL Genre"}
	profileSuffix := models.FilenameOps{OpType: "suffix", OpValue: " [P]"}
	channelPrefix := models.FilenameOps{OpType: "prefix", OpValue: "[C] "}

	chanProfile := addTestProfile(t, cs, "replaced profile",
		&models.Settings{Filters: []models.Filters{profileFilter}},
		&models.MetarrArgs{MetaOps: []models.MetaOps{profileOp}, MetaOpsFile: opsFile, FilenameOps: []models.FilenameOps{profileSuffix}})
	urlProfile := addTestProfile(t, cs, "URL profile", nil, &models.MetarrArgs{MetaOps: []models.MetaOps{urlProfileOp}})

	const (
		replacing = "https://example.com/@replacing"
		emptied   = "https://example.com/@emptied"
		adding    = "https://example.com/@adding"
	)
	c := testChannel(t, "replace chan", replacing, emptied, adding)
	c.ProfileID = chanProfile.ID
	c.ChanSettings.Filters = []models.Filters{channelFilter}
	c.ChanSettings.FiltersReplaceInherited = true
	c.ChanMetarrArgs.MetaOps = []models.MetaOps{channelOp}
	c.ChanMetarrArgs.MetaOpsReplaceInherited = true
	c.ChanMetarrArgs.FilenameOps = []models.FilenameOps{channelPrefix}
	c.URLModels[0].ProfileID = urlProfile.ID
	c.URLModels[0].ChanURLMetarrArgs = &models.MetarrArgs{MetaOps: []models.MetaOps{urlOp}, MetaOpsReplaceInherited: true}
	c.URLModels[1].ChanURLMetarrArgs = &models.MetarrArgs{MetaOpsReplaceInherited: true} // Ticked with nothing else, so it must still be saved.
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)

	loaded, _, err := cs.GetChannelModel(consts.QChanID, id, true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	check := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", what, got, want)
		}
	}

	// The channel replaces its profile's filters and meta ops, file included, but still adds to its filename ops.
	check("channel filters", loaded.ChanSettings.Filters, []models.Filters{channelFilter})
	check("channel meta ops", loaded.ChanMetarrArgs.MetaOps, []models.MetaOps{channelOp})
	check("channel meta ops file", loaded.ChanMetarrArgs.MetaOpsFile, "")
	check("channel filename ops", loaded.ChanMetarrArgs.FilenameOps, []models.FilenameOps{profileSuffix, channelPrefix})

	urls := map[string]*models.ChannelURL{}
	for _, cu := range loaded.URLModels {
		urls[cu.URL] = cu
	}
	check("replacing URL meta ops", urls[replacing].ChanURLMetarrArgs.MetaOps, []models.MetaOps{urlProfileOp, urlOp})
	check("replacing URL filename ops", urls[replacing].ChanURLMetarrArgs.FilenameOps, []models.FilenameOps{profileSuffix, channelPrefix})
	check("emptied URL meta ops", len(urls[emptied].ChanURLMetarrArgs.MetaOps), 0)
	check("adding URL meta ops", urls[adding].ChanURLMetarrArgs.MetaOps, []models.MetaOps{channelOp})

	// A tick alone is saved, as the URL's own.
	saved, _, err := cs.GetChannelModel(consts.QChanID, id, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, cu := range saved.URLModels {
		if cu.URL == emptied && (cu.ChanURLMetarrArgs == nil || !cu.ChanURLMetarrArgs.MetaOpsReplaceInherited) {
			t.Errorf("a URL ticked with nothing else loads as saved with %+v, want its tick kept", cu.ChanURLMetarrArgs)
		}
	}
}
