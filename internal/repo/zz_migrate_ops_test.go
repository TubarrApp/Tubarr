package repo

import (
	"fmt"
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

// TestMigrateFilterTypeNames tests stored "notequals", "morethan" and "lessthan" filter and move op types are renamed on
// startup, leaving values of those words alone.
func TestMigrateFilterTypeNames(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.ChanSettings.Filters = []models.Filters{
		{Field: "duration", FilterType: "more-than", Value: "60", MustAny: "must"},
		{Field: "title", FilterType: "contains", Value: "morethan", MustAny: "must"},
	}
	c.ChanSettings.MetaFilterMoveOps = []models.MetaFilterMoveOps{{Field: "duration", FilterType: "less-than", Value: "600", OutputDir: t.TempDir()}}
	c.ChanMetarrArgs.FilteredMetaOps = []models.FilteredMetaOps{{
		Filters: []models.Filters{{Field: "language", FilterType: "not-equals", Value: "de", MustAny: "must"}},
		MetaOps: []models.MetaOps{{Field: "title", OpType: "suffix", OpValue: "!"}},
	}}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Store the filters as an older version did, with the migration not yet run.
	const old = `REPLACE(REPLACE(REPLACE(%[1]s, 'filter_type":"not-equals"', 'filter_type":"notequals"'), 'filter_type":"more-than"', 'filter_type":"morethan"'), 'filter_type":"less-than"', 'filter_type":"lessthan"')`
	for _, stmt := range []string{
		fmt.Sprintf("UPDATE channels SET settings = %s, metarr = %s", fmt.Sprintf(old, "settings"), fmt.Sprintf(old, "metarr")),
		`DELETE FROM settings WHERE key = 'migrated-filter-type-names'`,
	} {
		if _, err := cs.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	var stored string
	if err := cs.db.QueryRow("SELECT settings FROM channels").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, `"filter_type":"morethan"`) || !strings.Contains(stored, `"move_op_filter_type":"lessthan"`) {
		t.Fatalf("old filter types weren't stored: %s", stored)
	}

	// Starting up again migrates the filters.
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrated, err := GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := migrated.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), false)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LoadErr != nil {
		t.Fatalf("migrated channel didn't load cleanly: %v", loaded.LoadErr)
	}
	s := loaded.ChanSettings
	if s.Filters[0].FilterType != "more-than" || s.Filters[1].Value != "morethan" {
		t.Errorf("filters = %+v, want more-than and a value of \"morethan\" kept", s.Filters)
	}
	if got := s.MetaFilterMoveOps[0].FilterType; got != "less-than" {
		t.Errorf("move op filter type = %q, want less-than", got)
	}
	if got := loaded.ChanMetarrArgs.FilteredMetaOps[0].Filters[0].FilterType; got != "not-equals" {
		t.Errorf("filtered meta op filter type = %q, want not-equals", got)
	}
}
