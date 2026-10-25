package downloaders

import (
	"math"
	"testing"
	"tubarr/internal/domain/consts"
)

// TestYTDLPProgressParser tests parsing of Tubarr's yt-dlp progress template lines.
func TestYTDLPProgressParser(t *testing.T) {
	tests := []struct {
		line      string
		want      float64
		wantSpeed float64
		wantOK    bool
	}{
		{"[tubarr-progress] 523264 1051142 NA NA NA 150000.5", 523264.0 / 1051142 * 100, 150000.5, true},
		{"[tubarr-progress] 1051142 1051142 NA NA NA NA", 100, 0, true},
		{"[tubarr-progress] 0 1051142 NA NA NA NA", 0, 0, true},
		{"[tubarr-progress] 250 NA 1000.5 NA NA 2048", 250 / 1000.5 * 100, 2048, true},
		{"[tubarr-progress] 4096 NA NA 3 12 1e6", 25, 1e6, true},
		{"[tubarr-progress] 2000 1000 NA NA NA NA", 100, 0, true},
		{"  [tubarr-progress] 500 1000 NA NA NA NA\r", 50, 0, true},
		{"[tubarr-progress] 4096 NA NA NA NA 2048", 0, 0, false},
		{"[tubarr-progress] 523264 1051142 NA NA NA", 0, 0, false},
		{"[tubarr-progress] 4096 1000", 0, 0, false},
		{"[download]  49.8% of 1.00MiB at 2.00MiB/s ETA 00:00", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tt := range tests {
		got, speed, ok := YTDLPProgressParser(tt.line)
		if ok != tt.wantOK || got != tt.want || speed != tt.wantSpeed {
			t.Errorf("YTDLPProgressParser(%q) = (%v, %v, %v), want (%v, %v, %v)", tt.line, got, speed, ok, tt.want, tt.wantSpeed, tt.wantOK)
		}
	}
}

// TestExternalProgressParsers tests per-downloader progress parsing through ProgressParser.
func TestExternalProgressParsers(t *testing.T) {
	tests := []struct {
		downloader string
		line       string
		want       float64
		wantOK     bool
	}{
		{"axel", "[ 45%]  .......... .......... .......... .......... ..........  [ 931.0KB/s]", 45, true},
		{"axel", "[100%]  .......... ..........  [1.2MB/s]", 100, true},
		{"axel", "File size: 5.00061 Megabyte(s) (5243523 bytes)", 0, false},
		{"curl", " 23 5120k   23 1183k    0     0   452k      0  0:00:11  0:00:02  0:00:09  451k", 23, true},
		{"curl", "100 5120k  100 5120k    0     0   752k      0  0:00:06  0:00:06 --:--:--  939k", 100, true},
		{"curl", "  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current", 0, false},
		{"curl", "                                 Dload  Upload   Total   Spent    Left  Speed", 0, false},
		{"curl", "#########################                                     41.7%", 41.7, true},
		{"ffmpeg", "out_time_us=3934000", 39.34, true},
		{"ffmpeg", "out_time_us=N/A", 0, false},
		{"ffmpeg", "progress=end", 100, true},
		{"ffmpeg", "progress=continue", 0, false},
		{"ffmpeg", "frame=119", 0, false},
		{"wget", "   384K 14% 1.14M 3s", 14, true},
		{"wget", "  4992K100%  178M=4.9s2026-10-15 22:43:56 URL:https://a.example/v.webm [5243523/5243523] -> \"v.webm.part\" [1]", 100, true},
		{"wget", "     0K  7% 1.32M 4s", 7, true},
		{"wget", "[ 45%] looks like axel", 0, false},
		{"/usr/bin/aria2c", "[#7b70f9 1.1MiB/5.0MiB(23%) CN:5 DL:717KiB ETA:5s]", 23, true},
		{"", "[tubarr-progress] 500 1000 NA NA NA NA", 50, true},
		{"", "[ 45%] not native output", 0, false},
	}
	for _, tt := range tests {
		got, _, ok := NewProgressParser(tt.downloader, 10).Parse(tt.line)
		if ok != tt.wantOK || (ok && math.Abs(got-tt.want) > 1e-9) {
			t.Errorf("%s: Parse(%q) = (%v, %v), want (%v, %v)", tt.downloader, tt.line, got, ok, tt.want, tt.wantOK)
		}
	}

	// ffmpeg needs a duration to give a percentage.
	if _, _, ok := NewProgressParser("ffmpeg", 0).Parse("out_time_us=3934000"); ok {
		t.Error("ffmpeg without duration: parsed progress, want none")
	}
}

// TestName tests resolving external downloader settings to downloader names.
func TestName(t *testing.T) {
	tests := map[string]string{
		"aria2c":              "aria2c",
		"/usr/bin/aria2c":     "aria2c",
		"m3u8:ffmpeg":         "ffmpeg",
		"dash:/opt/ff/ffmpeg": "ffmpeg",
		`C:\tools\curl.exe`:   "curl",
		"http":                "httpie",
		"Wget":                "wget",
		"axel":                "axel",
		"native":              "native",
		"":                    "",
	}
	for in, want := range tests {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAria2OutputParser tests parsing of aria2c console readouts, with and without a batch notice.
func TestAria2OutputParser(t *testing.T) {
	readout := "[#7b70f9 1.1MiB/5.0MiB(23%) CN:5 DL:717KiB ETA:5s]"

	// No "Downloading N item(s)" notice (yt-dlp's default aria2c invocation).
	if ok, _, _, pct, _ := Aria2OutputParser(readout, 0, 0, 0, consts.DLStatusQueued); !ok || pct != 23 {
		t.Errorf("readout without batch notice: got (%v, %v), want (true, 23)", ok, pct)
	}

	// Batch notice, then readout.
	ok, total, _, _, _ := Aria2OutputParser("10/15 22:08:24 [NOTICE] Downloading 1 item(s)", 0, 0, 0, consts.DLStatusQueued)
	if !ok || total != 1 {
		t.Fatalf("batch notice: got (%v, %d), want (true, 1)", ok, total)
	}
	if ok, _, _, pct, _ := Aria2OutputParser(readout, total, 0, 0, consts.DLStatusDownloading); !ok || pct != 23 {
		t.Errorf("readout after batch notice: got (%v, %v), want (true, 23)", ok, pct)
	}

	// Multi-item batches track completed items.
	if ok, _, done, pct, _ := Aria2OutputParser("[NOTICE] Download complete: /a.part", 4, 1, 25, consts.DLStatusDownloading); !ok || done != 2 || pct != 50 {
		t.Errorf("multi-item completion: got (%v, %d, %v), want (true, 2, 50)", ok, done, pct)
	}
}
