package validation

import (
	"io"
	"os"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
)

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
		_ = ValidateYtdlpOutputExtension(s)
		_ = ValidateRenameFlag(s)
		_ = ValidateDateFormat(s)
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
		_, _ = ValidatePreferredVideoCodecs(list)
		_, _ = ValidatePreferredAudioCodecs(list)
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
