package validation_test

import (
	"testing"

	"tubarr/internal/models"
	"tubarr/internal/validation"
)

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

// TestExtraFFmpegArgsOnCopiedAudio tests audio options are refused when audio is only ever copied, and left alone otherwise.
func TestExtraFFmpegArgsOnCopiedAudio(t *testing.T) {
	for _, c := range []struct {
		name  string
		extra string
		audio []string
		ext   string
		ok    bool
	}{
		{name: "filter with no codec", extra: "-af volume=0.5"},
		{name: "filter with plain copy", extra: "-filter:a volume=0.5", audio: []string{"copy"}},
		{name: "bitrate with no codec", extra: "-b:a 128k"},
		{name: "first stream's bitrate with no codec", extra: "-b:a:0 128k"},
		{name: "channels with no codec", extra: "-ac 1"},
		{name: "sample rate with plain copy and a container", extra: "-ar 44100", audio: []string{"copy"}, ext: ".mp4"},
		{name: "quality with no codec", extra: "-q:a 2"},

		{name: "filter with a codec", extra: "-af volume=0.5", audio: []string{"aac"}, ok: true},
		{name: "filter with a pair, which depends on the video", extra: "-af volume=0.5", audio: []string{"dts:ac3"}, ok: true},
		{name: "filter when the container may force a codec", extra: "-af volume=0.5", ext: ".mp4", ok: true},
		{name: "bitrate with a codec", extra: "-b:a 128k", audio: []string{"aac"}, ok: true},
		{name: "settings that don't touch audio", extra: "-movflags +faststart -threads 1", ok: true},

		// Fail on FFmpeg 9 with these builds, but that could change.
		{name: "-vsync", extra: "-vsync cfr", ok: true},
		{name: "Opus with a rate it refuses here", extra: "-ar 44100", audio: []string{"opus"}, ok: true},
		{name: "Opus with a quality it refuses here", extra: "-q:a 2", audio: []string{"opus"}, ok: true},
		{name: "AAC filter on AC-3", extra: "-bsf:a aac_adtstoasc", audio: []string{"ac3"}, ok: true},
		{name: "HEVC tag", extra: "-tag:v hvc1", ok: true},
	} {
		m := &models.MetarrArgs{ExtraFFmpegArgs: c.extra, TranscodeAudioCodecs: c.audio, OutputExt: c.ext}
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
