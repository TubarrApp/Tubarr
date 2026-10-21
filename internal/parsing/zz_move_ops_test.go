package parsing

import (
	"encoding/json"
	"testing"
	"tubarr/internal/models"
)

// TestParseMetaFilterMoveOps tests the filter|directory form, and that it's written back the same way.
func TestParseMetaFilterMoveOps(t *testing.T) {
	tests := []struct {
		in   string
		want models.MetaFilterMoveOps
	}{
		{"Title:contains:Cats|/home/cats", models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/home/cats"}},
		{"title:omits:dogs|/home/no dogs", models.MetaFilterMoveOps{Field: "title", FilterType: "omits", Value: "dogs", OutputDir: "/home/no dogs"}},
		{"duration:morethan:3600|/home/long", models.MetaFilterMoveOps{Field: "duration", FilterType: "morethan", Value: "3600", OutputDir: "/home/long"}},
		{"director:contains|/home/directed", models.MetaFilterMoveOps{Field: "director", FilterType: "contains", OutputDir: "/home/directed"}},
		{`title:contains:cats|C:\Videos\Cats`, models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "cats", OutputDir: `C:\Videos\Cats`}},
		{"title:contains:cats|/home/a|b", models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/home/a|b"}},
		{"https://tubesite.com/@cats|title:contains:cats|/home/cats", models.MetaFilterMoveOps{ChannelURL: "https://tubesite.com/@cats", Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/home/cats"}},
	}
	for _, tt := range tests {
		got, err := ParseMetaFilterMoveOps([]string{tt.in})
		if err != nil || len(got) != 1 || got[0] != tt.want {
			t.Errorf("ParseMetaFilterMoveOps(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
			continue
		}
		// The written form reads back the same.
		line := models.MetaFilterMoveOpsToString(got[0])
		if again, err := ParseMetaFilterMoveOps([]string{line}); err != nil || len(again) != 1 || again[0] != tt.want {
			t.Errorf("%q written as %q read back as %+v, %v", tt.in, line, again, err)
		}
	}

	for _, bad := range []string{
		"title:contains:cats",             // No directory.
		"title:cats:/home/cats",           // The old form, with no filter type.
		"title:contains:cats:extra|/home", // Too many filter parts.
		"title:morethan|/home",            // A comparison with no value.
	} {
		if _, err := ParseMetaFilterMoveOps([]string{bad}); err == nil {
			t.Errorf("ParseMetaFilterMoveOps(%q) was accepted", bad)
		}
	}
}

// TestMoveOpsOldBlob tests move ops saved before filter types still read, as contains filters.
func TestMoveOpsOldBlob(t *testing.T) {
	var s models.Settings
	if err := json.Unmarshal([]byte(`{"move_ops":[{"move_url_specific":"","move_op_field":"title","move_op_value":"cats","move_op_output_dir":"/cats"}]}`), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.MetaFilterMoveOps) != 1 {
		t.Fatalf("got %d move ops, want 1", len(s.MetaFilterMoveOps))
	}
	if f := s.MetaFilterMoveOps[0].Filter(); f.FilterType != "contains" || f.Field != "title" || f.Value != "cats" {
		t.Errorf("old move op's filter = %+v, want title contains cats", f)
	}
	if got := models.MetaFilterMoveOpsToString(s.MetaFilterMoveOps[0]); got != "title:contains:cats|/cats" {
		t.Errorf("old move op written as %q, want the new form", got)
	}
}
