package parsing

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/enums"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/validation"
	"unicode/utf8"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/TubarrApp/gocommon/sharedtags"
	"github.com/spf13/viper"
)

// ******************************** Args **********************************************************************************************

// TestSplitArgs tests shell-style splitting of extra command line arguments.
func TestShellSplit(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  -t   sleep \n --retries 3 ", []string{"-t", "sleep", "--retries", "3"}},
		{`--add-headers "Referer: https://a.example"`, []string{"--add-headers", "Referer: https://a.example"}},
		{`--add-headers 'Referer: https://a.example'`, []string{"--add-headers", "Referer: https://a.example"}},
		{`--add-headers=Referer:\ x`, []string{"--add-headers=Referer: x"}},
		{`-o "%(title)s [%(id)s].%(ext)s"`, []string{"-o", "%(title)s [%(id)s].%(ext)s"}},
		{`--match-filter "title~='\\d+'"`, []string{"--match-filter", `title~='\d+'`}},
		{`"say \"hi\"" 'it''s'`, []string{`say "hi"`, "its"}},
		{`--referer ""`, []string{"--referer", ""}},
		{`a"b c"d`, []string{"ab cd"}},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := sharedparsing.ShellSplit(tt.in)
			if err != nil {
				t.Fatalf("ShellSplit(%q) error = %v", tt.in, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ShellSplit(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ******************************** Directories ***************************************************************************************

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

// ******************************** Reversed directories ******************************************************************************

// TestParseDirectoryReversedDelimiters tests a path with "}}" before its "{{" is an error, not a panic that would end
// the program.
func TestParseDirectoryReversedDelimiters(t *testing.T) {
	logger.Pl.Console = io.Discard
	dp := NewDirectoryParser(&models.Channel{Name: "Cats"}, &models.ChannelURL{URL: "https://example.com/a"}, TubarrTags)

	if got, err := dp.ParseDirectory("/tmp/{{channel_name}}", "test"); err != nil || got != "/tmp/Cats" {
		t.Errorf("valid path = %q, %v, want /tmp/Cats", got, err)
	}
	for _, dir := range []string{"/tmp/}}x{{", "/tmp/{{channel_name}}}}{{", "}}{{"} {
		if got, err := dp.ParseDirectory(dir, "test"); err == nil {
			t.Errorf("%q = %q, want an error", dir, got)
		}
	}
}

// ******************************** Fuzz **********************************************************************************************

// opSeeds are op strings in each grammar's format, to start the fuzzer from realistic inputs.
var opSeeds = []string{
	"title:date-tag:prefix:ymd",
	"fulltitle:date-tag:suffix:mdy",
	"all-credits:set:Cool Guy",
	"description:replace:old:new",
	"title:prefix:[Live] ",
	"date-tag:prefix:ymd",
	"replace-suffix:_1:",
	"https://site.example/channel|title:set:x",
	"title:contains:live:must",
	"title:omits:clip:any",
	"title:contains:dsp:/path/{{channel_name}}",
	"title:contains:live||title:set:x",
	"https://localhost:32400/library/sections/1/refresh?X-Plex-Token=abc|Plex",
	"",
	":",
	"::::",
	"\\:",
	"a:b:c:d:e:f:g",
}

// fuzzOps splits a fuzz input into a list of ops, one per line.
func fuzzOps(s string) []string {
	return strings.Split(s, "\n")
}

// FuzzParseMetaOps tests that parsing and validating meta ops never panics.
func FuzzParseMetaOps(f *testing.F) {
	for _, s := range opSeeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = ParseMetaOps(fuzzOps(s))
	})
}

// FuzzParseFilenameOps tests that parsing and validating filename ops never panics.
func FuzzParseFilenameOps(f *testing.F) {
	for _, s := range opSeeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = ParseFilenameOps(fuzzOps(s))
	})
}

// FuzzParseFilterOps tests that parsing and validating filter ops never panics.
func FuzzParseFilterOps(f *testing.F) {
	for _, s := range opSeeds {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(_ *testing.T, s string, requireMustAny bool) {
		ops, err := ParseFilterOps(fuzzOps(s), requireMustAny)
		if err == nil {
			_ = validation.ValidateFilterOps(ops)
		}
	})
}

// FuzzParseNotifications tests that parsing and validating notifications never panics.
func FuzzParseNotifications(f *testing.F) {
	for _, s := range opSeeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = ParseNotifications("", fuzzOps(s))
	})
}

// FuzzParseMetaFilterMoveOps tests that parsing and validating move ops never panics.
func FuzzParseMetaFilterMoveOps(f *testing.F) {
	for _, s := range opSeeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		ops, err := ParseMetaFilterMoveOps(fuzzOps(s))
		if err == nil {
			_ = validation.ValidateMetaFilterMoveOps(ops, true)
		}
	})
}

// FuzzParseFilteredMetaOps tests that parsing and validating filtered meta ops never panics.
func FuzzParseFilteredMetaOps(f *testing.F) {
	for _, s := range opSeeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = ParseFilteredMetaOps(fuzzOps(s))
	})
}

// FuzzParseFilteredFilenameOps tests that parsing and validating filtered filename ops never panics.
func FuzzParseFilteredFilenameOps(f *testing.F) {
	for _, s := range opSeeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = ParseFilteredFilenameOps(fuzzOps(s))
	})
}

// channelConfigSeeds are channel config files in each supported format.
var channelConfigSeeds = []struct{ ext, content string }{
	{".yaml", "channel_name: \"Cool\"\nchannel_urls:\n  - \"https://www.tubesite.com/@Cool\"\nvideo_directory: \"x\"\nurl-settings:\n  \"https://www.tubesite.com/@Cool\":\n    settings:\n      crawl_freq: 60\n    metarr:\n      metarr_output_directory: \"y\"\n"},
	{".toml", "channel_name = \"Cool\"\nchannel_urls = [\"https://www.tubesite.com/@Cool\"]\nvideo_directory = \"x\"\nmetarr_meta_ops = [\"title:date-tag:prefix:ymd\"]\n"},
	{".json", `{"channel_name":"Cool","channel_urls":["https://www.tubesite.com/@Cool"],"video_directory":"x","crawl_freq":30,"notify":["https://x.example"]}`},
}

// urlPattern matches URLs, whose slashes are part of the address rather than a file path.
var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'\[\]{},|]*`)

// pathOutsideDir reports whether s could name a path outside the working directory: a slash outside a URL (an absolute
// or nested path), or ".." anywhere.
func pathOutsideDir(s string) bool {
	return strings.Contains(s, "..") || strings.Contains(urlPattern.ReplaceAllString(s, ""), "/")
}

// FuzzChannelConfig tests that loading any channel config file content never panics, and that a channel is only built
// when it has the required fields.
func FuzzChannelConfig(f *testing.F) {
	for i, s := range channelConfigSeeds {
		f.Add(i, s.content)
	}
	f.Fuzz(func(t *testing.T, format int, content string) {
		// Directory settings are created when validated, so only allow paths inside a temporary working directory.
		if pathOutsideDir(content) {
			return
		}
		t.Chdir(t.TempDir())

		ext := channelConfigSeeds[((format%3)+3)%3].ext
		p := filepath.Join(t.TempDir(), "channel"+ext)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		v := viper.New()
		v.SetConfigFile(p)
		if err := v.ReadInConfig(); err != nil {
			return
		}
		var in models.ChannelInput
		if err := LoadViperIntoStruct(v, &in); err != nil {
			return
		}
		us, err := ParseURLSettingsFromViper(v)
		if err != nil {
			return
		}
		in.URLSettings = us

		c, _, err := BuildChannelFromInput(in)
		if err != nil {
			return
		}
		if c == nil {
			t.Fatalf("BuildChannelFromInput returned no channel and no error for %q", content)
		}
		if c.Name == "" {
			t.Errorf("built a channel with no name from %q", content)
		}
	})
}

// FuzzScrapeRulesSave tests that saving any scrape rules content never panics, never changes the existing file when
// the content is rejected, and never leaves temporary files behind.
func FuzzScrapeRulesSave(f *testing.F) {
	for _, s := range []string{
		"[[sites]]\ndomain = \"a.com\"\nflaresolverr = true\n",
		"[[sites]]\ndomain = \"a.com\"\nflaresolverr_timeout = 99999\n  [sites.crawl]\n  selector = \"a\"\n  include = \"(\"\n",
		"[[sites]]\ndomain = \"rumble.com\"\nimpersonate = \"opera\"\nuser_agent = \"x\"\n",
		"[[sites]]\n[[sites.selectors]]\nfield = \"title\"\n",
		"",
		"not toml",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, content string) {
		dir := t.TempDir()
		p := filepath.Join(dir, "rules.toml")
		original := "[[sites]]\ndomain = \"keep.example\"\nflaresolverr = true\n"
		if err := os.WriteFile(p, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := SaveScrapeSelectorsFile(p, content)
		got, _ := os.ReadFile(p)
		if err != nil && string(got) != original {
			t.Errorf("rejected content changed the file: %q", got)
		}
		if err == nil && string(got) != content {
			t.Errorf("accepted content wasn't saved exactly")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("expected only the rules file to remain, got %d entries", len(entries))
		}
	})
}

// FuzzSplitArgs tests that plain input splits like strings.Fields, and that any arguments survive being shell-quoted,
// joined, and split again.
func FuzzSplitArgs(f *testing.F) {
	for _, s := range []string{"-t sleep --retries 3", `--add-headers "Referer: a b"`, `'it''s'`, `a\ b`, `"x\"y"`, "", `\`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			return
		}
		got, _ := sharedparsing.ShellSplit(s)
		if !strings.ContainsAny(s, `'"\`) && !slices.Equal(got, strings.Fields(s)) {
			t.Errorf("SplitArgs(%q) = %q, want strings.Fields result %q", s, got, strings.Fields(s))
		}

		quoted := make([]string, len(got))
		for i, a := range got {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		if again, _ := sharedparsing.ShellSplit(strings.Join(quoted, " ")); !slices.Equal(again, got) {
			t.Errorf("quoting and re-splitting %q gave %q", got, again)
		}
	})
}

// ******************************** Scrape config *************************************************************************************

// writeScrapeConfig writes the given content to a temporary file with the given name, returning the full path.
func writeScrapeConfig(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestParseScrapeSelectorsFileNeedsSites tests that a file without a sites key is rejected, and an empty list accepted.
func TestParseScrapeSelectorsFileNeedsSites(t *testing.T) {
	if _, err := ParseScrapeSelectorsFile(writeScrapeConfig(t, "other.yaml", "name: something else\n")); err == nil {
		t.Error("file without a sites key was accepted")
	}
	sites, err := ParseScrapeSelectorsFile(writeScrapeConfig(t, "empty.yaml", "sites: []\n"))
	if err != nil || len(sites) != 0 {
		t.Errorf("sites: [] = %d sites, %v; want 0, nil", len(sites), err)
	}
}

// TestParseScrapeSelectorsFile tests parsing a scrape config file with multiple sites and selectors.
func TestParseScrapeSelectorsFileCrawl(t *testing.T) {
	p := writeScrapeConfig(t, "rules.yaml", `
sites:
  - domain: example.com
    impersonate: chrome
    crawl:
      selector: 'script[type="application/json"]'
      json_path: 'items.#.url'
      include: '/v\d+'
      exclude: '/shorts/'
      strip_query: true
`)
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].Crawl == nil {
		t.Fatalf("expected one site with a crawl rule, got %+v", sites)
	}
	c := sites[0].Crawl
	if c.JSONPath != "items.#.url" || !c.StripQuery || !c.Include.MatchString("/v123") || !c.Exclude.MatchString("/shorts/x") {
		t.Errorf("crawl rule parsed incorrectly: %+v", c)
	}
	if len(sites[0].Selectors) != 0 {
		t.Errorf("expected no metadata selectors, got %d", len(sites[0].Selectors))
	}
}

// TestParseScrapeSelectorsFileCrawlPreview tests parsing a crawl rule's item, item_path, and preview fields.
func TestParseScrapeSelectorsFileCrawlPreview(t *testing.T) {
	p := writeScrapeConfig(t, "rules.toml", `
[[sites]]
domain = "example.com"

  [sites.crawl]
  selector = 'script[type="application/json"]'
  item_path = 'items.#(object_type=="video")#'
  json_path = 'url'
  item = '.card'
  preview_fields = [
    { field = 'title', json_path = 'title' },
    { field = 'duration', json_path = 'length', regex = '(\d+)' },
  ]
`)
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	c := sites[0].Crawl
	if c.ItemPath != `items.#(object_type=="video")#` || c.JSONPath != "url" || c.Item != ".card" || len(c.PreviewFields) != 2 {
		t.Fatalf("crawl rule parsed incorrectly: %+v", c)
	}
	if f := c.PreviewFields[1]; f.Name != sharedtags.JDuration || f.JSONPath != "length" || f.Regex == nil {
		t.Errorf("duration preview field parsed incorrectly: %+v", f)
	}
}

// TestParseScrapeSelectorsFileCrawlErrors tests that invalid crawl rules in a scrape config file produce errors.
func TestParseScrapeSelectorsFileCrawlErrors(t *testing.T) {
	tests := map[string]string{
		"sets nothing":           "sites:\n  - domain: example.com\n",
		"missing crawl selector": "sites:\n  - domain: example.com\n    crawl:\n      include: '/v'\n",
		"bad include regex":      "sites:\n  - domain: example.com\n    crawl:\n      selector: a\n      include: '('\n",
		"item_path no json_path": "sites:\n  - domain: example.com\n    crawl:\n      selector: script\n      item_path: items\n",
		"unknown preview field":  "sites:\n  - domain: example.com\n    crawl:\n      selector: a\n      preview_fields:\n        - {field: description, selector: p}\n",
		"preview no selector":    "sites:\n  - domain: example.com\n    crawl:\n      selector: a\n      preview_fields:\n        - {field: title}\n",
		"preview fetch":          "sites:\n  - domain: example.com\n    crawl:\n      selector: a\n      preview_fields:\n        - {field: title, selector: h5, fetch: [{url: '/x'}]}\n",
		"preview bad regex":      "sites:\n  - domain: example.com\n    crawl:\n      selector: a\n      preview_fields:\n        - {field: title, selector: h5, regex: '('}\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseScrapeSelectorsFile(writeScrapeConfig(t, "rules.yaml", content)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// TestParseScrapeSelectorsFileFetch tests parsing a selector's json_path, regex, and fetch steps.
func TestParseScrapeSelectorsFileFetch(t *testing.T) {
	p := writeScrapeConfig(t, "rules.toml", `
[[sites]]
domain = "example.com"

  [[sites.selectors]]
  field = "direct_video_url"
  selector = 'script[type="application/ld+json"]'
  json_path = "embedUrl"
  regex = 'embed/([a-z0-9]+)'

    [[sites.selectors.fetch]]
    url = "https://example.com/api?v={value}"
    json_path = "u.hls.url"

    [[sites.selectors.fetch]]
    url = "{value}"
    selector = "source"
    attr = "src"
    regex = '\.m3u8'
`)
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || len(sites[0].Selectors) != 1 {
		t.Fatalf("expected one site with one selector, got %+v", sites)
	}
	sel := sites[0].Selectors[0]
	if sel.JSONPath != "embedUrl" || sel.Regex == nil || sel.Regex.String() != "embed/([a-z0-9]+)" || len(sel.Fetch) != 2 {
		t.Fatalf("selector parsed incorrectly: %+v", sel)
	}
	if f := sel.Fetch[0]; f.URL != "https://example.com/api?v={value}" || f.JSONPath != "u.hls.url" || f.Regex != nil {
		t.Errorf("fetch step 1 parsed incorrectly: %+v", f)
	}
	if f := sel.Fetch[1]; f.URL != "{value}" || f.Selector != "source" || f.Attr != "src" || f.Regex == nil {
		t.Errorf("fetch step 2 parsed incorrectly: %+v", f)
	}
}

// TestParseScrapeSelectorsFileFetchErrors tests that invalid selector regexes and fetch steps produce errors.
func TestParseScrapeSelectorsFileFetchErrors(t *testing.T) {
	sel := "sites:\n  - domain: example.com\n    selectors:\n      - field: title\n        selector: h1\n"
	tests := map[string]string{
		"bad selector regex": sel + "        regex: '('\n",
		"fetch missing url":  sel + "        fetch:\n          - json_path: a\n",
		"bad fetch regex":    sel + "        fetch:\n          - url: '{value}'\n            regex: '['\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseScrapeSelectorsFile(writeScrapeConfig(t, "rules.yaml", content)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// TestParseExampleScrapeRules tests that the example scrape rules file parses, including its Rumble crawl and direct video URL rules.
func TestParseExampleScrapeRules(t *testing.T) {
	sites, err := ParseScrapeSelectorsFile("../../web/src/files/example-scrape-rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		if s.Domain != "rumble.com" {
			continue
		}
		if s.Crawl == nil {
			t.Error("expected a crawl rule for rumble.com")
		}
		for _, sel := range s.Selectors {
			if sel.Field == "direct_video_url" && len(sel.Fetch) == 1 {
				return
			}
		}
		t.Error("expected a direct_video_url selector with a fetch step for rumble.com")
		return
	}
	t.Error("expected rules for rumble.com")
}

// TestParseScrapeSelectorsFileImpersonate tests that an unset impersonate value is distinguished from an explicit "" or "none".
func TestParseScrapeSelectorsFileImpersonate(t *testing.T) {
	files := map[string]string{
		"rules.toml": `
[[sites]]
domain = "unset.com"
  [sites.crawl]
  selector = "a"
[[sites]]
domain = "empty.com"
impersonate = ""
  [sites.crawl]
  selector = "a"
[[sites]]
domain = "none.com"
impersonate = "none"
  [sites.crawl]
  selector = "a"
[[sites]]
domain = "firefox.com"
impersonate = "Firefox"
  [sites.crawl]
  selector = "a"
`,
		"rules.yaml": `
sites:
  - domain: unset.com
    crawl: {selector: a}
  - domain: empty.com
    impersonate: ""
    crawl: {selector: a}
  - domain: none.com
    impersonate: none
    crawl: {selector: a}
  - domain: firefox.com
    impersonate: Firefox
    crawl: {selector: a}
`,
		"rules.json": `{"sites": [
  {"domain": "unset.com", "crawl": {"selector": "a"}},
  {"domain": "empty.com", "impersonate": "", "crawl": {"selector": "a"}},
  {"domain": "none.com", "impersonate": "none", "crawl": {"selector": "a"}},
  {"domain": "firefox.com", "impersonate": "Firefox", "crawl": {"selector": "a"}}
]}`,
	}
	want := map[string]string{"empty.com": "", "none.com": "", "firefox.com": "firefox"}

	for name, content := range files {
		t.Run(name, func(t *testing.T) {
			sites, err := ParseScrapeSelectorsFile(writeScrapeConfig(t, name, content))
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range sites {
				w, explicit := want[s.Domain]
				switch {
				case !explicit && s.Impersonate != nil:
					t.Errorf("%s: expected unset impersonate, got %q", s.Domain, *s.Impersonate)
				case explicit && s.Impersonate == nil:
					t.Errorf("%s: expected impersonate %q, got unset", s.Domain, w)
				case explicit && string(*s.Impersonate) != w:
					t.Errorf("%s: expected impersonate %q, got %q", s.Domain, w, *s.Impersonate)
				}
			}
		})
	}
}

// TestParseScrapeSelectorsFileFlareSolverr tests that the flaresolverr key is read from a scrape config file.
func TestParseScrapeSelectorsFileFlareSolverr(t *testing.T) {
	p := writeScrapeConfig(t, "rules.toml", "[[sites]]\ndomain = \"on.com\"\nflaresolverr = true\n  [sites.crawl]\n  selector = \"a\"\n[[sites]]\ndomain = \"off.com\"\n  [sites.crawl]\n  selector = \"a\"\n")
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		if s.FlareSolverr != (s.Domain == "on.com") {
			t.Errorf("%s: FlareSolverr = %v", s.Domain, s.FlareSolverr)
		}
	}
}

// TestSaveScrapeSelectorsFile tests that only valid content replaces a scrape config file, leaving no temporary files.
func TestSaveScrapeSelectorsFile(t *testing.T) {
	original := "[[sites]]\ndomain = \"a.com\"\n  [sites.crawl]\n  selector = \"a\"\n"
	p := writeScrapeConfig(t, "rules.toml", original)

	// Invalid content is rejected, naming the real file, and the original is untouched.
	_, err := SaveScrapeSelectorsFile(p, "[[sites]]\ndomain = \"b.com\"\n")
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Errorf("expected a validation error naming %q, got %v", p, err)
	}
	if got, _ := os.ReadFile(p); string(got) != original {
		t.Errorf("expected the original file to be untouched, got %q", got)
	}

	// Valid content replaces the file and returns its sites.
	updated := "[[sites]]\ndomain = \"c.com\"\n  [sites.crawl]\n  selector = \"a\"\n"
	sites, err := SaveScrapeSelectorsFile(p, updated)
	if err != nil || len(sites) != 1 || sites[0].Domain != "c.com" {
		t.Fatalf("expected 1 site c.com, got %+v, %v", sites, err)
	}
	if got, _ := os.ReadFile(p); string(got) != updated {
		t.Errorf("expected the file to be replaced, got %q", got)
	}

	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("expected only the rules file to remain, got %d entries", len(entries))
	}
}

// TestParseScrapeSelectorsFileSettingsOnly tests that a site may set only impersonate or flaresolverr.
func TestParseScrapeSelectorsFileSettingsOnly(t *testing.T) {
	p := writeScrapeConfig(t, "rules.toml", "[[sites]]\ndomain = \"rumble.com\"\nflaresolverr = true\n\n[[sites]]\ndomain = \"b.com\"\nimpersonate = \"firefox\"\n")
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 || !sites[0].FlareSolverr || sites[1].Impersonate == nil || *sites[1].Impersonate != "firefox" {
		t.Errorf("unexpected sites: %+v", sites)
	}
}

// TestParseScrapeSelectorsFileFlareSolverrTimeout tests that flaresolverr_timeout is read in seconds and clamped.
func TestParseScrapeSelectorsFileFlareSolverrTimeout(t *testing.T) {
	p := writeScrapeConfig(t, "rules.toml", `
[[sites]]
domain = "unset.com"
flaresolverr = true
[[sites]]
domain = "ok.com"
flaresolverr = true
flaresolverr_timeout = 120
[[sites]]
domain = "low.com"
flaresolverr_timeout = 5
[[sites]]
domain = "high.com"
flaresolverr_timeout = 1000
`)
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Duration{
		"unset.com": 0,
		"ok.com":    120 * time.Second,
		"low.com":   consts.FlareSolverrMinTimeout,
		"high.com":  consts.FlareSolverrMaxTimeout,
	}
	for _, s := range sites {
		if s.FlareSolverrTimeout != want[s.Domain] {
			t.Errorf("%s: got %s, want %s", s.Domain, s.FlareSolverrTimeout, want[s.Domain])
		}
	}
}

// TestParseScrapeSelectorsFileUserAgent tests that user_agent is read, and is enough for a site on its own.
func TestParseScrapeSelectorsFileUserAgent(t *testing.T) {
	p := writeScrapeConfig(t, "rules.toml", "[[sites]]\ndomain = \"ua.example\"\nuser_agent = \"  Site/1 Chrome/130  \"\n")
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].UserAgent != "Site/1 Chrome/130" {
		t.Errorf("unexpected sites: %+v", sites)
	}
}

// ******************************** Viper *********************************************************************************************

// TestExampleConfigFiles tests the example channel config files load their download concurrency, crawl frequency,
// operations and per-URL settings.
func TestExampleConfigFiles(t *testing.T) {
	for _, name := range []string{"example.yaml", "example.toml", "example.json"} {
		v := viper.New()
		if err := file.LoadConfigFile(v, filepath.Join("..", "..", "web", "src", "files", name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var in models.ChannelInput
		if err := LoadViperIntoStruct(v, &in); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		urlSettings, err := ParseURLSettingsFromViper(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if NilOrZeroValue(in.DownloadConcurrency) != 3 || NilOrZeroValue(in.CrawlFreq) != 30 || len(NilOrZeroValue(in.MetaOps)) != 3 || len(urlSettings) != 1 {
			t.Errorf("%s: download concurrency %d, crawl frequency %d, %d meta ops, %d URL settings; want 3, 30, 3, 1",
				name, NilOrZeroValue(in.DownloadConcurrency), NilOrZeroValue(in.CrawlFreq), len(NilOrZeroValue(in.MetaOps)), len(urlSettings))
		}
	}
}

// TestProfileConfigKeys tests that the profile key names a channel's profile, and a URL's in its url-settings.
func TestProfileConfigKeys(t *testing.T) {
	const url = "https://example.com/@a"
	v := viper.New()
	v.Set("profile", "music")
	v.Set("url-settings", map[string]any{url: map[string]any{"profile": "podcasts"}})
	var in models.ChannelInput
	if err := LoadViperIntoStruct(v, &in); err != nil {
		t.Fatal(err)
	}
	urlSettings, err := ParseURLSettingsFromViper(v)
	if err != nil {
		t.Fatal(err)
	}
	if got := NilOrZeroValue(in.Profile); got != "music" {
		t.Errorf("channel profile %q, want music", got)
	}
	if o := urlSettings[url]; o == nil || NilOrZeroValue(o.Profile) != "podcasts" {
		t.Errorf("URL settings %+v, want profile podcasts", o)
	}

	v.Set("url-settings", map[string]any{url: map[string]any{"profile": []any{"podcasts"}}})
	if _, err := ParseURLSettingsFromViper(v); err == nil {
		t.Error("read a URL profile given as a list")
	}
}

// TestDownloadConcurrencyKey tests only the download-concurrency config key sets download concurrency.
func TestDownloadConcurrencyKey(t *testing.T) {
	for key, want := range map[string]int{"download-concurrency": 4, "download_concurrency": 4, "concurrency-limit": 0, "concurrency": 0} {
		v := viper.New()
		v.Set(key, 4)
		var in models.ChannelInput
		if err := LoadViperIntoStruct(v, &in); err != nil || NilOrZeroValue(in.DownloadConcurrency) != want {
			t.Errorf("config key %q: download concurrency %d, %v; want %d", key, NilOrZeroValue(in.DownloadConcurrency), err, want)
		}
	}
}

// TestGivenKeys tests that GivenKeys lists the keys of the fields given, including those of embedded inputs.
func TestGivenKeys(t *testing.T) {
	name, freq, ext := "chan", 5, "mkv"
	var in models.ChannelInput
	in.Name, in.CrawlFreq, in.MetarrExt = &name, &freq, &ext
	want := []string{"channel-name", "crawl-freq", "metarr-output-ext"}
	if got := GivenKeys(&in); !slices.Equal(got, want) {
		t.Errorf("GivenKeys = %v, want %v", got, want)
	}
}

// TestURLSettingsName tests a URL's name is read from its url-settings entry, trimmed, and must be text.
func TestURLSettingsName(t *testing.T) {
	v := viper.New()
	v.Set("url-settings", map[string]any{"https://example.com/@a": map[string]any{"name": " Main "}})
	urlSettings, err := ParseURLSettingsFromViper(v)
	if err != nil {
		t.Fatal(err)
	}
	if o := urlSettings["https://example.com/@a"]; o == nil || o.Name == nil || *o.Name != "Main" {
		t.Errorf("got %+v, want name %q", o, "Main")
	}

	v.Set("url-settings", map[string]any{"https://example.com/@a": map[string]any{"name": 3}})
	if _, err := ParseURLSettingsFromViper(v); err == nil {
		t.Error("a name that isn't text was accepted")
	}
}

// TestApplySameAndKeep tests the 'Same as input' extensions and 'Keep' metadata files are accepted and stored as chosen.
func TestApplySameAndKeep(t *testing.T) {
	same := sharedconsts.ExtSame
	keep := "keep"

	// yt-dlp's extension.
	var s models.Settings
	if err := ApplySettingsInput(&s, &models.SettingsInput{YTDLPOutputExt: &same}); err != nil || s.YtdlpOutputExt != same {
		t.Errorf("yt-dlp extension: got %q, %v; want %q", s.YtdlpOutputExt, err, same)
	}

	// Metarr's extension and metadata files.
	var m models.MetarrArgs
	if err := ApplyMetarrArgsInput(&m, &models.MetarrArgsInput{MetarrExt: &same, MetaPurge: &keep}, enums.ChannelLevel); err != nil || m.OutputExt != same || m.MetaPurge != keep {
		t.Errorf("Metarr args: got output extension %q and purge %q, %v; want %q and %q", m.OutputExt, m.MetaPurge, err, same, keep)
	}
}

// TestApplyOutputDirectoryTags tests which directories accept the output directory tags on a channel and on a channel URL.
func TestApplyOutputDirectoryTags(t *testing.T) {
	channelTag := "{{channel_output_directory}}/Shorts"
	urlTag := "{{channel_url_output_directory}}/Shorts"

	// Video and JSON directories accept neither tag.
	for _, dir := range []*string{&channelTag, &urlTag} {
		var s models.Settings
		if err := ApplySettingsInput(&s, &models.SettingsInput{VideoDir: dir}); err == nil {
			t.Errorf("video directory %q was accepted", *dir)
		}
		if err := ApplySettingsInput(&s, &models.SettingsInput{JSONDir: dir}); err == nil {
			t.Errorf("JSON directory %q was accepted", *dir)
		}
	}

	// Metarr output directories accept both on a channel URL, and only the channel's on a channel.
	tests := []struct {
		name  string
		dir   *string
		level enums.SettingsLevel
		want  bool
	}{
		{"channel tag on a channel", &channelTag, enums.ChannelLevel, true},
		{"channel URL tag on a channel", &urlTag, enums.ChannelLevel, false},
		{"channel tag on a channel URL", &channelTag, enums.ChannelURLLevel, true},
		{"channel URL tag on a channel URL", &urlTag, enums.ChannelURLLevel, true},
	}
	for _, tt := range tests {
		var m models.MetarrArgs
		if err := ApplyMetarrArgsInput(&m, &models.MetarrArgsInput{OutDir: tt.dir}, tt.level); (err == nil) != tt.want {
			t.Errorf("%s: got %v, want accepted: %t", tt.name, err, tt.want)
		}
	}
}

// TestConvertConfigValueRejectsMapsAndLists tests that a map or list given for a string setting is rejected, rather than
// turned into text like "map[a:1]" (which a directory setting would then create as a folder).
func TestConvertConfigValueRejectsMapsAndLists(t *testing.T) {
	for _, v := range []any{map[string]any{"a": 1}, []any{"x"}, nil} {
		if got, ok := convertConfigValue[string](v); ok {
			t.Errorf("convertConfigValue[string](%#v) = %q, want rejected", v, got)
		}
	}
	if _, ok := convertConfigValue[[]string]([]any{"a", map[string]any{"b": 1}}); ok {
		t.Error("expected a list containing a map to be rejected as []string")
	}
	for v, want := range map[any]string{"dir": "dir", 123: "123", true: "true", 1.5: "1.5"} {
		if got, ok := convertConfigValue[string](v); !ok || got != want {
			t.Errorf("convertConfigValue[string](%#v) = %q, %v, want %q", v, got, ok, want)
		}
	}
}
