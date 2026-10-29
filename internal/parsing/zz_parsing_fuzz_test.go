package parsing

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/models"
	"tubarr/internal/validation"
	"unicode/utf8"

	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/spf13/viper"
)

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
