package validation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// ******************************** Dates and sizes ***********************************************************************************

// TestValidateToFromDate_DayWithoutMonth tests a number marked as a day stays the day when no month is given.
func TestValidateToFromDate_DayWithoutMonth(t *testing.T) {
	thisYear := strconv.Itoa(time.Now().Year())
	for _, tc := range []struct{ input, want string }{
		{"2024y5d", "20240105"},
		{"2024y15d", "20240115"},
		{"5d", thisYear + "0105"},
		{"2024y1m5d", "20240105"},
		{"2024y5m", "20240501"},
		{"5m", thisYear + "0501"},
		{"20240105", "20240105"},
		{"2024-01-05", "20240105"},
	} {
		got, err := validation.ValidateToFromDate(tc.input)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestValidateYtdlpVideoCodecs_ErrorNamesVideo tests a refused video codec's error says video.
func TestValidateYtdlpVideoCodecs_ErrorNamesVideo(t *testing.T) {
	_, err := validation.ValidateYtdlpVideoCodecs([]string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "video codec") {
		t.Errorf("error = %v, want one naming a video codec", err)
	}
}

// ******************************** Encoder args **************************************************************************************

// TestEncoderArgsThatCantWork tests that encoder args that can't work on any FFmpeg version are refused.
func TestEncoderArgsThatCantWork(t *testing.T) {
	for _, line := range []string{
		// Copied video isn't encoded.
		"copy:-pix_fmt yuv420p10le",
		"copy:-crf 20",

		// A word where the encoder takes a number.
		"av1:-preset medium",
		"av1:-preset ultrafast",
		"av1:-preset p5",
		"vp9:-profile:v main",
		"vp8:-profile:v high",
		"vp9:-profile main",

		// Options named after another encoder.
		"vp9:-x265-params log-level=error",
		"h264:-svtav1-params film-grain=8",
		"hevc:-x264-params keyint=12",
		"av1:-x264-params keyint=12",
		"mpeg2:-x265-params log-level=error",
	} {
		m := &models.MetarrArgs{TranscodeVideoArgs: []string{line}}
		if err := validation.ValidateMetarrEncoderArgs(m); err == nil {
			t.Errorf("%q was saved", line)
		}
	}
}

// TestEncoderArgsLeftForFFmpeg tests that anything that might work on some FFmpeg version saves, even where it fails on the
// one measured, so a video that can't use it fails with FFmpeg's own error.
func TestEncoderArgsLeftForFFmpeg(t *testing.T) {
	for _, line := range []string{
		// Working values.
		"h264:-pix_fmt yuv420p10le",
		"av1:-pix_fmt yuv420p10le -preset 8",
		"av1:-preset -1",
		"vp9:-profile:v 2 -pix_fmt yuv420p10le",
		"h264:-preset veryslow -tune film -profile:v high",
		"h264:-x264-params keyint=12",
		"hevc:-x265-params log-level=error",
		"av1:-svtav1-params film-grain=8",

		// Fail on FFmpeg 9 with these builds, but that could change.
		"vp8:-pix_fmt yuv420p10le",
		"av1:-pix_fmt yuv444p",
		"mpeg2:-pix_fmt yuv444p",
		"hevc:-pix_fmt yuva420p",
		"av1:-profile:v high",
		"av1:-preset 14",
		"h264:-preset p5",
		"h264:-preset 12",
		"h264:-profile:v 2",
		"hevc:-profile:v rext",
		"h264:-tune hq",
		"hevc:-tune film",
		"vp9:-tune film",
		"h264:-level 8",
		"av1:-level 4",
		"vp9:-level 31",
		"mpeg2:-level 8",
		"h264:-vsync cfr",
		"h264:-profile:v main -pix_fmt yuv420p10le",
		"h264:-crf 0 -profile:v high",
		"vp8:-pix_fmt yuva420p",
		"vp9:-maxrate 500k",
		"mpeg2:-bufsize 1M",
		"av1:-b:v 400k -maxrate 500k",
		"vp9:-qmin 40 -qmax 10",
		"vp9:-qmax 31",
		"h264:-tag:v hvc1",
		"vp9:-bsf:v h264_mp4toannexb",

		// Ignored here, but another version or encoder could read them.
		"vp8:-preset slow",
		"av1:-tune film",
		"h264:-row-mt 1",

		// Never measured.
		"av1:-preset somefuturepreset",
		"vp9:-profile:v somefutureprofile",
		"hevc:-pix_fmt somefutureformat",
		"h264:-somefutureoption 1",
	} {
		m := &models.MetarrArgs{TranscodeVideoArgs: []string{line}}
		if err := validation.ValidateMetarrEncoderArgs(m); err != nil {
			t.Errorf("%q was refused: %v", line, err)
		}
	}
}

// TestEncoderArgsWithGPU tests that values only wrong on software encoders are left alone when a GPU encoder may run.
func TestEncoderArgsWithGPU(t *testing.T) {
	for _, c := range []struct {
		name string
		gpu  string
		line string
		ok   bool
	}{
		{"AV1 preset name with no GPU", "", "av1:-preset medium", false},
		{"AV1 preset name with the GPU off", validation.GPUNone, "av1:-preset medium", false},
		{"AV1 preset name with auto, which encodes in software", "auto", "av1:-preset medium", false},
		{"AV1 preset name on NVENC, which takes names", "cuda", "av1:-preset p5", true},
		{"AV1 preset name on QSV, which takes names", "qsv", "av1:-preset medium", true},
		{"VP9 profile name on VA-API", "vaapi", "vp9:-profile:v main", true},
		{"copy line with a GPU", "cuda", "copy:-crf 20", false},
		{"another encoder's option with a GPU", "cuda", "h264:-x265-params log-level=error", false},
	} {
		m := &models.MetarrArgs{TranscodeGPU: c.gpu, TranscodeVideoArgs: []string{c.line}}
		if err := validation.ValidateMetarrEncoderArgs(m); (err == nil) != c.ok {
			t.Errorf("%s: got %v, want ok %t", c.name, err, c.ok)
		}
	}
}

// TestEncoderLinesForCopiedVideo tests a codec's line is refused when its videos are copied and nothing encodes to it.
func TestEncoderLinesForCopiedVideo(t *testing.T) {
	for _, c := range []struct {
		name  string
		video []string
		ext   string
		ok    bool
	}{
		{name: "plain copy", video: []string{"copy"}},
		{name: "plain copy with a container set", video: []string{"copy"}, ext: ".mp4"},
		{name: "a pair to copy", video: []string{"h264:copy"}},
		{name: "a pair to copy beside another plain codec", video: []string{"hevc", "h264:copy"}},
		{name: "a pair to copy beside another plain codec with a container set", video: []string{"hevc", "h264:copy"}, ext: ".mp4"},
		{name: "a pair to copy beside its own plain codec, which others encode to", video: []string{"h264", "h264:copy"}, ok: true},
		{name: "plain copy with another source encoded elsewhere", video: []string{"copy", "vp9:hevc"}},
		{name: "plain copy with a pair encoding to it", video: []string{"copy", "vp9:h264"}, ok: true},
		{name: "plain copy with its own pair", video: []string{"copy", "h264:h264"}, ok: true},
		{name: "a pair to copy with a pair encoding to it", video: []string{"h264:copy", "vp9:h264"}, ok: true},
		{name: "a pair to copy when a container may choose this codec", video: []string{"h264:copy"}, ext: ".mp4", ok: true},
		{name: "a level's pair to copy, as other levels can still encode", video: []string{"lc_h264:copy"}, ok: true},
		{name: "plain own codec", video: []string{"h264"}, ok: true},
		{name: "a pair for another source", video: []string{"vp9:hevc"}, ok: true},
		{name: "nothing named", ok: true},
	} {
		m := &models.MetarrArgs{TranscodeVideoCodecs: c.video, OutputExt: c.ext, TranscodeVideoArgs: []string{"h264:-crf 20"}}
		if err := validation.ValidateMetarrEncoderArgs(m); (err == nil) != c.ok {
			t.Errorf("%s: got %v, want ok %t", c.name, err, c.ok)
		}
	}
}

// TestExtraFFmpegArgsAudioOptions tests audio filters and audio encoding options are refused in extra FFmpeg args, whatever
// the audio codecs.
func TestExtraFFmpegArgsAudioOptions(t *testing.T) {
	for _, c := range []struct {
		name  string
		extra string
		audio []string
		ok    bool
	}{
		{name: "filter with no codec", extra: "-af volume=0.5"},
		{name: "filter with plain copy", extra: "-filter:a volume=0.5", audio: []string{"copy"}},
		{name: "filter with a codec", extra: "-af volume=0.5", audio: []string{"aac"}},
		{name: "bitrate with a codec", extra: "-b:a 128k", audio: []string{"aac"}},
		{name: "first stream's bitrate with no codec", extra: "-b:a:0 128k"},
		{name: "channels with no codec", extra: "-ac 1"},
		{name: "sample rate with a pair", extra: "-ar 44100", audio: []string{"dts:ac3"}},
		{name: "quality with no codec", extra: "-q:a 2"},
		{name: "settings that don't touch audio", extra: "-movflags +faststart -threads 1", ok: true},

		// Fail on FFmpeg 9 with these builds, but that could change.
		{name: "-vsync", extra: "-vsync cfr", ok: true},
		{name: "AAC filter on AC-3", extra: "-bsf:a aac_adtstoasc", audio: []string{"ac3"}, ok: true},
		{name: "HEVC tag", extra: "-tag:v hvc1", ok: true},
	} {
		m := &models.MetarrArgs{ExtraFFmpegArgs: c.extra, TranscodeAudioCodecs: c.audio}
		if err := validation.ValidateMetarrEncoderArgs(m); (err == nil) != c.ok {
			t.Errorf("%s: got %v, want ok %t", c.name, err, c.ok)
		}
	}
}

// TestAudioEncoderArgs tests audio encoder args that can't work are refused, and anything that might work on some FFmpeg version saves.
func TestAudioEncoderArgs(t *testing.T) {
	for _, c := range []struct {
		name  string
		line  string
		audio []string
		ok    bool
	}{
		// Can't work.
		{name: "copy line", line: "copy:-b:a 128k"},
		{name: "line for copied audio", line: "aac:-b:a 128k", audio: []string{"copy"}},
		{name: "no codec", line: "-b:a 128k"},
		{name: "two filters", line: "aac:-af volume=2 -filter:a loudnorm"},

		// Working values.
		{name: "bitrate, channels and filter", line: "aac:-b:a 160k -ac 2 -af loudnorm", audio: []string{"aac"}, ok: true},
		{name: "sample rate in kHz", line: "aac:-ar 44.1k", ok: true},

		// Fail on FFmpeg 9 with these builds, but that could change.
		{name: "Opus with a rate it refuses here", line: "opus:-ar 44100", audio: []string{"opus"}, ok: true},
		{name: "Opus with a quality it refuses here", line: "opus:-q:a 2", audio: []string{"opus"}, ok: true},
	} {
		m := &models.MetarrArgs{TranscodeAudioArgs: []string{c.line}, TranscodeAudioCodecs: c.audio}
		if err := validation.ValidateMetarrEncoderArgs(m); (err == nil) != c.ok {
			t.Errorf("%s: got %v, want ok %t", c.name, err, c.ok)
		}
	}
}

// TestEncoderArgLinesAlone tests the checks made with no other settings known, as for a profile not used yet, leave out
// anything a channel's GPU or codecs could change.
func TestEncoderArgLinesAlone(t *testing.T) {
	for _, c := range []struct {
		line string
		ok   bool
	}{
		{line: "copy:-crf 20"},
		{line: "h264:-x265-params log-level=error"},
		{line: "av1:-preset p5", ok: true},
		{line: "vp9:-profile:v main", ok: true},
		{line: "h264:-crf 20", ok: true},
	} {
		if err := validation.ValidateEncoderArgLines([]string{c.line}); (err == nil) != c.ok {
			t.Errorf("%q: got %v, want ok %t", c.line, err, c.ok)
		}
	}
}

// ******************************** Validation ****************************************************************************************

// TestValidateRenameFlags ------------------------------------------------------------------------------------------------
func TestValidateRenameFlag(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
	}{
		// Valid.
		{"skips", true},
		{"", true},

		// Invalid.
		{"blah", false},
	}

	for _, tt := range tests {
		err := validation.ValidateRenameFlag(tt.input)
		if tt.ok && err != nil {
			t.Fatalf("input %q expected to pass, got err: %v", tt.input, err)
		}
		if !tt.ok && err == nil {
			t.Fatalf("input %q expected to fail, got nil err", tt.input)
		}
	}
}

// TestValidateRenameFlagError tests that an invalid rename style's error names the style given and lists the valid ones.
func TestValidateRenameFlagError(t *testing.T) {
	err := validation.ValidateRenameFlag("bogus")
	if err == nil {
		t.Fatal("accepted rename style bogus")
	}
	if msg := err.Error(); !strings.Contains(msg, `"bogus"`) || strings.Contains(msg, "map[") {
		t.Errorf("error %q should name %q and list the valid styles plainly", msg, "bogus")
	}
}

func TestMaxCPU(t *testing.T) {
	tests := []struct {
		input     float64
		allowZero bool
		expect    float64
	}{
		// Zero values.
		{0, true, 0.0},
		{0.0, true, 0.0},
		{0, false, 101.0},

		// Non-zero values.
		{100, false, 101.0},
		{1, false, 5.0},
		{105, false, 101.0},
		{100, true, 101.0},
		{1, true, 5.0},
		{105, true, 101.0},
	}

	for _, tt := range tests {
		got := sharedvalidation.ValidateMaxCPU(tt.input, tt.allowZero)
		if tt.expect != got {
			t.Fatalf("input %f allowZero %v: expected %f, got %f", tt.input, tt.allowZero, tt.expect, got)
		}
	}
}

// TestValidateDirectory runs checks for directory validation --------------------------------------------------------------------
func TestValidateDirectory_ExistingDirectory(t *testing.T) {
	tmp := t.TempDir()

	_, info, err := sharedvalidation.ValidateDirectory(tmp, false, sharedtemplates.AllTemplatesMap)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if info == nil {
		t.Fatalf("expected file info, got nil")
	}
}

func TestValidateDirectory_CreateIfMissing(t *testing.T) {
	tmp := t.TempDir()
	missing := tmp + "/new"
	invalidName := tmp + "/bad\x00name"
	t.Cleanup(func() {
		if err := os.Remove(missing); err != nil {
			fmt.Printf("Could not remove %q: %v", missing, err)
		}
		if err := os.Remove(invalidName); err != nil {
			fmt.Printf("Could not remove %q: %v", invalidName, err)
		}
	})

	// Missing, create it
	_, info, err := sharedvalidation.ValidateDirectory(missing, true, sharedtemplates.AllTemplatesMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(missing); statErr != nil {
		t.Fatalf("directory was not created")
	}
	if info == nil {
		t.Fatalf("expected file info, got nil")
	}

	// Missing, invalid directory name
	_, info, err = sharedvalidation.ValidateDirectory(invalidName, true, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error, got: %v", err)
	}
	if _, statErr := os.Stat(invalidName); statErr == nil {
		t.Fatalf("directory was created, should have failed")
	}
	if info != nil {
		t.Fatalf("expected nil file info, got info")
	}
}

func TestValidateDirectory_ErrorIfMissing(t *testing.T) {
	tmp := t.TempDir()
	missing := tmp + "/missing"

	_, info, err := sharedvalidation.ValidateDirectory(missing, false, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error for missing directory, got nil")
	}
	if info != nil {
		t.Fatalf("expected nil os.FileInfo for missing, uncreated directory")
	}

}

func TestValidateDirectory_ValidTemplate(t *testing.T) {
	goodTag := "/path/{{video_title}}/file"

	_, info, err := sharedvalidation.ValidateDirectory(goodTag, false, sharedtemplates.AllTemplatesMap)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if info != nil {
		t.Fatalf("expected os.FileInfo for good template directories")
	}
}

func TestValidateDirectory_InvalidTemplate(t *testing.T) {
	// Unsupported tag: {{badtag}}
	dir := "/root/{{badtag}}/video"

	_, info, err := sharedvalidation.ValidateDirectory(dir, false, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error for invalid template tag")
	}
	if info != nil {
		t.Fatalf("expected nil os.Info, got info")
	}
}

// TestValidateFile runs checks for file validation -----------------------------------------------------------------------------
func TestValidateFile_ExistingFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "x.txt")
	t.Cleanup(func() {
		if err := os.Remove(f); err != nil {
			fmt.Printf("Could not remove %q: %v", f, err)
		}
	})

	if err := os.WriteFile(f, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, info, err := sharedvalidation.ValidateFile(f, false, sharedtemplates.AllTemplatesMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatalf("expected file info, got nil")
	}
}

func TestValidateFile_CreateIfMissing(t *testing.T) {
	tmp := t.TempDir()
	valid := tmp + "newfile.txt"
	invalid := tmp + "\x00"
	t.Cleanup(func() {
		if err := os.Remove(valid); err != nil {
			fmt.Printf("Could not remove file %q: %v", valid, err)
		}
		if err := os.Remove(invalid); err != nil {
			fmt.Printf("Could not remove file %q: %v", invalid, err)
		}
	})

	// Valid filename
	_, info, err := sharedvalidation.ValidateFile(valid, true, sharedtemplates.AllTemplatesMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(valid); statErr != nil {
		t.Fatalf("file was not created")
	}
	if info == nil {
		t.Fatalf("expected os.FileInfo, got nil")
	}

	// Invalid filename
	_, info, err = sharedvalidation.ValidateFile(invalid, true, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error, got: %v", err)
	}
	if _, statErr := os.Stat(invalid); statErr == nil {
		t.Fatalf("invalid file was created")
	}
	if info != nil {
		t.Fatalf("expected nil os.FileInfo, got info")
	}
}

func TestValidateFile_MissingNoCreate(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "does_not_exist.txt")

	if _, _, err := sharedvalidation.ValidateFile(f, false, sharedtemplates.AllTemplatesMap); err == nil {
		t.Fatalf("expected error for missing file without create flag")
	}
}

func TestValidateFile_PathIsDirectory(t *testing.T) {
	tmp := t.TempDir()

	_, _, err := sharedvalidation.ValidateFile(tmp, false, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error when path is a directory")
	}
}

func TestValidateFile_ValidTemplate(t *testing.T) {
	tmp := os.TempDir()
	goodTag := tmp + "/{{video_title}}.txt"

	_, info, err := sharedvalidation.ValidateFile(goodTag, false, sharedtemplates.AllTemplatesMap)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if info != nil {
		t.Fatalf("expected os.FileInfo for good template directories")
	}
}

func TestValidateFile_InvalidTemplate(t *testing.T) {
	// Unsupported tag: {{badtag}}
	tmp := os.TempDir()
	badTag := tmp + "/{{badtag}}.txt"

	_, info, err := sharedvalidation.ValidateFile(badTag, false, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error for invalid template tag")
	}
	if info != nil {
		t.Fatalf("expected nil os.Info, got info")
	}
}

// TestValidateMaxFilesize checks various values for validation -----------------------------------------------------------------
func TestValidateMaxFilesize(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		// GB
		{"1g", "1g", true},
		{"60gb", "60g", true},
		// MB
		{"10m", "10m", true},
		{"60MB", "60m", true},
		// KB
		{"500k", "500k", true},
		{"1000KB", "1000k", true},
		// Decimals, as yt-dlp takes.
		{"44.6M", "44.6m", true},
		{"1.5gb", "1.5g", true},
		// Bytes
		{"1B", "1", true},
		{"b", "", false},
		{"5BB", "", false},
		// Fail
		{"", "", true},
		{"1xb", "", false},
		{"abc", "", false},
		{"-5m", "", false}, // yt-dlp would refuse it, failing every download.
		{"1.", "", false},
		{".5g", "", false},
		{"1e5", "", false},
		{"inf", "", false},
	}

	for _, tc := range tests {
		got, err := validation.ValidateMaxFilesize(tc.input)
		if tc.ok && err != nil {
			t.Fatalf("input %q: unexpected error: %v", tc.input, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("input %q: expected error, got none", tc.input)
		}
		if got != tc.want {
			t.Fatalf("input %q: expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

// TestValidateNotifyURL tests a notify URL must be an http(s) URL with nothing a URL can't hold.
func TestValidateNotifyURL(t *testing.T) {
	if err := validation.ValidateNotifyURL("http://plex.local:32400/library/sections/1/refresh?X-Plex-Token=abc"); err != nil {
		t.Errorf("valid notify URL rejected: %v", err)
	}
	for _, bad := range []string{"", "plex.local/refresh", "http://example.com/%😃", "http://a.com/b|c", "http://a.com/b c"} {
		if err := validation.ValidateNotifyURL(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestValidateYtdlpOutputExtension ------------------------------------------------------------------------------------
func TestValidateYtdlpOutputExtension(t *testing.T) {
	for in, want := range map[string]string{
		"mp4":    "mp4",
		"mov":    "mov",
		"mkv":    "mkv",
		".mp4":   "mp4",
		".webm":  "webm",
		" MP4 ":  "mp4",
		".MKV":   "mkv",
		"":       "",
		"Same":   sharedconsts.ExtSame,
		" same ": sharedconsts.ExtSame,
	} {
		if got, err := validation.ValidateYtdlpOutputExtension(in); err != nil || got != want {
			t.Errorf("ValidateYtdlpOutputExtension(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	var invalidExts = []string{
		"mk4",
		".nope",
	}

	for _, v := range invalidExts {
		if _, err := validation.ValidateYtdlpOutputExtension(v); err == nil {
			t.Fatalf("invalid YTDLP extension passed")
		}
	}
}

// TestValidateExtraYtdlpArgs tests extra yt-dlp args are split, and refused if one is blocked or they can't be split.
func TestValidateExtraYtdlpArgs(t *testing.T) {
	if got, err := validation.ValidateExtraYtdlpArgs(`-f "bv*+ba" --no-mtime`); err != nil || !slices.Equal(got, []string{"-f", "bv*+ba", "--no-mtime"}) {
		t.Errorf("got %q, %v; want the args split", got, err)
	}
	if got, err := validation.ValidateExtraYtdlpArgs(""); err != nil || len(got) != 0 {
		t.Errorf("blank args: got %q, %v; want none", got, err)
	}
	for _, bad := range []string{"--exec 'rm -rf x'", "--no-mtime --config-location /tmp/x", `-f "unclosed`} {
		if _, err := validation.ValidateExtraYtdlpArgs(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestBlockedYtdlpArgsRefusedOnSave tests a settings save refuses extra yt-dlp args holding a blocked option.
func TestBlockedYtdlpArgsRefusedOnSave(t *testing.T) {
	blocked := "--exec 'touch x'"
	for name, in := range map[string]models.SettingsInput{
		"video":    {ExtraYTDLPVideoArgs: &blocked},
		"metadata": {ExtraYTDLPMetaArgs: &blocked},
		"crawl":    {ExtraYTDLPCrawlArgs: &blocked},
	} {
		if err := parsing.ApplySettingsInput(&models.Settings{}, &in); err == nil {
			t.Errorf("%s args: a blocked option was accepted", name)
		}
	}
	if err := validation.ValidateSettingsModel(&models.Settings{ExtraYTDLPCrawlArgs: blocked}, false); err == nil {
		t.Error("loaded settings with a blocked crawl option passed validation")
	}
}

// TestSameAndKeepChoices tests the 'Same as input' extensions and 'Keep' metadata files are accepted and kept as chosen.
func TestSameAndKeepChoices(t *testing.T) {
	// yt-dlp's extension.
	if got, err := validation.ValidateYtdlpOutputExtension(sharedconsts.ExtSame); err != nil || got != sharedconsts.ExtSame {
		t.Errorf("yt-dlp extension %q: got %q, %v", sharedconsts.ExtSame, got, err)
	}

	// Metarr's extension, with codecs, and metadata files.
	m := &models.MetarrArgs{OutputExt: sharedconsts.ExtSame, MetaPurge: "keep", TranscodeVideoCodecs: []string{"h264:hevc"}}
	if err := validation.ValidateMetarrArgsModel(m, true); err != nil {
		t.Errorf("ValidateMetarrArgsModel: %v", err)
	}
	if err := validation.ValidateMetarrArgsCombination(m); err != nil {
		t.Errorf("ValidateMetarrArgsCombination: %v", err)
	}
	if m.OutputExt != sharedconsts.ExtSame || m.MetaPurge != "keep" {
		t.Errorf("got output extension %q and purge %q, want %q and keep", m.OutputExt, m.MetaPurge, sharedconsts.ExtSame)
	}
}

// TestValidateFilterOps --------------------------------------------------------------------------------------
func TestValidateFilterOps_EmptySliceOK(t *testing.T) {
	if err := validation.ValidateFilterOps(nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateFilterOps_Valid(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "contains",
			MustAny:    "must",
			Value:      "foo",
		},
		{
			Field:      "channel",
			FilterType: "omits",
			MustAny:    "any",
			Value:      "bar",
		},
		{
			Field:      "duration",
			FilterType: "more-than",
			MustAny:    "must",
			Value:      "60",
		},
	}

	if err := validation.ValidateFilterOps(filters); err != nil {
		t.Fatalf("expected filters to be valid, got %v", err)
	}
}

func TestValidateFilterOps_InvalidFilterType(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "bad-type",
			MustAny:    "must",
			Value:      "foo",
		},
	}

	err := validation.ValidateFilterOps(filters)
	if err == nil {
		t.Fatalf("expected error for invalid FilterType")
	}
	if !strings.Contains(err.Error(), "invalid type") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilterOps_InvalidMustAny(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "contains",
			MustAny:    "wrong",
			Value:      "foo",
		},
	}

	err := validation.ValidateFilterOps(filters)
	if err == nil {
		t.Fatalf("expected error for invalid MustAny")
	}
	if !strings.Contains(err.Error(), "invalid condition") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilterOps_EmptyField(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "",
			FilterType: "contains",
			MustAny:    "must",
			Value:      "foo",
		},
	}

	err := validation.ValidateFilterOps(filters)
	if err == nil {
		t.Fatalf("expected error for empty field")
	}
	if !strings.Contains(err.Error(), "empty field") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestValidateMetaFilterMoveOps ----------------------------------------------------------------------------------------
func TestValidateMetaFilterMoveOps_EmptySliceOK(t *testing.T) {
	if err := validation.ValidateMetaFilterMoveOps(nil, true); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateMetaFilterMoveOps_Valid(t *testing.T) {
	tmp := t.TempDir()

	moveOps := []models.MetaFilterMoveOps{
		{
			Filters:   []models.Filters{{Field: "channel", FilterType: "contains", MustAny: "must"}},
			OutputDir: tmp,
		},
	}

	if err := validation.ValidateMetaFilterMoveOps(moveOps, true); err != nil {
		t.Fatalf("expected moveOps to be valid, got %v", err)
	}
}

func TestValidateMetaFilterMoveOps_EmptyField(t *testing.T) {
	tmp := t.TempDir()

	moveOps := []models.MetaFilterMoveOps{
		{
			Filters:   []models.Filters{{FilterType: "contains", MustAny: "must"}},
			OutputDir: tmp,
		},
	}

	err := validation.ValidateMetaFilterMoveOps(moveOps, true)
	if err == nil {
		t.Fatalf("expected error for empty field")
	}
	if !strings.Contains(err.Error(), "empty field") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestValidateMetaFilterMoveOps_NoFilters tests a move op with no filters is refused, rather than matching every video.
func TestValidateMetaFilterMoveOps_NoFilters(t *testing.T) {
	err := validation.ValidateMetaFilterMoveOps([]models.MetaFilterMoveOps{{OutputDir: t.TempDir()}}, true)
	if err == nil || !strings.Contains(err.Error(), "no filters") {
		t.Fatalf("expected an error for no filters, got %v", err)
	}
}

func TestValidateMetaFilterMoveOps_InvalidDirectory(t *testing.T) {
	// A file where a directory should be is invalid, as is a missing directory (it may be on an unmounted drive).
	tmp := t.TempDir()
	file := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	moveOps := []models.MetaFilterMoveOps{
		{
			Filters:   []models.Filters{{Field: "channel", FilterType: "contains", MustAny: "must"}},
			OutputDir: file,
		},
	}

	err := validation.ValidateMetaFilterMoveOps(moveOps, true)
	if err == nil {
		t.Fatalf("expected error for invalid directory")
	}
	// Don't assert too tightly on the message; just ensure it's about directory
	if !strings.Contains(err.Error(), "invalid directory") && !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// Loading doesn't check paths, so one channel's folder becoming a file can't stop every channel loading.
	if err := validation.ValidateMetaFilterMoveOps(moveOps, false); err != nil {
		t.Fatalf("expected paths to be left unchecked, got %v", err)
	}

	moveOps[0].OutputDir = filepath.Join(tmp, "missing_dir")
	if err := validation.ValidateMetaFilterMoveOps(moveOps, true); err == nil {
		t.Fatal("expected a missing directory to be refused")
	}
	if _, err := os.Stat(moveOps[0].OutputDir); err == nil {
		t.Error("validation created the missing directory")
	}
}

// TestValidateMetarrArgsModelGPUNode tests that a missing GPU node fails saving but not loading.
func TestValidateMetarrArgsModelGPUNode(t *testing.T) {
	m := &models.MetarrArgs{TranscodeGPU: "vaapi", TranscodeGPUNode: filepath.Join(t.TempDir(), "missing-renderD128")}
	if err := validation.ValidateMetarrArgsModel(m, false); err != nil {
		t.Errorf("loading with a missing GPU node failed: %v", err)
	}
	if err := validation.ValidateMetarrArgsModel(m, true); err == nil {
		t.Error("saving with a missing GPU node succeeded")
	}
}

// TestValidateMetarrArgsModelUnsupportedGPU tests a GPU type this OS doesn't support is left out, as its warning says.
func TestValidateMetarrArgsModelUnsupportedGPU(t *testing.T) {
	for _, accel := range []string{sharedconsts.AccelTypeAMF, sharedconsts.AccelTypeVideoToolbox} {
		if sharedvalidation.OSSupportsAccelType(accel) {
			continue
		}
		m := &models.MetarrArgs{TranscodeGPU: accel}
		if err := validation.ValidateMetarrArgsModel(m, false); err != nil {
			t.Fatalf("%s: %v", accel, err)
		}
		if m.TranscodeGPU != "" {
			t.Errorf("unsupported GPU %q kept as %q", accel, m.TranscodeGPU)
		}
	}
}

// TestBadSettingsKeptForTheirErrors tests a setting that fails validation is named in the error and left as it was, so
// the edit page still shows it.
func TestBadSettingsKeptForTheirErrors(t *testing.T) {
	const bad = "bogus-value"
	var s models.Settings
	for name, field := range map[string]*string{
		"from date":        &s.FromDate,
		"to date":          &s.ToDate,
		"max filesize":     &s.MaxFilesize,
		"max resolution":   &s.MaxResolution,
		"yt-dlp extension": &s.YtdlpOutputExt,
	} {
		s = models.Settings{}
		*field = bad
		if err := validation.ValidateSettingsModel(&s, false); err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%s: got %v, want an error naming %q", name, err, bad)
		}
		if *field != bad {
			t.Errorf("%s: left as %q, want %q kept", name, *field, bad)
		}
	}

	m := &models.MetarrArgs{MetaPurge: bad}
	if err := validation.ValidateMetarrArgsModel(m, false); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("metadata purge: got %v, want an error naming %q", err, bad)
	}
	if m.MetaPurge != bad {
		t.Errorf("metadata purge: left as %q, want %q kept", m.MetaPurge, bad)
	}
}

// TestValidateMetarrArgsModelStoredDeleteDateTag tests that a delete-date-tag stored with the location and format it
// once took still loads, and is written back without them.
func TestValidateMetarrArgsModelStoredDeleteDateTag(t *testing.T) {
	m := &models.MetarrArgs{
		MetaOps:     []models.MetaOps{{Field: "title", OpType: "delete-date-tag", OpLoc: "all", DateFormat: "ymd"}},
		FilenameOps: []models.FilenameOps{{OpType: "delete-date-tag", OpLoc: "prefix", DateFormat: "Ymd"}},
	}
	if err := validation.ValidateMetarrArgsModel(m, false); err != nil {
		t.Fatalf("loading a stored delete-date-tag failed: %v", err)
	}
	if got := sharedparsing.FormatMetaOps(m.MetaOps); len(got) != 1 || got[0] != "title:delete-date-tag" {
		t.Errorf("meta op written back as %q", got)
	}
	if got := sharedparsing.FormatFilenameOps(m.FilenameOps); len(got) != 1 || got[0] != "delete-date-tag" {
		t.Errorf("filename op written back as %q", got)
	}
}

// TestValidateFilteredMetaOps ----------------------------------------------------------------------------------------
func TestValidateFilteredMetaOps_EmptySliceOK(t *testing.T) {
	if err := sharedvalidation.ValidateFilteredMetaOps(nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateFilteredMetaOps_Valid(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "contains",
			MustAny:    "must",
			Value:      "foo",
		},
	}
	metaOps := []models.MetaOps{
		{
			Field:        "title",
			OpType:       sharedconsts.OpReplace,
			OpFindString: "foo",
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: filters,
			MetaOps: metaOps,
		},
	}

	if err := sharedvalidation.ValidateFilteredMetaOps(input); err != nil {
		t.Fatalf("expected valid filtered meta ops, got %v", err)
	}
}

func TestValidateFilteredMetaOps_InvalidFilters(t *testing.T) {
	// invalid filter: bad FilterType
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "bad",
			MustAny:    "must",
		},
	}
	metaOps := []models.MetaOps{
		{
			Field:        "title",
			OpType:       sharedconsts.OpReplace,
			OpFindString: "foo",
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: filters,
			MetaOps: metaOps,
		},
	}

	err := sharedvalidation.ValidateFilteredMetaOps(input)
	if err == nil {
		t.Fatalf("expected error for invalid filters")
	}
	if !strings.Contains(err.Error(), "invalid filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredMetaOps_NoFilters(t *testing.T) {
	metaOps := []models.MetaOps{
		{
			Field:        "title",
			OpType:       sharedconsts.OpReplace,
			OpFindString: "foo",
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: nil,
			MetaOps: metaOps,
		},
	}

	err := sharedvalidation.ValidateFilteredMetaOps(input)
	if err == nil {
		t.Fatalf("expected error for no filters")
	}
	if !strings.Contains(err.Error(), "has no filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredMetaOps_NoMetaOps(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "contains",
			MustAny:    "must",
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: filters,
			MetaOps: nil,
		},
	}

	err := sharedvalidation.ValidateFilteredMetaOps(input)
	if err == nil {
		t.Fatalf("expected error for no meta operations")
	}
	if !strings.Contains(err.Error(), "has no meta operations") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestValidateFilteredFilenameOps -----------------------------------------------------------------------------------------------
func TestValidateFilteredFilenameOps_EmptySliceOK(t *testing.T) {
	if err := sharedvalidation.ValidateFilteredFilenameOps(nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateFilteredFilenameOps_Valid(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "contains",
			MustAny:    "must",
			Value:      "foo",
		},
	}
	filenameOps := []models.FilenameOps{
		{
			OpType:       sharedconsts.OpReplace,
			OpFindString: "foo",
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     filters,
			FilenameOps: filenameOps,
		},
	}

	if err := sharedvalidation.ValidateFilteredFilenameOps(input); err != nil {
		t.Fatalf("expected valid filtered filename ops, got %v", err)
	}
}

func TestValidateFilteredFilenameOps_InvalidFilters(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "bad",
			MustAny:    "must",
		},
	}
	filenameOps := []models.FilenameOps{
		{
			OpType:       sharedconsts.OpReplace,
			OpFindString: "foo",
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     filters,
			FilenameOps: filenameOps,
		},
	}

	err := sharedvalidation.ValidateFilteredFilenameOps(input)
	if err == nil {
		t.Fatalf("expected error for invalid filters")
	}
	if !strings.Contains(err.Error(), "invalid filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredFilenameOps_NoFilters(t *testing.T) {
	filenameOps := []models.FilenameOps{
		{
			OpType:       sharedconsts.OpReplace,
			OpFindString: "foo",
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     nil,
			FilenameOps: filenameOps,
		},
	}

	err := sharedvalidation.ValidateFilteredFilenameOps(input)
	if err == nil {
		t.Fatalf("expected error for no filters")
	}
	if !strings.Contains(err.Error(), "has no filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredFilenameOps_NoFilenameOps(t *testing.T) {
	filters := []models.Filters{
		{
			Field:      "title",
			FilterType: "contains",
			MustAny:    "must",
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     filters,
			FilenameOps: nil,
		},
	}

	err := sharedvalidation.ValidateFilteredFilenameOps(input)
	if err == nil {
		t.Fatalf("expected error for no filename operations")
	}
	if !strings.Contains(err.Error(), "has no filename operations") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestValidateToFromDate -----------------------------------------------------------------------------------------------------
func TestValidateToFromDate_EmptyOK(t *testing.T) {
	got, err := validation.ValidateToFromDate("")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestValidateToFromDate_Today(t *testing.T) {
	got, err := validation.ValidateToFromDate("today")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Now().Format("20060102")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestValidateToFromDate_TodayCaseInsensitive(t *testing.T) {
	got, err := validation.ValidateToFromDate("ToDaY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("expected 8-char date, got %q", got)
	}
}

func TestValidateToFromDate_RawYYYYMMDD(t *testing.T) {
	got, err := validation.ValidateToFromDate("20241231")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "20241231" {
		t.Fatalf("expected 20241231, got %q", got)
	}
}

func TestValidateToFromDate_WithMarkers(t *testing.T) {
	got, err := validation.ValidateToFromDate("2025y12m31d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "20251231" {
		t.Fatalf("expected 20251231, got %q", got)
	}
}

func TestValidateToFromDate_NoYearDefaultsToCurrent(t *testing.T) {
	year := time.Now().Year()
	got, err := validation.ValidateToFromDate("12m31d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPrefix := strconv.Itoa(year)
	if !strings.HasPrefix(got, expectedPrefix) {
		t.Fatalf("expected year prefix %q, got %q", expectedPrefix, got)
	}
	if !strings.HasSuffix(got, "1231") {
		t.Fatalf("expected month/day 1231, got %q", got)
	}
}

func TestValidateToFromDate_SingleDigitMonthDayPadded(t *testing.T) {
	got, err := validation.ValidateToFromDate("2025y6m3d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "20250603" {
		t.Fatalf("expected 20250603, got %q", got)
	}
}

func TestValidateToFromDate_InvalidFormat(t *testing.T) {
	in := "nonsense"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateToFromDate_InvalidMonth(t *testing.T) {
	// becomes "20251301" after cleaning; regex should parse and range check
	in := "2025-13-01"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid month")
	}
	if !strings.Contains(err.Error(), "invalid month") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateToFromDate_InvalidDay(t *testing.T) {
	// 32 > 31
	in := "2025y12m32d"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid day")
	}
	if !strings.Contains(err.Error(), "invalid day") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateToFromDate_InvalidYear(t *testing.T) {
	in := "0999y12m31d"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid year")
	}
	if !strings.Contains(err.Error(), "invalid year") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateGPU(t *testing.T) {
	// An alias is checked for OS support as what it stands for, so "amd" as "amf".
	amdGPU := ""
	if sharedvalidation.OSSupportsAccelType(sharedconsts.AccelTypeAMF) {
		amdGPU = sharedconsts.AccelTypeAMF
	}

	tests := []struct {
		gpu       string
		expectGPU string
		gpuMatch  bool
		ok        bool
	}{
		// Pass.
		{"auto", "auto", true, true},
		{"", "", true, true},
		{"nvidia", "cuda", true, true},
		{"amd", amdGPU, true, true},

		// Fail.
		{"fake", "", true, false},
	}

	for _, tt := range tests {
		gpu, err := validation.ValidateGPUAcceleration(tt.gpu)
		if err != nil && tt.ok {
			t.Fatalf("Expected pass, failed with error: %q", err)
		}
		if gpu != tt.expectGPU && tt.gpuMatch {
			t.Fatalf("Expected %q, got %q", tt.expectGPU, gpu)
		}
	}
}

func TestValidateGPUNode(t *testing.T) {
	// Create a temporary file to simulate a device node
	tmpFile, err := os.CreateTemp("", "gpu-node-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			fmt.Printf("Failed to remove temp file: %v", err)
		}
	}()
	if err := tmpFile.Close(); err != nil {
		fmt.Printf("Failed to close temp file: %v", err)
	}

	tests := []struct {
		name         string
		accelType    string
		nodePath     string
		expectedNode string
		expectError  bool
	}{
		// Pass cases
		{"auto with valid path", sharedconsts.AccelTypeAuto, tmpFile.Name(), tmpFile.Name(), false},
		{"cuda with valid path", sharedconsts.AccelTypeCuda, tmpFile.Name(), tmpFile.Name(), false},
		{"vaapi with valid path", sharedconsts.AccelTypeVAAPI, tmpFile.Name(), tmpFile.Name(), false},
		{"empty type with valid path", "", tmpFile.Name(), tmpFile.Name(), false},
		{"empty type with empty path", "", "", "", false},
		{"auto with empty path", sharedconsts.AccelTypeAuto, "", "", false},
		{"cuda with empty path", sharedconsts.AccelTypeCuda, "", "", false},

		// Fail cases
		{"vaapi with empty path", sharedconsts.AccelTypeVAAPI, "", "", true},
		{"auto with non-existent path", sharedconsts.AccelTypeAuto, "/dev/nonexistent-gpu-12345", "", true},
		{"vaapi with non-existent path", sharedconsts.AccelTypeVAAPI, "/dev/nonexistent-gpu-12345", "", true},
		{"cuda with non-existent path", sharedconsts.AccelTypeCuda, "/dev/nonexistent-gpu-12345", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpuNode, err := sharedvalidation.ValidateAccelTypeDeviceNode(tt.accelType, tt.nodePath)

			if tt.expectError && err == nil {
				t.Fatalf("Expected error, but got none")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("Expected pass, but got error: %q", err)
			}
			if !tt.expectError && gpuNode != tt.expectedNode {
				t.Fatalf("Expected %q, got %q", tt.expectedNode, gpuNode)
			}
		})
	}
}
