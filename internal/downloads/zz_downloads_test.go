package downloads

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
	_ "github.com/mattn/go-sqlite3"
)

// ******************************** Commands ******************************************************************************************

// TestCommandsLeaveRetriesToTubarr tests that Download Retries isn't passed to yt-dlp, as Tubarr runs the retries itself.
func TestCommandsLeaveRetriesToTubarr(t *testing.T) {
	vd, jd := newTestDownloads(t.Context(), t, "https://retries.example/v", 1)
	vd.ChannelURL.ChanURLSettings.Retries = 5
	jd.ChannelURL.ChanURLSettings.Retries = 5

	if args, _ := vd.buildVideoCommand(); slices.Contains(args.Args, "--retries") {
		t.Errorf("video command passes --retries to yt-dlp: %q", args)
	}
	if args, _ := jd.buildJSONCommand(); slices.Contains(args.Args, "--retries") {
		t.Errorf("JSON command passes --retries to yt-dlp: %q", args)
	}
}

// TestCommandsCombineCookies tests that a cookie file and a browser's cookies are both passed, for yt-dlp to combine.
func TestCommandsCombineCookies(t *testing.T) {
	vd, jd := newTestDownloads(t.Context(), t, "https://cookies.example/v", 1)
	for _, cu := range []*models.ChannelURL{vd.ChannelURL, jd.ChannelURL} {
		cu.Session.CookiePath = "/tmp/cookies.txt"
		cu.ChanURLSettings.CookiesFromBrowser = "firefox"
	}

	for what, build := range map[string]func() (*exec.Cmd, error){
		"video": vd.buildVideoCommand,
		"JSON":  jd.buildJSONCommand,
	} {
		cmd, err := build()
		if err != nil {
			t.Fatalf("building %s command: %v", what, err)
		}

		args := cmd.Args
		if !slices.Contains(args, "--cookies") || !slices.Contains(args, "--cookies-from-browser") {
			t.Errorf("%s command doesn't pass both the cookie file and the browser: %q", what, args)
		}
	}
}

// TestVideoCommandNamedAfterJSON tests the video is named after its JSON file, so a title changed since can't make them differ.
func TestVideoCommandNamedAfterJSON(t *testing.T) {
	tests := []struct {
		name       string
		jsonFile   string
		customFile string
		want       string
	}{
		{"no JSON", "", "", command.FilenameSyntax},
		{"yt-dlp JSON", "/json/Old_Title [abc123].info.json", "", "Old_Title [abc123].%(ext)s"},
		{"custom JSON wins", "/json/Old_Title [abc123].info.json", "/json/Scraped [1a2b3c4d].json", "Scraped [1a2b3c4d].%(ext)s"},
		{"percent escaped", "/json/100% [abc123].info.json", "", "100%% [abc123].%(ext)s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vd, _ := newTestDownloads(t.Context(), t, "https://name.example/v", 1)
			vd.Video.Files.JSONFilePath = tt.jsonFile
			vd.Video.Files.JSONCustomFile = tt.customFile

			cmd, err := vd.buildVideoCommand()
			if err != nil {
				t.Fatal(err)
			}
			i := slices.Index(cmd.Args, command.Output)
			if i < 0 || i+1 >= len(cmd.Args) {
				t.Fatalf("no %s in video command: %q", command.Output, cmd.Args)
			}
			if got := cmd.Args[i+1]; got != tt.want {
				t.Errorf("output template %q, want %q", got, tt.want)
			}
		})
	}
}

// TestVideoCommandExtSame tests an extension is passed to yt-dlp as its merge output format, and 'same' passes none.
func TestVideoCommandExtSame(t *testing.T) {
	vd, _ := newTestDownloads(t.Context(), t, "https://ext.example/v", 1)

	// An extension.
	vd.ChannelURL.ChanURLSettings.YtdlpOutputExt = "mkv"
	if args, _ := vd.buildVideoCommand(); !slices.Contains(args.Args, command.YtDLPOutputExtension) {
		t.Errorf("mkv not passed to yt-dlp: %q", args)
	}

	// Same as input.
	vd.ChannelURL.ChanURLSettings.YtdlpOutputExt = sharedconsts.ExtSame
	if args, _ := vd.buildVideoCommand(); slices.Contains(args.Args, command.YtDLPOutputExtension) {
		t.Errorf("%q passed a merge output format to yt-dlp: %q", sharedconsts.ExtSame, args)
	}
}

// ******************************** Download helpers **********************************************************************************

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

// ******************************** ETA ***********************************************************************************************

// TestETA tests download ETAs from the average rate.
func TestETA(t *testing.T) {
	const id = 9001
	now := time.Now()
	TrackETA(id, consts.DLStatusDownloading, 10, now.Add(-20*time.Second))
	TrackETA(id, consts.DLStatusDownloading, 30, now)

	// 20% in 20 seconds leaves 70% at 1% a second.
	if got := ETA(id, 30); got < 69 || got > 71 {
		t.Errorf("ETA at 30%% = %d, want about 70", got)
	}
	if got := ETA(id, 11); got != -1 {
		t.Errorf("ETA 1%% in = %d, want -1 (too early)", got)
	}

	// A drop (e.g. yt-dlp starting the audio file) restarts the estimate.
	TrackETA(id, consts.DLStatusDownloading, 2, now)
	if got := ETA(id, 2); got != -1 {
		t.Errorf("ETA after restarting = %d, want -1", got)
	}

	TrackETA(id, consts.DLStatusCompleted, 100, now)
	if got := ETA(id, 100); got != -1 {
		t.Errorf("ETA after finishing = %d, want -1", got)
	}

	// Revised size estimates dip the percentage without restarting the estimate.
	const wobbly = 9003
	TrackETA(wobbly, consts.DLStatusDownloading, 23.5, now.Add(-27*time.Second))
	for _, pct := range []float64{25.5, 24.3, 26.4, 25.5, 27.4, 26.4, 28.3, 27.3, 29.2, 28.3, 30.2, 29.1, 53.0, 52.2, 56.0} {
		TrackETA(wobbly, consts.DLStatusDownloading, pct, now)
	}
	if got := ETA(wobbly, 56); got < 35 || got > 38 {
		t.Errorf("ETA through estimate dips = %d, want about 36", got)
	}
	TrackETA(wobbly, consts.DLStatusDownloading, 2, now)
	if got := ETA(wobbly, 2); got != -1 {
		t.Errorf("ETA once the next file starts = %d, want -1", got)
	}

	// Time at 0% before bytes arrive (yt-dlp starting up) isn't counted.
	const slowStart = 9002
	TrackETA(slowStart, consts.DLStatusDownloading, 0, now.Add(-60*time.Second))
	TrackETA(slowStart, consts.DLStatusDownloading, 10, now.Add(-20*time.Second))
	if got := ETA(slowStart, 30); got < 69 || got > 71 {
		t.Errorf("ETA after a slow start = %d, want about 70", got)
	}
}

// ******************************** Execute *******************************************************************************************

// TestNewDownloadsRefuseNil tests the download constructors return an error, rather than a download that would panic, when
// given a nil video, channel URL or channel.
func TestNewDownloadsRefuseNil(t *testing.T) {
	v, cu, c := &models.Video{URL: "https://example.com/v"}, &models.ChannelURL{}, &models.Channel{}
	for _, tc := range []struct {
		missing string
		v       *models.Video
		cu      *models.ChannelURL
		c       *models.Channel
	}{
		{"video", nil, cu, c},
		{"channel URL", v, nil, c},
		{"channel", v, cu, nil},
	} {
		if _, err := NewVideoDownload(t.Context(), tc.v, tc.cu, tc.c, nilDBStore{}, nil, Options{}); err == nil {
			t.Errorf("NewVideoDownload without a %s succeeded", tc.missing)
		}
		if _, err := NewJSONDownload(t.Context(), tc.v, tc.cu, tc.c, nilDBStore{}, nil, Options{}); err == nil {
			t.Errorf("NewJSONDownload without a %s succeeded", tc.missing)
		}
	}
}

// nilDBStore is a download store without a database, so Execute's domain block checks are skipped.
type nilDBStore struct{}

func (nilDBStore) GetDB() *sql.DB                                                  { return nil }
func (nilDBStore) UpdateDownloadStatus(context.Context, models.StatusUpdate) error { return nil }

// statusTracker keeps a download's status on its video, as the download queue does.
type statusTracker struct{}

func (statusTracker) SetStatus(v *models.Video, status consts.DownloadStatus, pct float64, stage string, err error) {
	v.DownloadStatus = models.DLStatus{Status: status, Percent: pct, Stage: stage, Error: err}
}
func (statusTracker) SendUpdate(*models.Video) {}

// blockCheckStore is a download store with a database handle, so Execute's domain block checks run.
type blockCheckStore struct {
	nilDBStore
	db *sql.DB
}

func (s blockCheckStore) GetDB() *sql.DB { return s.db }

// lockedBuffer is a goroutine-safe bytes.Buffer.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeYTDLP puts a yt-dlp script running body first on PATH, returning the file it adds a line to on each run.
func fakeYTDLP(t *testing.T, body string) (runs string) {
	t.Helper()
	dir := t.TempDir()
	runs = filepath.Join(dir, "runs")
	script := "#!/bin/sh\necho run >> '" + runs + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, sharedconsts.ProgramYTDLP), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return runs
}

// countRuns returns how many times the fake yt-dlp ran.
func countRuns(t *testing.T, runs string) int {
	t.Helper()
	data, err := os.ReadFile(runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run\n")
}

// newTestDownloads returns a video and a JSON download of url, with no wait between attempts.
func newTestDownloads(ctx context.Context, t *testing.T, url string, maxAttempts int) (*VideoDownload, *JSONDownload) {
	t.Helper()
	opts := Options{MaxAttempts: maxAttempts}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{}}
	c := &models.Channel{ChanSettings: &models.Settings{}, ChanMetarrArgs: &models.MetarrArgs{}}
	tracker := statusTracker{}
	vd, err := NewVideoDownload(ctx, &models.Video{URL: url, Files: models.VideoFiles{VideoDir: t.TempDir()}}, cu, c, nilDBStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}
	jd, err := NewJSONDownload(ctx, &models.Video{URL: url, Files: models.VideoFiles{JSONDir: t.TempDir()}}, cu, c, nilDBStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}
	return vd, jd
}

// TestExecuteRetries tests that failing downloads are tried MaxAttempts times, then report every attempt failed.
func TestExecuteRetries(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	runs := fakeYTDLP(t, `echo "ERROR: video unavailable" >&2; exit 1`)
	vd, jd := newTestDownloads(t.Context(), t, "https://retries.example/v", 3)

	if err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "all 3 download attempts failed") {
		t.Errorf("video download: got error %v, want all 3 attempts failed", err)
	}
	if n := countRuns(t, runs); n != 3 {
		t.Errorf("video download ran yt-dlp %d times, want 3", n)
	}

	if err := os.Remove(runs); err != nil {
		t.Fatal(err)
	}
	if err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "all 3 JSON download attempts failed") {
		t.Errorf("JSON download: got error %v, want all 3 attempts failed", err)
	}
	if n := countRuns(t, runs); n != 3 {
		t.Errorf("JSON download ran yt-dlp %d times, want 3", n)
	}
}

// TestExecuteVideoSucceedsFirstTry tests that a successful video download isn't retried.
func TestExecuteVideoSucceedsFirstTry(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	videoPath := filepath.Join(t.TempDir(), "video.webm")
	if err := os.WriteFile(videoPath, []byte("not really a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs := fakeYTDLP(t, "echo '"+command.FinalPathPrefix+videoPath+"'")
	vd, _ := newTestDownloads(t.Context(), t, "https://success.example/v", 3)

	if err := vd.Execute(); err != nil {
		t.Fatalf("got error %v, want a clean success", err)
	}
	if n := countRuns(t, runs); n != 1 {
		t.Errorf("ran yt-dlp %d times, want 1", n)
	}
	if vd.Video.Files.VideoFilePath != videoPath {
		t.Errorf("VideoFilePath = %q, want %q", vd.Video.Files.VideoFilePath, videoPath)
	}
}

// TestExecuteCanceledBeforeStart tests that a canceled download never runs yt-dlp.
func TestExecuteCanceledBeforeStart(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	runs := fakeYTDLP(t, "exit 1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	vd, jd := newTestDownloads(ctx, t, "https://canceled.example/v", 3)

	if err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("video download: got error %v, want a cancellation", err)
	}
	if err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("JSON download: got error %v, want a cancellation", err)
	}
	if n := countRuns(t, runs); n != 0 {
		t.Errorf("ran yt-dlp %d times after cancellation, want 0", n)
	}
}

// TestExecuteBlockedDomainSkipsYTDLP tests that downloads on an already blocked domain fail without running yt-dlp.
func TestExecuteBlockedDomainSkipsYTDLP(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	vars.BlockedDomainsMutex.Lock()
	origBlocked := vars.BlockedDomains
	vars.BlockedDomains = map[string]map[vars.BlockContext]vars.BlockedDomain{
		"blocked.example": {vars.BlockContextUnauth: {BlockedAt: time.Now()}},
	}
	vars.BlockedDomainsMutex.Unlock()
	t.Cleanup(func() {
		vars.BlockedDomainsMutex.Lock()
		vars.BlockedDomains = origBlocked
		vars.BlockedDomainsMutex.Unlock()
	})

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE blocked_domains (
			domain TEXT NOT NULL,
			context TEXT NOT NULL CHECK (context IN ('unauth', 'cookie', 'auth')),
			blocked_at TIMESTAMP NOT NULL,
			PRIMARY KEY (domain, context)
		);
		CREATE TABLE blocked_domain_videos (
			domain TEXT NOT NULL,
			context TEXT NOT NULL CHECK (context IN ('unauth', 'cookie', 'auth')),
			video_id INTEGER NOT NULL,
			channel_id INTEGER NOT NULL,
			PRIMARY KEY (domain, context, video_id),
			FOREIGN KEY (domain, context)
				REFERENCES blocked_domains (domain, context)
				ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatal(err)
	}

	runs := fakeYTDLP(t, "exit 1")
	opts := Options{MaxAttempts: 3}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{}}
	c := &models.Channel{ChanSettings: &models.Settings{}, ChanMetarrArgs: &models.MetarrArgs{}}
	tracker := statusTracker{}
	store := blockCheckStore{db: db}
	url := "https://www.blocked.example/v"

	vd, err := NewVideoDownload(t.Context(), &models.Video{URL: url, Files: models.VideoFiles{VideoDir: t.TempDir()}}, cu, c, store, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}
	jd, err := NewJSONDownload(t.Context(), &models.Video{URL: url, Files: models.VideoFiles{JSONDir: t.TempDir()}}, cu, c, store, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}

	if err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "is blocked") {
		t.Errorf("JSON download: got error %v, want a domain block", err)
	}
	if err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "is blocked") {
		t.Errorf("video download: got error %v, want a domain block", err)
	}
	if n := countRuns(t, runs); n != 0 {
		t.Errorf("ran yt-dlp %d times on a blocked domain, want 0", n)
	}
}

// TestCancelRemovesPartials tests that a user cancel removes only the partials of reported destinations in the video directory,
// and that a shutdown keeps them for resuming.
func TestCancelRemovesPartials(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	for _, tc := range []struct {
		name        string
		cause       error
		wantRemoved bool
	}{
		{"user cancel", ErrCanceledByUser, true},
		{"shutdown", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			dest := filepath.Join(dir, "video.f137.mp4")
			outsideDest := filepath.Join(outside, "elsewhere.mp4")
			kept := []string{filepath.Join(dir, "other.mp4.part"), outsideDest + ".part"}
			for _, p := range kept {
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			d := &VideoDownload{
				Video:      &models.Video{ID: 800001, URL: "https://a.example/v", Files: models.VideoFiles{VideoDir: dir}},
				Channel:    &models.Channel{},
				ChannelURL: &models.ChannelURL{ChanURLSettings: &models.Settings{}},
				DLTracker:  statusTracker{},
				Context:    ctx,
			}

			script := fmt.Sprintf(`echo "[download] Destination: %s"; echo "[download] Destination: %s"; : > %q; : > %q; exec sleep 30`,
				dest, outsideDest, dest+".part", dest+".ytdl")
			cmd := exec.CommandContext(ctx, "sh", "-c", script)
			useProcessGroup(cmd)
			done := make(chan error, 1)
			go func() { done <- d.executeVideoDownload(cmd) }()

			for start := time.Now(); ; time.Sleep(10 * time.Millisecond) {
				if _, err := os.Stat(dest + ".ytdl"); err == nil {
					break
				}
				if time.Since(start) > 10*time.Second {
					t.Fatal("fake yt-dlp never wrote its partials")
				}
			}
			if tc.cause != nil {
				cancel(tc.cause)
			} else {
				cancel(context.Canceled)
			}
			if err := <-done; err == nil {
				t.Fatal("expected an error from a canceled download")
			}
			_ = d.cancelVideoDownload()

			for _, p := range []string{dest + ".part", dest + ".ytdl"} {
				_, err := os.Stat(p)
				if removed := os.IsNotExist(err); removed != tc.wantRemoved {
					t.Errorf("%s: removed = %v, want %v", filepath.Base(p), removed, tc.wantRemoved)
				}
			}
			for _, p := range kept {
				if _, err := os.Stat(p); err != nil {
					t.Errorf("%q was removed although it wasn't this download's partial in the video directory", p)
				}
			}
		})
	}
}

// TestExecuteVideoDownloadTrailingOutput checks output after the final filename is fully read before cmd.Wait closes the pipes.
func TestExecuteVideoDownloadTrailingOutput(t *testing.T) {
	logs := &lockedBuffer{}
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: logs}
	t.Cleanup(func() { logger.Pl = origLogger })

	videoPath := filepath.Join(t.TempDir(), "video.webm")
	if err := os.WriteFile(videoPath, []byte("not really a video"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A burst fitting the OS pipe buffer lets the process exit with lines still unread (the original race).
	// A burst larger than the pipe buffer hangs if the parser stops draining after completion.
	for _, trailing := range []int{3000, 200000} {
		for i := range 10 {
			t.Run(fmt.Sprintf("%d_lines_run_%d", trailing, i), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()

				script := fmt.Sprintf(`echo %q; seq %d; seq %d >&2`, command.FinalPathPrefix+videoPath, trailing, trailing)
				d := &VideoDownload{
					Video:      &models.Video{ID: int64(900000 + trailing + i), URL: "https://a.example/v"},
					Channel:    &models.Channel{},
					ChannelURL: &models.ChannelURL{ChanURLSettings: &models.Settings{}},
					DLTracker:  statusTracker{},
					Context:    ctx,
				}

				done := make(chan error, 1)
				go func() { done <- d.executeVideoDownload(exec.CommandContext(ctx, "sh", "-c", script)) }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("executeVideoDownload() error = %v", err)
					}
				case <-ctx.Done():
					t.Fatal("executeVideoDownload() hung")
				}

				if d.Video.Files.VideoFilePath != videoPath {
					t.Errorf("VideoFilePath = %q, want %q", d.Video.Files.VideoFilePath, videoPath)
				}
				if strings.Contains(logs.String(), "Scanner error") {
					t.Fatalf("pipe read after close:\n%s", logs.String())
				}
			})
		}
	}
}

// ******************************** Final path ****************************************************************************************

// TestExecuteAudioSource tests a source saved with an audio extension finishes, rather than failing once it's downloaded.
func TestExecuteAudioSource(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	audioPath := filepath.Join(t.TempDir(), "clip.m4a")
	if err := os.WriteFile(audioPath, []byte("not really audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs := fakeYTDLP(t, "echo '"+command.FinalPathPrefix+audioPath+"'")
	vd, _ := newTestDownloads(t.Context(), t, "https://audio.example/v", 3)

	if err := vd.Execute(); err != nil {
		t.Fatalf("got error %v, want a clean success", err)
	}
	if n := countRuns(t, runs); n != 1 {
		t.Errorf("ran yt-dlp %d times, want 1", n)
	}
	if vd.Video.Files.VideoFilePath != audioPath {
		t.Errorf("VideoFilePath = %q, want %q", vd.Video.Files.VideoFilePath, audioPath)
	}
}

// TestFinalPathLine tests only the line yt-dlp prints for the finished file is read as its path.
func TestFinalPathLine(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	for _, tc := range []struct{ name, line, want string }{
		{"video", command.FinalPathPrefix + "/videos/a [x].mp4", "/videos/a [x].mp4"},
		{"audio", command.FinalPathPrefix + "/videos/a [x].m4a", "/videos/a [x].m4a"},
		{"windows", command.FinalPathPrefix + `C:\videos\a [x].mp4`, `C:\videos\a [x].mp4`},
		{"other path output", "/tmp/other-download-output.mp4", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vd, _ := newTestDownloads(t.Context(), t, "https://example.com/v", 1)
			lines := make(chan string, 1)
			lines <- tc.line
			close(lines)
			if got := vd.scanVideoCmdOutput(lines).filename; got != tc.want {
				t.Errorf("filename = %q, want %q", got, tc.want)
			}
		})
	}
}

// ******************************** Live progress *************************************************************************************

const (
	liveTestVideoURL      = "https://test-videos.co.uk/vids/bigbuckbunny/webm/vp9/1080/Big_Buck_Bunny_1080_10s_5MB.webm"
	liveTestVideoDuration = 10
)

// TestLiveDownloadProgress downloads a real file through each available downloader and checks progress is parsed.
func TestLiveDownloadProgress(t *testing.T) {
	if os.Getenv("TUBARR_LIVE_TESTS") == "" {
		t.Skip("set TUBARR_LIVE_TESTS=1 to download from the internet through each downloader")
	}
	if _, err := exec.LookPath(sharedconsts.ProgramYTDLP); err != nil {
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
		Video:   &models.Video{URL: liveTestVideoURL, Files: models.VideoFiles{VideoDir: t.TempDir()}, Metadata: models.VideoMetadata{Duration: liveTestVideoDuration}},
		Channel: &models.Channel{},
		ChannelURL: &models.ChannelURL{ChanURLSettings: &models.Settings{
			ExternalDownloader:     downloader,
			ExternalDownloaderArgs: dlArgs,
			ExtraYTDLPVideoArgs:    ytdlpArgs,
		}},
		Context: ctx,
	}
	cmd, err := d.buildVideoCommand()
	if err != nil {
		t.Fatal(err)
	}
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
