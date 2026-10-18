package downloads

import (
	"regexp"
	"testing"
)

// TestYTDLPCodecRegex tests each codec's pattern matches the codec strings yt-dlp reports for it (the names its
// parse_codecs accepts, with their leading zeros), and not other codecs'.
func TestYTDLPCodecRegex(t *testing.T) {
	reported := map[string][]string{
		"av1":    {"av01.0.08M.08", "av1"},
		"h264":   {"avc1.640028", "avc3.4D401F", "h264", "x264"},
		"hevc":   {"hvc1.2.4.L153.B0", "hev1.1.6.L93.B0", "hev2", "hevc", "h265"},
		"vp8":    {"vp08.00.41.08", "vp8"},
		"vp9":    {"vp09.00.40.08", "vp9"},
		"aac":    {"mp4a.40.2", "aac"},
		"ac3":    {"ac-3", "ac3"},
		"eac3":   {"ec-3", "eac3"},
		"dts":    {"dtsc", "dtse", "dtsh", "dtsl"},
		"flac":   {"flac"},
		"mp3":    {"mp3"},
		"opus":   {"opus"},
		"vorbis": {"vorbis"},
	}
	for codec := range reported {
		re := regexp.MustCompile(ytdlpCodecRegex(codec))
		for other, otherStrs := range reported {
			for _, s := range otherStrs {
				if matched := re.MatchString(s); matched != (other == codec) {
					t.Errorf("%s pattern %q matching %q (%s) = %t", codec, re, s, other, matched)
				}
			}
		}
	}
	if got := ytdlpCodecRegex("unknown"); got != "unknown" {
		t.Errorf("unknown codec pattern = %q, want the name as-is", got)
	}
}

// TestBuildCodecLists tests the format preferences fall back through each codec in order to the best format.
func TestBuildCodecLists(t *testing.T) {
	if got, want := buildVideoCodecList([]string{"hevc", "h264"}), `(bv*[vcodec~='^([hx]265|he?vc?)']/bv*[vcodec~='^([hx]264|avc)']/bv*)`; got != want {
		t.Errorf("video preference = %s, want %s", got, want)
	}
	if got, want := buildAudioCodecList([]string{"eac3", "aac"}), `(ba[acodec~='^e-?a?c-?3']/ba[acodec~='^(aac|mp4a)']/ba)`; got != want {
		t.Errorf("audio preference = %s, want %s", got, want)
	}
}
