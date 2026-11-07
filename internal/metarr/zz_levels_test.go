package metarr

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
)

// TestOpsFilesFromEveryLevel tests a channel URL loaded from the database gets the ops of every level's file, a
// profile's file losing to its channel's typed op where both set the same thing.
func TestOpsFilesFromEveryLevel(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cs, err := repo.GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	parse := func(ops ...string) []models.MetaOps {
		t.Helper()
		parsed, err := parsing.ParseMetaOps(ops)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	profile := &models.Profile{Name: "files", ProfileSettings: &models.Settings{}, ProfileMetarrArgs: &models.MetarrArgs{
		MetaOps:     parse("title:prefix:[P] "),
		MetaOpsFile: write("profile.txt", "title:set:From Profile File\nartist:set:Profile File\n"),
	}}
	if err := cs.AddProfile(profile); err != nil {
		t.Fatal(err)
	}
	c := &models.Channel{
		Name:           "Files",
		ProfileID:      profile.ID,
		ChanSettings:   &models.Settings{VideoDir: t.TempDir()},
		ChanMetarrArgs: &models.MetarrArgs{MetaOps: parse("title:set:From Channel")},
		URLModels: []*models.ChannelURL{{
			URL:               ownURL,
			ChanURLMetarrArgs: &models.MetarrArgs{MetaOpsFile: write("url.txt", "album:set:URL File\n")},
		}},
	}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	cu := loaded.URLModels[0]
	ops, err := channelURLMetaOps(cu, parsing.NewDirectoryParser(loaded, cu, parsing.TubarrTags))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"artist:set:Profile File", "title:prefix:[P] ", "title:set:From Channel", "album:set:URL File"}
	if got := getDedupedMetaOpStrings(ops); !slices.Equal(got, want) {
		t.Errorf("meta ops = %q, want %q", got, want)
	}
}
