package downloads

import (
	"bufio"
	"context"
	"os/exec"
	"testing"
	"time"
	"tubarr/internal/domain/command"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/models"
)

const (
	liveTestVideoURL      = "https://test-videos.co.uk/vids/bigbuckbunny/webm/vp9/1080/Big_Buck_Bunny_1080_10s_5MB.webm"
	liveTestVideoDuration = 10
)

// TestLiveDownloadProgress downloads a real file through each available downloader and checks progress is parsed.
func TestLiveDownloadProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live download in short mode")
	}
	if _, err := exec.LookPath(command.YTDLP); err != nil {
		t.Skip("yt-dlp not installed")
	}

	tests := []struct {
		name             string
		exe              string // External downloader, "" for native.
		dlArgs           string
		ytdlpArgs        string
		wantIntermediate bool // httpie progress is captured by yt-dlp, so only the final report arrives.
	}{
		{name: "native", ytdlpArgs: "--limit-rate 1M", wantIntermediate: true},
		{name: command.DownloaderAria, exe: command.DownloaderAria, dlArgs: "--max-overall-download-limit=1M", wantIntermediate: true},
		{name: command.DownloaderAxel, exe: command.DownloaderAxel, dlArgs: "-s 1000000", wantIntermediate: true},
		{name: command.DownloaderCurl, exe: command.DownloaderCurl, dlArgs: "--limit-rate 1M", wantIntermediate: true},
		{name: command.DownloaderFFmpeg, exe: command.DownloaderFFmpeg, wantIntermediate: true},
		{name: command.DownloaderWget, exe: command.DownloaderWget, dlArgs: "--limit-rate=1m", wantIntermediate: true},
		{name: command.DownloaderHttpie, exe: "http"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.exe != "" {
				if _, err := exec.LookPath(tt.exe); err != nil {
					t.Skipf("%s not installed", tt.exe)
				}
			}
			checkLiveProgress(t, tt.exe, tt.dlArgs, tt.ytdlpArgs, tt.wantIntermediate)
		})
	}
}

// checkLiveProgress runs Tubarr's yt-dlp video command, feeding its output through the progress parser.
func checkLiveProgress(t *testing.T, downloader, dlArgs, ytdlpArgs string, wantIntermediate bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	d := &VideoDownload{
		Video:   &models.Video{URL: liveTestVideoURL, VideoDir: t.TempDir(), Duration: liveTestVideoDuration},
		Channel: &models.Channel{},
		ChannelURL: &models.ChannelURL{ChanURLSettings: &models.Settings{
			ExternalDownloader:     downloader,
			ExternalDownloaderArgs: dlArgs,
			ExtraYTDLPVideoArgs:    ytdlpArgs,
		}},
		Context: ctx,
	}
	cmd := d.buildVideoCommand()
	cleanup, err := d.setDownloaderEnv(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var (
		pcts     []float64
		progress = downloaders.NewProgressParser(downloader, d.durationSeconds())
		scanner  = bufio.NewScanner(stdout)
	)
	scanner.Split(scanLinesOrCR)
	for scanner.Scan() {
		if pct, _, ok := progress.Parse(scanner.Text()); ok {
			pcts = append(pcts, pct)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("yt-dlp failed: %v\n%s", err, cmd.String())
	}

	t.Logf("parsed progress: %v", pcts)
	if len(pcts) == 0 {
		t.Fatal("no progress parsed")
	}
	if wantIntermediate {
		var intermediate bool
		for _, p := range pcts {
			if p > 0 && p < 100 {
				intermediate = true
				break
			}
		}
		if !intermediate {
			t.Errorf("no intermediate progress between 0%% and 100%% parsed: %v", pcts)
		}
	}
	if last := pcts[len(pcts)-1]; last != 100 {
		t.Errorf("final progress = %v, want 100", last)
	}
}
