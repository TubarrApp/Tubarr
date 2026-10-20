package metadata

import (
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/models"
)

// TestParseAndStoreJSONPrecedence tests a downloaded video's title, description and date are taken by the precedence
// Metarr embeds them with, filling only those not already stored.
func TestParseAndStoreJSONPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.json")
	body := `{"title": "Short", "fulltitle": "Full", "description": "Desc", "longdescription": "Long desc",
		"upload_date": "20240105", "release_date": "2024-01-02"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	v := &models.Video{URL: "https://example.com/v/1", JSONFilePath: path}
	if valid, err := parseAndStoreJSON(v); err != nil || !valid {
		t.Fatalf("valid %t, err %v", valid, err)
	}
	if v.Title != "Full" || v.Description != "Long desc" || v.UploadDate.Format("2006-01-02") != "2024-01-02" {
		t.Errorf("got title %q, description %q, date %v", v.Title, v.Description, v.UploadDate)
	}

	v = &models.Video{URL: "https://example.com/v/1", JSONFilePath: path, Title: "Kept", Description: "Kept"}
	if _, err := parseAndStoreJSON(v); err != nil {
		t.Fatal(err)
	}
	if v.Title != "Kept" || v.Description != "Kept" {
		t.Errorf("stored values replaced: got title %q, description %q", v.Title, v.Description)
	}
}

// TestMetadataDate tests dates are taken by precedence, skipping ones that don't parse and ignoring times.
func TestMetadataDate(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    map[string]any
		want string
	}{
		{"release date first", map[string]any{"upload_date": "20240105", "release_date": "20240102"}, "2024-01-02"},
		{"unparsable skipped", map[string]any{"release_date": "soon", "upload_date": "2024-01-05"}, "2024-01-05"},
		{"creation time last", map[string]any{"creation_time": "2024-01-03T10:00:00Z"}, "2024-01-03"},
		{"none", map[string]any{"title": "x"}, "0001-01-01"},
	} {
		if got := metadataDate(tc.m).Format("2006-01-02"); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
