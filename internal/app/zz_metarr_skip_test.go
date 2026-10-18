package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
)

// TestMetarrSkip tests each reason Metarr is held back for a channel URL, and that it isn't otherwise.
func TestMetarrSkip(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	c := &models.Channel{ID: 1, Name: "Cats"}

	for name, tc := range map[string]struct {
		metarrErr error
		args      models.MetarrArgs
		want      models.SkipMetarrReasons
	}{
		"runs":                       {},
		"no metarr":                  {metarrErr: errors.New("not found"), want: models.SkipMetarrNoPath},
		"meta ops file":              {args: models.MetarrArgs{MetaOpsFile: missing}, want: models.SkipMetarrMissingMetaOpsFile},
		"filename ops file":          {args: models.MetarrArgs{FilenameOpsFile: missing}, want: models.SkipMetarrMissingFilenameOpsFile},
		"filtered meta ops file":     {args: models.MetarrArgs{FilteredMetaOpsFile: missing}, want: models.SkipMetarrMissingFilteredMetaOpsFile},
		"filtered filename ops file": {args: models.MetarrArgs{FilteredFilenameOpsFile: missing}, want: models.SkipMetarrMissingFilteredFilenameOpsFile},
	} {
		cu := &models.ChannelURL{URL: "https://example.com/@cats", ChanURLMetarrArgs: &tc.args}
		skip, err := metarrSkip(tc.metarrErr, cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
		if skip.Skip != (tc.want != "") || skip.Reason != tc.want || (err != nil) != skip.Skip {
			t.Errorf("%s: metarrSkip = %+v, %v; want reason %q", name, skip, err, tc.want)
		}
	}
}

// TestRunMetarrLeavesPendingForMissingOpsFile tests Run Metarr doesn't run Metarr without a channel URL's ops file,
// leaving the video pending to try again once it's fixed.
func TestRunMetarrLeavesPendingForMissingOpsFile(t *testing.T) {
	// A metarr that answers --help, and records any real run.
	bin, dir := t.TempDir(), t.TempDir()
	ran := filepath.Join(dir, "metarr-ran")
	script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = --help ] && exit 0\ntouch %q\n", ran)
	if err := os.WriteFile(filepath.Join(bin, "metarr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := repo.InitStores(db)
	if err != nil {
		t.Fatal(err)
	}

	opsFile := filepath.Join(dir, "meta-ops.txt")
	if err := os.WriteFile(opsFile, []byte("title:set:Cats\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := &models.Channel{
		Name:           "Cats",
		ChanSettings:   &models.Settings{VideoDir: dir},
		ChanMetarrArgs: &models.MetarrArgs{MetaOpsFile: opsFile},
		URLModels:      []*models.ChannelURL{{URL: "https://example.com/@cats"}},
	}
	id, err := store.ChannelStore().AddChannel(add)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := store.ChannelStore().GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
	if err != nil {
		t.Fatal(err)
	}

	video, metaFile := filepath.Join(dir, "v.mp4"), filepath.Join(dir, "v.json")
	for _, f := range []string{video, metaFile} {
		if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, video_path, json_path, metarr_status)
		VALUES (?, ?, ?, 1, 0, 'https://example.com/watch/1', ?, ?, ?)`,
		c.ID, c.URLModels[0].ID, c.URLModels[0].URL, video, metaFile, consts.MetarrStatusPending); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(opsFile); err != nil {
		t.Fatal(err)
	}
	if err := RunMetarr(context.Background(), store, c, nil); err == nil {
		t.Error("RunMetarr with a missing meta ops file succeeded, want an error")
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("Metarr ran without the channel URL's meta ops file")
	}
	if waiting, err := store.VideoStore().GetVideosAwaitingMetarr(c.ID); err != nil || len(waiting) != 1 {
		t.Errorf("videos awaiting Metarr = %d, %v; want the video still pending", len(waiting), err)
	}
}
