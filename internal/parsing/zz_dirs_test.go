package parsing

import (
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// TestParseTemplateDateDirectives tests Metarr date tags with directives are left for Metarr, and bad ones rejected.
func TestParseTemplateDateDirectives(t *testing.T) {
	bin := t.TempDir() // Metarr tags need a metarr executable on $PATH.
	if err := os.WriteFile(filepath.Join(bin, sharedconsts.ProgramMetarr), []byte("#!/bin/sh\n"), 0o755); err != nil {
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

// TestFillOutputDirectory tests both output directory tags are swapped, in any case or spacing, leaving other tags.
func TestFillOutputDirectory(t *testing.T) {
	c := &models.Channel{ChanSettings: &models.Settings{VideoDir: "/channel-video"}, ChanMetarrArgs: &models.MetarrArgs{OutputDir: "/channel"}}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{VideoDir: "/url-video"}, ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: "/media/{{channel_name}}"}}
	tests := []struct{ dir, want string }{
		{"{{channel_url_output_directory}}/Destiny", "/media/{{channel_name}}/Destiny"},
		{"{{channel_url_output_directory}}/Destiny/", "/media/{{channel_name}}/Destiny/"},
		{"{{channel_url_output_directory}}Destiny", "/media/{{channel_name}}Destiny"},
		{"{{ Channel_URL_Output_Directory }}/{{year}}", "/media/{{channel_name}}/{{year}}"},
		{"{{channel_output_directory}}/Destiny", "/channel/Destiny"},
		{"{{ Channel_Output_Directory }}/{{year}}", "/channel/{{year}}"},
		{"/other/{{channel_name}}/Destiny", "/other/{{channel_name}}/Destiny"},
		{"/plain/dir", "/plain/dir"},
		{"/broken/{{channel_url_output_directory", "/broken/{{channel_url_output_directory"},
	}
	for _, tt := range tests {
		if got, allFilled := FillOutputDirectoryTags(tt.dir, cu, c); got != tt.want || !allFilled {
			t.Errorf("FillOutputDirectoryTags(%q) = %q, %v; want %q, true", tt.dir, got, allFilled, tt.want)
		}
	}
}

// TestFillOutputDirectoryFallback tests each tag uses the video directory without an output directory, and stays in place without either.
func TestFillOutputDirectoryFallback(t *testing.T) {
	const dir = "{{channel_output_directory}}/a/{{channel_url_output_directory}}/b"

	c := &models.Channel{ChanSettings: &models.Settings{VideoDir: "/channel-video"}, ChanMetarrArgs: &models.MetarrArgs{}}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{VideoDir: "/url-video"}, ChanURLMetarrArgs: &models.MetarrArgs{}}
	if got, allFilled := FillOutputDirectoryTags(dir, cu, c); got != "/channel-video/a//url-video/b" || !allFilled {
		t.Errorf("video directories: got %q, %v", got, allFilled)
	}

	if got, allFilled := FillOutputDirectoryTags(dir, &models.ChannelURL{}, &models.Channel{}); got != dir || allFilled {
		t.Errorf("no directories: got %q, %v; want the tags left in place and false", got, allFilled)
	}
	if got, allFilled := FillOutputDirectoryTags(dir, nil, nil); got != dir || allFilled {
		t.Errorf("nil models: got %q, %v; want the tags left in place and false", got, allFilled)
	}
}

// TestFillChannelOutputDirectory tests a channel's Metarr output directory fills the channel's tag from its video directory.
func TestFillChannelOutputDirectory(t *testing.T) {
	c := &models.Channel{
		ChanSettings:   &models.Settings{VideoDir: "/video"},
		ChanMetarrArgs: &models.MetarrArgs{OutputDir: "{{channel_output_directory}}/Processed"},
	}
	FillChannelOutputDirectory(c)
	if c.ChanMetarrArgs.OutputDir != "/video/Processed" {
		t.Errorf("got output directory %q", c.ChanMetarrArgs.OutputDir)
	}
}

// TestFillChannelURLOutputDirectory tests a channel URL's Metarr output directory fills from the channel's output directory and its own video directory.
func TestFillChannelURLOutputDirectory(t *testing.T) {
	c := &models.Channel{ChanSettings: &models.Settings{VideoDir: "/channel-video"}, ChanMetarrArgs: &models.MetarrArgs{OutputDir: "/channel"}}
	tests := []struct{ dir, want string }{
		{"{{channel_output_directory}}/Shorts", "/channel/Shorts"},
		{"{{channel_url_output_directory}}/Shorts", "/url-video/Shorts"},
		{"/plain", "/plain"},
	}
	for _, tt := range tests {
		cu := &models.ChannelURL{ChanURLSettings: &models.Settings{VideoDir: "/url-video"}, ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: tt.dir}}
		FillChannelURLOutputDirectory(cu, c)
		if cu.ChanURLMetarrArgs.OutputDir != tt.want {
			t.Errorf("%q: got %q, want %q", tt.dir, cu.ChanURLMetarrArgs.OutputDir, tt.want)
		}
	}
}

// TestParseDirectoryUnfilledOutputTag tests an output directory tag left with no directory to fill from is an error.
func TestParseDirectoryUnfilledOutputTag(t *testing.T) {
	dp := NewDirectoryParser(&models.Channel{Name: "Cats"}, &models.ChannelURL{URL: "https://example.com/@cats"}, TubarrTags)
	for _, dir := range []string{"{{channel_output_directory}}/Shorts", "{{channel_url_output_directory}}/Shorts"} {
		if got, err := dp.ParseDirectory(dir, "test"); err == nil {
			t.Errorf("ParseDirectory(%q) = %q, want an error", dir, got)
		}
	}
}
