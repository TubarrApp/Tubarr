package repo

import (
	"strconv"
	"strings"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestMigrateAppendOps tests stored "append" ops are renamed "suffix" on startup, leaving values of "append" alone.
func TestMigrateAppendOps(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ChanMetarrArgs = &models.MetarrArgs{
		MetaOps:     []models.MetaOps{{Field: "title", OpType: "suffix", OpValue: " [C]"}, {Field: "artist", OpType: "set", OpValue: "append"}},
		FilenameOps: []models.FilenameOps{{OpType: "suffix", OpValue: " [F]"}},
		FilteredMetaOps: []models.FilteredMetaOps{{
			Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}},
			MetaOps: []models.MetaOps{{Field: "title", OpType: "suffix", OpValue: "!"}},
		}},
	}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Store the ops as an older version did, with the migration not yet run.
	for _, stmt := range []string{
		`UPDATE channels SET metarr = REPLACE(metarr, '_op_type":"suffix"', '_op_type":"append"')`,
		`UPDATE channel_urls SET metarr = '{"metarr_filename_ops":[{"filename_op_type":"append","filename_op_value":" [U]"}]}'`,
		`DELETE FROM settings WHERE key = 'migrated-append-ops'`,
	} {
		if _, err := cs.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	// Starting up again migrates the ops.
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, table := range []string{"channels", "channel_urls"} {
		var metarr string
		if err := db.QueryRow("SELECT metarr FROM " + table).Scan(&metarr); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(metarr, `_op_type":"append"`) || !strings.Contains(metarr, `_op_type":"suffix"`) {
			t.Errorf("%s ops not migrated: %s", table, metarr)
		}
		if table == "channels" && !strings.Contains(metarr, `"meta_op_value":"append"`) {
			t.Errorf("a value of \"append\" was changed: %s", metarr)
		}
	}

	migrated, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := migrated.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.ChanMetarrArgs.FilteredMetaOps[0].MetaOps[0].OpType; got != "suffix" {
		t.Errorf("filtered meta op type = %q, want suffix", got)
	}
}
