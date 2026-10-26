package parsing

import (
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/models"
)

// TestParseTemplateDateDirectives tests Metarr date tags with directives are left for Metarr, and bad ones rejected.
func TestParseTemplateDateDirectives(t *testing.T) {
	bin := t.TempDir() // Metarr tags need a metarr executable on $PATH.
	if err := os.WriteFile(filepath.Join(bin, "metarr"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	dp := NewDirectoryParser(&models.Channel{ID: 3, Name: "Cats"}, nil, AllTags)
	got, err := dp.parseTemplate("/media/{{channel_name}}/{{year:yy}}/{{ Month : Word }}/{{day}}")
	if want := "/media/Cats/{{year:yy}}/{{month:word}}/{{day}}"; err != nil || got != want {
		t.Errorf("parseTemplate() = %q, %v; want %q", got, err, want)
	}

	for _, dir := range []string{"/media/{{year:yyy}}", "/media/{{channel_name:word}}", "/media/{{day:word}}"} {
		if got, err := dp.parseTemplate(dir); err == nil {
			t.Errorf("parseTemplate(%q) = %q, want an error", dir, got)
		}
	}

	// Metarr tags aren't allowed where only Tubarr fills tags.
	dp = NewDirectoryParser(&models.Channel{ID: 3, Name: "Cats"}, nil, TubarrTags)
	if got, err := dp.parseTemplate("/media/{{month:word}}"); err == nil {
		t.Errorf("parseTemplate() with Tubarr tags only = %q, want an error", got)
	}
}

// TestParseTemplateChannelTags tests every channel tag is filled, as a single path-safe directory level.
func TestParseTemplateChannelTags(t *testing.T) {
	c := &models.Channel{ID: 3, Name: "AC/DC"}
	cu := &models.ChannelURL{URL: "https://www.youtube.com/@acdc"}

	dp := NewDirectoryParser(c, cu, TubarrTags)
	got, err := dp.parseTemplate("/media/{{channel_name}}/{{channel_id}}/{{channel_url}}")
	if want := "/media/AC-DC/3/www.youtube.com-@acdc"; err != nil || got != want {
		t.Errorf("parseTemplate() = %q, %v; want %q", got, err, want)
	}

	// A name can't climb out of the directory.
	dp = NewDirectoryParser(&models.Channel{ID: 3, Name: ".."}, cu, TubarrTags)
	if got, err := dp.parseTemplate("/media/{{channel_name}}"); err != nil || got != "/media/--" {
		t.Errorf("parseTemplate() with name %q = %q, %v; want %q", "..", got, err, "/media/--")
	}

	// No channel URL to fill from.
	dp = NewDirectoryParser(c, nil, TubarrTags)
	if got, err := dp.parseTemplate("/media/{{channel_url}}"); err == nil {
		t.Errorf("parseTemplate() without a channel URL = %q, want an error", got)
	}
}

// TestParseTemplateChannelURLName tests a channel URL's name fills its tag, made path-safe, and the URL fills it when
// the name is blank.
func TestParseTemplateChannelURLName(t *testing.T) {
	c := &models.Channel{ID: 3, Name: "Cats"}
	for _, tc := range []struct{ name, want string }{
		{"Shorts/Clips", "/media/Shorts-Clips"},
		{"", "/media/www.youtube.com-@cats"},
	} {
		dp := NewDirectoryParser(c, &models.ChannelURL{URL: "https://www.youtube.com/@cats", Name: tc.name}, TubarrTags)
		if got, err := dp.parseTemplate("/media/{{channel_url_name}}"); err != nil || got != tc.want {
			t.Errorf("name %q: parseTemplate() = %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
