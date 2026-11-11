package validation

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtemplates"
)

// ******************************** Fuzz **********************************************************************************************

// TestMain sets up the test environment for the validation package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// FuzzValidateStrings tests that the single-value validators never panic, and that normalising validators give the same
// result when run on their own output.
func FuzzValidateStrings(f *testing.F) {
	for _, s := range []string{
		"100M", "1.5G", "500K", "0", "-1M", "9999999999999999999T", "abc", "",
		"20261011", "2026-10-11", "today", "now-1week", "19000101", "99999999",
		"mp4", "mkv", "webm", ".mp4", "spaces", "underscores", "fixes-only", "skip",
		"auto", "nvidia", "vaapi", "intel", "amd", "none", "scale=1280:-1", "Y-m-d",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if out, err := ValidateMaxFilesize(s); err == nil {
			if again, err := ValidateMaxFilesize(out); err != nil || again != out {
				t.Errorf("ValidateMaxFilesize(%q) = %q, but validating that gives %q, %v", s, out, again, err)
			}
		}
		if out, err := ValidateToFromDate(s); err == nil && out != "" {
			if again, err := ValidateToFromDate(out); err != nil || again != out {
				t.Errorf("ValidateToFromDate(%q) = %q, but validating that gives %q, %v", s, out, again, err)
			}
		}
		if out, err := ValidateYtdlpOutputExtension(s); err == nil {
			if again, err := ValidateYtdlpOutputExtension(out); err != nil || again != out {
				t.Errorf("ValidateYtdlpOutputExtension(%q) = %q, but validating that gives %q, %v", s, out, again, err)
			}
		}
		_, _ = ValidateExtraYtdlpArgs(s)
		_ = ValidateRenameFlag(s)
		_, _ = ValidatePurgeMetafiles(s)
		_, _ = ValidateGPUAcceleration(s)
	})
}

// FuzzValidateLists tests that the list validators never panic on any comma- or newline-separated input.
func FuzzValidateLists(f *testing.F) {
	for _, s := range []string{"h264,hevc", "av1", "aac:ac3\ndts:eac3", "h264:hevc\nvp9:h264", "webm:h264", ":", "", "a:b:c"} {
		f.Add(s, "auto")
		f.Add(s, "")
	}
	f.Fuzz(func(_ *testing.T, s, accel string) {
		list := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' })
		_, _ = ValidateYtdlpVideoCodecs(list)
		_, _ = ValidateYtdlpAudioCodecs(list)
		_, _ = ValidateAudioTranscodeCodecSlice(list)
		_, _ = ValidateVideoTranscodeCodecSlice(list, accel)
	})
}

// FuzzDeduplicateSliceEntries tests that deduplication keeps the first of each entry, in order, with no duplicates.
func FuzzDeduplicateSliceEntries(f *testing.F) {
	f.Add("a\nb\na\nc\nb")
	f.Add("")
	f.Add("x\nx\nx")
	f.Fuzz(func(t *testing.T, s string) {
		in := strings.Split(s, "\n")
		out := DeduplicateSliceEntries(in)

		seen := map[string]bool{}
		var want []string
		for _, v := range in {
			if !seen[v] {
				seen[v] = true
				want = append(want, v)
			}
		}
		if strings.Join(out, "\n") != strings.Join(want, "\n") || len(out) != len(want) {
			t.Errorf("DeduplicateSliceEntries(%q) = %q, want %q", in, out, want)
		}
	})
}

// ******************************** Directories ***************************************************************************************

// TestCheckDirectory tests that directory paths are validated without anything being created.
func TestCheckDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"existing", dir, false},
		{"missing (created when saved)", filepath.Join(dir, "new", "videos"), false},
		{"templated", filepath.Join(dir, "{{"+sharedtemplates.ChannelName+"}}", "videos"), false},
		{"unsupported template tag", filepath.Join(dir, "{{not_a_tag}}"), true},
		{"file, not a directory", file, true},
	}
	for _, tt := range tests {
		if err := CheckDirectory(tt.path, sharedtemplates.TubarrTemplateTags); (err != nil) != tt.wantErr {
			t.Errorf("%s: CheckDirectory(%q) error = %v, want error: %v", tt.name, tt.path, err, tt.wantErr)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("CheckDirectory created something: %v", entries)
	}
}

// TestCheckDestination tests that a destination is only available when it (or, if templated, the directory holding its
// fixed part) exists.
func TestCheckDestination(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"", false},
		{dir, false},
		{filepath.Join(dir, "missing"), true},
		{filepath.Join(dir, "{{year}}", "videos"), false},
		{filepath.Join(dir, "show-{{year}}"), false},
		{filepath.Join(dir, "missing", "{{year}}"), true},
		{"/{{year}}/videos", false},
		{"{{channel_name}}/videos", false},
		{file, true},
	}
	for _, tt := range tests {
		if err := CheckDestination(tt.path); (err != nil) != tt.wantErr {
			t.Errorf("CheckDestination(%q) error = %v, want error: %v", tt.path, err, tt.wantErr)
		}
	}
}

// TestSettingsRefuseMissingFolder tests saving settings refuses a missing plain folder, or a templated one whose fixed
// part is missing.
func TestSettingsRefuseMissingFolder(t *testing.T) {
	dir := t.TempDir()
	for _, missing := range []string{filepath.Join(dir, "missing"), filepath.Join(dir, "missing", "{{channel_name}}")} {
		if err := ValidateSettingsModel(&models.Settings{VideoDir: missing}, true); err == nil {
			t.Errorf("video folder %q: accepted", missing)
		}
		if err := ValidateSettingsModel(&models.Settings{JSONDir: missing}, true); err == nil {
			t.Errorf("JSON folder %q: accepted", missing)
		}
		moveOps := []models.MetaFilterMoveOps{{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cats", MustAny: "must"}}, OutputDir: missing}}
		if err := ValidateSettingsModel(&models.Settings{MetaFilterMoveOps: moveOps}, true); err == nil {
			t.Errorf("move op folder %q: accepted", missing)
		}
		if err := ValidateMetarrArgsModel(&models.MetarrArgs{OutputDir: missing}, true); err == nil {
			t.Errorf("Metarr output folder %q: accepted", missing)
		}
		if err := ValidateSettingsModel(&models.Settings{VideoDir: missing}, false); err != nil {
			t.Errorf("loading %q (paths unchecked): %v", missing, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("validation created something: %v", entries)
	}
}
