package models

import (
	"encoding/json"
	"testing"
)

// A metarr_args blob as the old version would have written it for:
//
//	title:date-tag:prefix:ymd
//	fulltitle:date-tag:prefix:ymd
//	all-credits:set:Kino Casino
const oldBlob = `{
  "metarr_output_ext": "mp4",
  "metarr_rename_style": "spaces",
  "metarr_meta_ops": [
    {"meta_op_channel_url":"","meta_op_field":"title","meta_op_find_string":"","meta_op_type":"date-tag","meta_op_value":"","meta_op_loc":"prefix","meta_op_date_format":"ymd"},
    {"meta_op_channel_url":"","meta_op_field":"fulltitle","meta_op_find_string":"","meta_op_type":"date-tag","meta_op_value":"","meta_op_loc":"prefix","meta_op_date_format":"ymd"},
    {"meta_op_channel_url":"","meta_op_field":"all-credits","meta_op_find_string":"","meta_op_type":"set","meta_op_value":"Kino Casino","meta_op_loc":"","meta_op_date_format":""}
  ],
  "metarr_filename_ops": [
    {"filename_op_channel_url":"","filename_op_type":"date-tag","filename_op_find_string":"","filename_op_value":"","filename_op_loc":"prefix","filename_op_date_format":"ymd"}
  ],
  "metarr_filtered_meta_ops": [
    {"Filters":[{"filter_url_specific":"","filter_field":"title","filter_type":"contains","filter_value":"cat","filter_must_any":"must"}],
     "MetaOps":[{"meta_op_channel_url":"","meta_op_field":"title","meta_op_find_string":"","meta_op_type":"prefix","meta_op_value":"[CAT] ","meta_op_loc":"","meta_op_date_format":""}],
     "FiltersMatched":false}
  ]
}`

func TestOldDBBlobStillReads(t *testing.T) {
	var args MetarrArgs
	if err := json.Unmarshal([]byte(oldBlob), &args); err != nil {
		t.Fatalf("failed to read an old metarr_args blob: %v", err)
	}

	if len(args.MetaOps) != 3 {
		t.Fatalf("read %d meta ops, want 3", len(args.MetaOps))
	}
	if args.MetaOps[0].Field != "title" || args.MetaOps[0].OpType != "date-tag" ||
		args.MetaOps[0].OpLoc != "prefix" || args.MetaOps[0].DateFormat != "ymd" {
		t.Errorf("title date-tag did not survive: %+v", args.MetaOps[0])
	}
	if args.MetaOps[1].Field != "fulltitle" {
		t.Errorf("fulltitle date-tag did not survive: %+v", args.MetaOps[1])
	}
	if args.MetaOps[2].Field != "all-credits" || args.MetaOps[2].OpValue != "Kino Casino" {
		t.Errorf("all-credits set did not survive: %+v", args.MetaOps[2])
	}

	if len(args.FilenameOps) != 1 || args.FilenameOps[0].DateFormat != "ymd" {
		t.Errorf("filename ops did not survive: %+v", args.FilenameOps)
	}

	if len(args.FilteredMetaOps) != 1 {
		t.Fatalf("read %d filtered entries, want 1", len(args.FilteredMetaOps))
	}
	fmo := args.FilteredMetaOps[0]
	if len(fmo.Filters) != 1 || fmo.Filters[0].Field != "title" || fmo.Filters[0].MustAny != "must" {
		t.Errorf("filters did not survive: %+v", fmo.Filters)
	}
	if len(fmo.MetaOps) != 1 || fmo.MetaOps[0].OpValue != "[CAT] " {
		t.Errorf("filtered meta ops did not survive: %+v", fmo.MetaOps)
	}

	// Re-marshal and confirm the operation arrays are written back in the same shape, so
	// saving a channel after reading it does not rewrite existing rows into a new format.
	out, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	var before, after map[string]json.RawMessage
	if err := json.Unmarshal([]byte(oldBlob), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &after); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"metarr_meta_ops", "metarr_filename_ops", "metarr_filtered_meta_ops"} {
		if !jsonEqual(t, before[key], after[key]) {
			t.Errorf("%s changed shape:\n old %s\n new %s", key, before[key], after[key])
		}
	}
}

// jsonEqual compares two JSON documents ignoring key order.
func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()

	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}

	an, _ := json.Marshal(av)
	bn, _ := json.Marshal(bv)
	return string(an) == string(bn)
}
