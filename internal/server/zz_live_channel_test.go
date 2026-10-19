package server

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"unicode"
	"unicode/utf8"

	"github.com/TubarrApp/gocommon/abstractions"
)

// liveTimeout is how long one user action, with its downloads and Metarr runs, may take.
const liveTimeout = 3 * time.Minute

// liveYTDLPArgs cancel the sleeps of yt-dlp's "-t sleep" preset, which Tubarr passes to avoid bot detection.
const liveYTDLPArgs = "--sleep-requests 0 --sleep-interval 0 --max-sleep-interval 0"

// liveMaxTail caps the fuzzed text added to titles, past the 255 byte file name limit.
const liveMaxTail = 400

// liveMaxActions caps the user actions one fuzz input runs.
const liveMaxActions = 10

// live holds what live tests share: the test clip, ffmpeg's encoder list, and the running test's Tubarr log.
var live struct {
	once     sync.Once
	clip     []byte
	encoders string
	err      error
	mu       sync.Mutex
	log      bytes.Buffer
}

// liveLogWriter collects Tubarr's console log, shown when a live test fails.
type liveLogWriter struct{}

func (liveLogWriter) Write(p []byte) (int, error) {
	live.mu.Lock()
	defer live.mu.Unlock()
	return live.log.Write(p)
}

// liveLogLines returns the Tubarr log lines containing any of subs.
func liveLogLines(subs ...string) []string {
	live.mu.Lock()
	defer live.mu.Unlock()
	var lines []string
	for line := range strings.SplitSeq(live.log.String(), "\n") {
		if slices.ContainsFunc(subs, func(s string) bool { return strings.Contains(line, s) }) {
			lines = append(lines, line)
		}
	}
	return lines
}

// setLiveEnv sets an environment variable until the test ends.
func setLiveEnv(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// setupLive skips unless yt-dlp and ffmpeg are installed, makes the test clip, keeps the tools Tubarr runs away from
// the user's own config and logs, and turns off Tubarr's and yt-dlp's bot-avoidance waits.
func setupLive(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("live channel tests run yt-dlp, skipped in short mode")
	}
	for _, tool := range []string{command.YTDLP, command.DownloaderFFmpeg} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s isn't installed: %v", tool, err)
		}
	}
	setupFuzzServer(t)
	live.once.Do(func() {
		path := filepath.Join(testDir, "live-clip.mp4")
		out, err := exec.Command(command.DownloaderFFmpeg, "-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=5",
			"-f", "lavfi", "-i", "sine=d=1",
			"-shortest", "-pix_fmt", "yuv420p", path).CombinedOutput()
		if err != nil {
			live.err = fmt.Errorf("making the test clip: %w: %s", err, out)
			return
		}
		if live.clip, live.err = os.ReadFile(path); live.err != nil {
			return
		}
		out, live.err = exec.Command(command.DownloaderFFmpeg, "-hide_banner", "-encoders").Output()
		live.encoders = string(out)
	})
	if live.err != nil {
		t.Fatal(live.err)
	}

	// yt-dlp and the downloaders read config from $HOME, and Metarr logs there. Pip --user installs stay findable.
	home := filepath.Join(testDir, "live-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := os.LookupEnv("PYTHONUSERBASE"); !ok {
		if userHome, err := os.UserHomeDir(); err == nil {
			setLiveEnv(t, "PYTHONUSERBASE", filepath.Join(userHome, ".local"))
		}
	}
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		setLiveEnv(t, k, home)
	}

	live.mu.Lock()
	live.log.Reset()
	live.mu.Unlock()
	logger.Pl.Console = liveLogWriter{}
	abstractions.Set(keys.SkipAllWaits, true)
	abstractions.Set(keys.ExtraYTDLPMetaArgs, liveYTDLPArgs)
	abstractions.Set(keys.ExtraYTDLPVideoArgs, liveYTDLPArgs)
	t.Cleanup(func() {
		if t.Failed() {
			live.mu.Lock()
			t.Logf("Tubarr log:\n%s", live.log.String())
			live.mu.Unlock()
		}
		logger.Pl.Console = io.Discard
		for _, k := range []string{keys.SkipAllWaits, keys.ExtraYTDLPMetaArgs, keys.ExtraYTDLPVideoArgs} {
			abstractions.Set(k, nil)
		}
	})
}

// liveTail makes fuzzed text safe to add to titles without changing which filters match them: no "c" or "d" (so no
// "cats" or "dogs"), no "&" (an HTML entity could spell one), and only characters XML allows.
func liveTail(s string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		switch {
		case unicode.ToLower(r) == 'c' || unicode.ToLower(r) == 'd':
			r = '_'
		case r == '&':
			r = '+'
		case r < 0x20 && r != '\n' && r != '\t', r == 0x7f, r == 0xfffe, r == 0xffff:
			continue
		}
		if b.Len()+utf8.RuneLen(r) > liveMaxTail {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// liveVideo is a video on the test site.
type liveVideo struct {
	id     string
	title  string // Starts "Cats", "Dogs" or "Cats and dogs", which sets the title filters it matches.
	day    int    // Uploaded on this day of October 2026.
	feeds  []string
	hidden bool // Taken off its feeds.
	gone   bool // Deleted, so its page and file answer 404.
	slow   bool // Its file download hangs until it's canceled.
}

func (v *liveVideo) date() time.Time {
	return time.Date(2026, time.October, v.day, 12, 0, 0, 0, time.UTC)
}

// liveSite is a test video site: RSS feeds of watch pages, whose JSON-LD gives each video's details and file.
type liveSite struct {
	srv      *httptest.Server
	tail     string // Added to every title.
	mu       sync.Mutex
	videos   []*liveVideo
	day      int
	notified map[string]int // POSTs to /notify/<name>.
	fetching chan string    // Slow videos whose file download started.
	closed   chan struct{}
}

// newLiveSite starts a test site, closed when the test ends.
func newLiveSite(t *testing.T, tail string) *liveSite {
	t.Helper()
	s := &liveSite{tail: tail, notified: map[string]int{}, fetching: make(chan string, 8), closed: make(chan struct{})}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(func() {
		close(s.closed)
		s.srv.CloseClientConnections()
		s.srv.Close()
	})
	return s
}

func (s *liveSite) feedURL(feed string) string { return s.srv.URL + "/" + feed + "/feed.xml" }

func (s *liveSite) pageURL(id string) string { return s.srv.URL + "/watch/" + id }

// upload adds a video ("cats", "dogs" or "mixed") to the given feeds, or none for an unlisted video.
func (s *liveSite) upload(category string, feeds ...string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.day++
	if s.day == 2 || s.day == 6 {
		s.day++ // The date filters' bounds, left out so either reading of them matches.
	}
	n := len(s.videos) + 1
	start := map[string]string{"cats": "Cats video", "dogs": "Dogs video", "mixed": "Cats and dogs"}[category]
	v := &liveVideo{id: fmt.Sprintf("%s-%d", category, n), title: fmt.Sprintf("%s %d", start, n), day: s.day, feeds: feeds}
	if s.tail != "" {
		v.title += " " + s.tail
	}
	s.videos = append(s.videos, v)
	return s.pageURL(v.id)
}

// find returns the video with the given ID, or nil. The caller holds s.mu.
func (s *liveSite) find(id string) *liveVideo {
	i := slices.IndexFunc(s.videos, func(v *liveVideo) bool { return v.id == id })
	if i < 0 {
		return nil
	}
	return s.videos[i]
}

// video returns a copy of the video at a page URL.
func (s *liveSite) video(pageURL string) (liveVideo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := strings.CutPrefix(pageURL, s.srv.URL+"/watch/"); ok {
		if v := s.find(id); v != nil {
			return *v, true
		}
	}
	return liveVideo{}, false
}

// setSlow makes a video's file download hang until canceled, or stop hanging.
func (s *liveSite) setSlow(pageURL string, slow bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v := s.find(strings.TrimPrefix(pageURL, s.srv.URL+"/watch/")); v != nil {
		v.slow = slow
	}
}

func (s *liveSite) serve(w http.ResponseWriter, r *http.Request) {
	switch p := r.URL.Path; {
	case strings.HasSuffix(p, "/feed.xml"):
		s.serveFeed(w, strings.Trim(strings.TrimSuffix(p, "feed.xml"), "/"))
	case strings.HasPrefix(p, "/watch/"):
		s.servePage(w, r, strings.TrimPrefix(p, "/watch/"))
	case strings.HasPrefix(p, "/media/"):
		s.serveFile(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/media/"), ".mp4"))
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/notify/"):
		s.mu.Lock()
		s.notified[strings.TrimPrefix(p, "/notify/")]++
		s.mu.Unlock()
	case r.Method == http.MethodPost && p == "/metarr-logs":
		_, _ = io.Copy(io.Discard, r.Body)
	default:
		http.NotFound(w, r)
	}
}

// serveFeed lists a feed's videos newest first.
func (s *liveSite) serveFeed(w http.ResponseWriter, feed string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b bytes.Buffer
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>%s</title><link>%s</link><description>Test feed</description>`, feed, s.srv.URL)
	for _, v := range slices.Backward(s.videos) {
		if v.hidden || !slices.Contains(v.feeds, feed) {
			continue
		}
		b.WriteString("<item><title>")
		_ = xml.EscapeText(&b, []byte(v.title))
		// No guid, which yt-dlp would add to each video URL as a fragment.
		fmt.Fprintf(&b, "</title><link>%s</link><pubDate>%s</pubDate></item>", s.pageURL(v.id), v.date().Format(time.RFC1123Z))
	}
	b.WriteString("</channel></rss>")
	w.Header().Set("Content-Type", "application/rss+xml")
	_, _ = w.Write(b.Bytes())
}

// servePage answers a watch page, giving the video's details and file in JSON-LD.
func (s *liveSite) servePage(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	var v liveVideo
	found := s.find(id)
	if found != nil {
		v = *found
	}
	s.mu.Unlock()
	if found == nil || v.gone {
		http.NotFound(w, r)
		return
	}
	ld, err := json.Marshal(map[string]string{
		"@context":    "https://schema.org",
		"@type":       "VideoObject",
		"name":        v.title,
		"description": "Test video " + v.id,
		"uploadDate":  v.date().Format(time.RFC3339),
		"contentUrl":  s.srv.URL + "/media/" + v.id + ".mp4",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title><script type="application/ld+json">%s</script></head><body></body></html>`,
		html.EscapeString(v.title), ld)
}

// serveFile answers a video file, hanging for slow videos until the download is canceled.
func (s *liveSite) serveFile(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	var v liveVideo
	found := s.find(id)
	if found != nil {
		v = *found
	}
	s.mu.Unlock()
	if found == nil || v.gone {
		http.NotFound(w, r)
		return
	}
	if v.slow && r.Method == http.MethodGet {
		select {
		case s.fetching <- id:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-s.closed:
		}
		return
	}
	http.ServeContent(w, r, id+".mp4", v.date(), bytes.NewReader(live.clip))
}

// liveFilter is a title filter.
type liveFilter struct {
	feed  string // Only for this feed's videos, if set.
	value string
	omits bool
	any   bool
}

// liveFilterSets are the channel filters picked from. None mixes must and any filters: checkFilters ignores any
// filters once a must filter passes, which may not be intended.
var liveFilterSets = [][]liveFilter{
	nil,
	{{value: "cats"}},
	{{value: "dogs", omits: true, any: true}},
	{{value: "dogs", omits: true, any: true}, {value: "cats", any: true}},
	{{feed: "b", value: "dogs"}},
	{{value: "dogs", omits: true}, {feed: "a", value: "cats"}},
}

// filtersText writes filters as the channel pages send them.
func (s *liveSite) filtersText(filters []liveFilter) string {
	lines := make([]string, 0, len(filters))
	for _, f := range filters {
		op, cond := "contains", "must"
		if f.omits {
			op = "omits"
		}
		if f.any {
			cond = "any"
		}
		line := fmt.Sprintf("title:%s:%s:%s", op, f.value, cond)
		if f.feed != "" {
			line = s.feedURL(f.feed) + "|" + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// liveURL is one of the channel's feeds, with its own settings.
type liveURL struct {
	id        int64
	paused    bool
	auto      *bool
	filters   []liveFilter // In place of the channel's, if set.
	videoDir  string       // In place of the channel's, if set.
	outputDir string       // In place of the channel's, if set.
}

// liveMove is a move op, sending videos whose title has value to dir.
type liveMove struct {
	feed, value, dir string
}

// liveOverrides are a video's Download As choices.
type liveOverrides struct {
	filename, videoDir, outputDir, metaOps string
}

// liveRow is a stored video.
type liveRow struct {
	id, channelURLID    int64
	finished, ignored   bool
	channelURL          string
	status              consts.DownloadStatus
	videoPath, jsonPath string
	metarrStatus        consts.MetarrStatus
}

// liveRows returns a channel's stored videos by URL.
func liveRows(t *testing.T, channelID int64) map[string]liveRow {
	t.Helper()
	rows, err := fuzzServer.db.Query(`SELECT v.id, v.url, COALESCE(v.channel_url_id, 0), COALESCE(v.channel_url, ''), v.finished,
		v.ignored, COALESCE(d.status, ''), COALESCE(v.video_path, ''), COALESCE(v.json_path, ''), COALESCE(v.metarr_status, '')
		FROM videos v LEFT JOIN downloads d ON d.video_id = v.id WHERE v.channel_id = ?`, channelID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]liveRow{}
	for rows.Next() {
		var u, status, metarrStatus string
		var r liveRow
		if err := rows.Scan(&r.id, &u, &r.channelURLID, &r.channelURL, &r.finished, &r.ignored, &status, &r.videoPath, &r.jsonPath, &metarrStatus); err != nil {
			t.Fatal(err)
		}
		r.status, r.metarrStatus = consts.DownloadStatus(status), consts.MetarrStatus(metarrStatus)
		out[u] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// waitLive waits until the channel has no crawl, download or Metarr run going.
func waitLive(t *testing.T, channelID int64) {
	t.Helper()
	deadline := time.Now().Add(liveTimeout)
	for {
		var activity map[string][]int64
		fetchJSON(t, "/api/v1/channels/activity", &activity)
		var busy int
		if err := fuzzServer.db.QueryRow("SELECT COUNT(*) FROM videos v JOIN downloads d ON d.video_id = v.id WHERE v.channel_id = ? AND d.status IN (?, ?)",
			channelID, consts.DLStatusQueued, consts.DLStatusDownloading).Scan(&busy); err != nil {
			t.Fatal(err)
		}
		idle := sendRequest(t, http.MethodGet, "/api/v1/status/idle", "", "").Code == http.StatusOK
		if idle && busy == 0 && !slices.Contains(activity["crawling"], channelID) && !slices.Contains(activity["metarr"], channelID) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("channel %d still busy after %v (activity %v, %d videos queued or downloading)", channelID, liveTimeout, activity, busy)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitLogged waits until the Tubarr log has more than before lines containing any of subs, for jobs started in the
// background before they show as running.
func waitLogged(t *testing.T, before int, subs ...string) {
	t.Helper()
	deadline := time.Now().Add(liveTimeout)
	for len(liveLogLines(subs...)) <= before {
		if time.Now().After(deadline) {
			t.Fatalf("no log line containing any of %q after %v", subs, liveTimeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// liveDownloaders are the external downloaders to pick from, blank for yt-dlp's own.
func liveDownloaders() []string {
	out := []string{""}
	for _, d := range []string{command.DownloaderFFmpeg, command.DownloaderWget, command.DownloaderCurl, command.DownloaderAria, command.DownloaderAxel} {
		if _, err := exec.LookPath(d); err == nil {
			out = append(out, d)
		}
	}
	return out
}

// liveTranscodes returns blank and each transcode value whose ffmpeg encoder is installed, from encoder, value pairs.
func liveTranscodes(pairs ...string) []string {
	out := []string{""}
	for i := 0; i+1 < len(pairs); i += 2 {
		if strings.Contains(live.encoders, " "+pairs[i]+" ") {
			out = append(out, pairs[i+1])
		}
	}
	return out
}

// liveChannelCount numbers the live channels, so each has its own name.
var liveChannelCount atomic.Int64

// liveScenario is a channel added with fuzzed settings, and the model of what it should do.
type liveScenario struct {
	t         *testing.T
	site      *liveSite
	dir       string
	name      string
	id        int64
	metarr    bool
	unmounted bool // The drive holding the channel's folders is unmounted.
	step      string
	auto      bool
	filters   int // Index into liveFilterSets.
	fromDay   int
	toDay     int
	videoDir  string // Templates filled.
	outputDir string // Templates filled.
	purge     bool
	moves     []liveMove
	notify    map[string]bool           // Notification endpoints set up.
	urls      map[string]*liveURL       // The channel's feeds.
	orphans   map[string]bool           // Files of videos deleted without them.
	overrides map[string]*liveOverrides // By video URL.
	notified  map[string]int            // Notifications so far.
	maybe     map[string]int            // Notifications that may also have been sent since the last check.
}

func (sc *liveScenario) errorf(format string, args ...any) {
	sc.t.Helper()
	sc.t.Errorf("%s: %s", sc.step, fmt.Sprintf(format, args...))
}

func (sc *liveScenario) fatalf(format string, args ...any) {
	sc.t.Helper()
	sc.t.Fatalf("%s: %s", sc.step, fmt.Sprintf(format, args...))
}

// newLiveScenario sets up the site and adds the channel, with settings picked by choices and title text added to
// every video's title.
func newLiveScenario(t *testing.T, choices []byte, title string) *liveScenario {
	t.Helper()
	i := 0
	pickIndex := func(n int) int {
		c := byte(0)
		if len(choices) > 0 {
			c = choices[i%len(choices)]
		}
		i++
		return int(c) % n
	}
	pick := func(values ...string) string { return values[pickIndex(len(values))] }

	dir := t.TempDir()
	site := newLiveSite(t, liveTail(title))
	setLiveEnv(t, "TUBARR_LOG_URL", site.srv.URL+"/metarr-logs") // Not a running Tubarr's.
	_, metarrErr := exec.LookPath("metarr")
	sc := &liveScenario{t: t, site: site, dir: dir, name: fmt.Sprintf("Live %d", liveChannelCount.Add(1)), metarr: metarrErr == nil,
		step: "adding the channel", notify: map[string]bool{}, urls: map[string]*liveURL{}, orphans: map[string]bool{},
		overrides: map[string]*liveOverrides{}, notified: map[string]int{}, maybe: map[string]int{}}

	site.upload("cats", "a")
	site.upload("dogs", "a")
	site.upload("mixed", "b")
	site.upload("cats", "b")
	site.upload("dogs", "c")
	if pick("", "shared") != "" {
		site.upload("cats", "a", "b") // Listed by both of the channel's URLs.
	}

	sc.auto = pick("true", "false") == "true"
	videoDir := pick(filepath.Join(dir, "videos"), filepath.Join(dir, "{{channel_name}}", "videos"), filepath.Join(dir, "videos", "{{channel_id}}"))
	for _, d := range []string{"videos", "json", "cats", "dogs", "metarr", "videos-b", "metarr-b"} { // Folders must exist to be saved.
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	form := url.Values{
		"name":                           {sc.name},
		"urls":                           {site.feedURL("a") + " " + site.feedURL("b")},
		jsonkeys.SettingsVideoDirectory:  {videoDir},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {strconv.FormatBool(sc.auto)},
	}
	set := func(key, value string) {
		if value != "" {
			form.Set(key, value)
		}
	}
	set(jsonkeys.SettingsJSONDirectory, pick("", filepath.Join(dir, "json")))
	set(jsonkeys.SettingsMaxConcurrency, pick("1", "3"))
	sc.filters = pickIndex(len(liveFilterSets))
	set("filters", site.filtersText(liveFilterSets[sc.filters]))
	if pick("", "from") != "" {
		form.Set("from_date", "20261002")
		sc.fromDay = 2
	}
	if pick("", "to") != "" {
		form.Set("to_date", "20261006")
		sc.toDay = 6
	}
	set("ytdlp_output_ext", pick("", "mkv", "mp4"))
	set("ytdlp_preferred_video_codecs", pick("", "h264", "av1\nvp9"))
	set("ytdlp_preferred_audio_codecs", pick("", "opus", "aac\nopus"))
	set("external_downloader", pick(liveDownloaders()...))
	set("max_filesize", pick("", "50M"))
	switch pickIndex(3) {
	case 1:
		sc.moves = []liveMove{{value: "cats", dir: filepath.Join(dir, "cats")}}
		form.Set("move_ops", "title:cats:"+sc.moves[0].dir)
	case 2:
		sc.moves = []liveMove{{feed: "b", value: "dogs", dir: filepath.Join(dir, "dogs")}}
		form.Set("move_ops", site.feedURL("b")+"|title:dogs:"+sc.moves[0].dir)
	}
	outputDir := pick("", filepath.Join(dir, "metarr"), filepath.Join(dir, "metarr", "{{year}}"))
	set("metarr_output_directory", outputDir)
	sc.outputDir = strings.ReplaceAll(outputDir, "{{year}}", "2026")
	set("metarr_output_ext", pick("", "mkv", "mp4"))
	set("metarr_rename_style", pick("", "underscores", "spaces", "fixes-only", "skip"))
	set("metarr_filename_ops", pick("", "prefix:[Live] ", "append: [Live]"))
	set("metarr_meta_ops", pick("", "artist:set:Live Band"))
	set("metarr_filtered_meta_ops", pick("", "title:contains:cats|artist:set:Cat Band"))
	set("metarr_filtered_filename_ops", pick("", "title:contains:dogs|prefix:[Dog] "))
	purge := pick("", "json", "all")
	set("metarr_purge_metafile", purge)
	sc.purge = purge != ""
	set("metarr_concurrency", pick("", "1", "2"))
	set("metarr_transcode_video_transcode_codecs", pick(liveTranscodes("libx265", "h264:hevc", "libvpx-vp9", "h264:vp9")...))
	set("metarr_transcode_audio_codecs", pick(liveTranscodes("libmp3lame", "aac:mp3", "libopus", "aac:opus")...))
	set("metarr_transcode_quality", pick("", "28"))
	set("metarr_transcode_video_filter", pick("", "hflip"))
	set("metarr_extra_ffmpeg_args", pick("", "-threads 1"))
	var notifications []string
	n := pickIndex(4)
	if sc.notify["all"] = n&1 != 0; sc.notify["all"] {
		notifications = append(notifications, site.srv.URL+"/notify/all|All")
	}
	if sc.notify["b"] = n&2 != 0; sc.notify["b"] {
		notifications = append(notifications, site.feedURL("b")+"|"+site.srv.URL+"/notify/b|B only")
	}
	set("notifications", strings.Join(notifications, "\n"))

	// The second URL's own settings.
	b := &liveURL{paused: pick("false", "true") == "true"}
	bSettings, bMetarr := map[string]any{"paused": b.paused}, map[string]any{}
	if auto := pick("", "true", "false"); auto != "" {
		v := auto == "true"
		b.auto = &v
		bSettings["auto_download"] = v
	}
	if pick("", "filters") != "" {
		b.filters = []liveFilter{{value: "cats"}}
		bSettings["filters"] = site.filtersText(b.filters)
	}
	if b.videoDir = pick("", filepath.Join(dir, "videos-b")); b.videoDir != "" {
		bSettings["video_directory"] = b.videoDir
	}
	if b.outputDir = pick("", filepath.Join(dir, "metarr-b")); b.outputDir != "" {
		bMetarr["metarr_output_directory"] = b.outputDir
	}
	perURL, _ := json.Marshal(map[string]any{site.feedURL("b"): map[string]any{"settings": bSettings, "metarr": bMetarr}})
	form.Set("url_settings", string(perURL))
	sc.urls["a"], sc.urls["b"] = &liveURL{}, b
	t.Logf("form: %s", form.Encode())

	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code >= 300 {
		t.Fatalf("adding the channel = %d %s", w.Code, w.Body.String())
	}
	if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", sc.name).Scan(&sc.id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if w := sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", sc.id), "", ""); w.Code != http.StatusNoContent {
			t.Errorf("deleting the channel = %d %s", w.Code, w.Body.String())
		}
	})
	sc.videoDir = strings.NewReplacer("{{channel_name}}", sc.name, "{{channel_id}}", strconv.FormatInt(sc.id, 10)).Replace(videoDir)
	sc.loadURLIDs()
	return sc
}

// loadURLIDs reads the IDs of the channel's feeds.
func (sc *liveScenario) loadURLIDs() {
	rows, err := fuzzServer.db.Query("SELECT id, url FROM channel_urls WHERE channel_id = ?", sc.id)
	if err != nil {
		sc.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var u string
		if err := rows.Scan(&id, &u); err != nil {
			sc.t.Fatal(err)
		}
		for f, cu := range sc.urls {
			if sc.site.feedURL(f) == u {
				cu.id = id
			}
		}
	}
}

// feeds returns the channel's feeds, leaving out paused ones unless withPaused.
func (sc *liveScenario) feeds(withPaused bool) []string {
	var out []string
	for _, f := range slices.Sorted(maps.Keys(sc.urls)) {
		if withPaused || !sc.urls[f].paused {
			out = append(out, f)
		}
	}
	return out
}

func (sc *liveScenario) autoFor(feed string) bool {
	if u := sc.urls[feed]; u != nil && u.auto != nil {
		return *u.auto
	}
	return sc.auto
}

// passes reports whether a video crawled through feed passes the filters and dates that apply to it.
func (sc *liveScenario) passes(feed string, v *liveVideo) bool {
	if sc.fromDay != 0 && v.day < sc.fromDay || sc.toDay != 0 && v.day > sc.toDay {
		return false
	}
	filters := liveFilterSets[sc.filters]
	if u := sc.urls[feed]; u != nil && u.filters != nil {
		filters = u.filters
	}
	title := strings.ToLower(v.title)
	var mustTotal, mustPassed, anyTotal, anyPassed int
	for _, f := range filters {
		if f.feed != "" && f.feed != feed {
			continue
		}
		ok := strings.Contains(title, f.value) != f.omits
		switch {
		case f.any:
			anyTotal++
			if ok {
				anyPassed++
			}
		default:
			mustTotal++
			if ok {
				mustPassed++
			}
		}
	}
	return mustPassed == mustTotal && (anyTotal == 0 || anyPassed > 0)
}

// feedOf returns the feed a stored video was downloaded or listed through, or "manual".
func (sc *liveScenario) feedOf(r liveRow) string {
	for _, f := range []string{"a", "b", "c"} {
		if r.channelURL == sc.site.feedURL(f) {
			return f
		}
	}
	return "manual"
}

// wantDir returns the directory a video downloaded through feed should end up in.
func (sc *liveScenario) wantDir(feed string, v liveVideo, o *liveOverrides) string {
	cu := sc.urls[feed]
	if cu == nil || feed == "manual" {
		cu = &liveURL{}
	}
	videoDir := cmpOr(cu.videoDir, sc.videoDir)
	if o != nil && o.videoDir != "" {
		videoDir = o.videoDir
	}
	if !sc.metarr {
		return videoDir
	}
	if o != nil && o.outputDir != "" {
		return o.outputDir
	}
	for _, m := range sc.moves {
		if (m.feed == "" || m.feed == feed) && strings.Contains(strings.ToLower(v.title), m.value) {
			return m.dir
		}
	}
	return cmpOr(cu.outputDir, sc.outputDir, videoDir)
}

// cmpOr returns the first non-blank string.
func cmpOr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// liveMediaExts are the extensions of files the test downloads or Metarr writes.
var liveMediaExts = map[string]bool{".mp4": true, ".mkv": true, ".webm": true, ".mov": true, ".m4a": true, ".mp3": true, ".opus": true, ".json": true}

// check checks what holds after every action: each stored video is on the site and consistent, downloaded ones have
// their files where their settings put them, no other files are left in the channel's directories (nor recreated while
// their drive is unmounted), the Available list matches, and the notifications sent are those expected.
func (sc *liveScenario) check(rows map[string]liveRow) {
	sc.t.Helper()
	if _, err := os.Stat(sc.dir); sc.unmounted && !errors.Is(err, fs.ErrNotExist) {
		sc.errorf("%q exists while its drive is unmounted, so something recreated it", sc.dir)
	}
	files := map[string]string{} // File path, to the video storing it.
	for u, r := range rows {
		v, ok := sc.site.video(u)
		if !ok {
			sc.errorf("%s: stored, but it isn't a video on the site", u)
			continue
		}
		feed := sc.feedOf(r)
		if feed == "manual" && r.channelURL != consts.ManualDownloadsCol {
			sc.errorf("%s: stored under channel URL %q, which the channel never had", u, r.channelURL)
		}
		switch r.status {
		case consts.DLStatusCompleted:
			if !r.finished || r.ignored {
				sc.errorf("%s: finished downloading, but stored as %+v", u, r)
			}
		case consts.DLStatusIgnored:
			if !r.ignored || r.finished {
				sc.errorf("%s: ignored, but stored as %+v", u, r)
			}
		case consts.DLStatusAvailable, consts.DLStatusFailed, consts.DLStatusCanceled:
			if r.finished || r.ignored {
				sc.errorf("%s: in the Available list, but stored as %+v", u, r)
			}
		default:
			sc.errorf("%s: status %q with nothing running", u, r.status)
		}
		for _, path := range []string{r.videoPath, r.jsonPath} {
			if other, dup := files[path]; dup {
				sc.errorf("%s and %s are both stored with file %q", other, u, path)
			} else if path != "" {
				files[path] = u
			}
		}
		if r.status != consts.DLStatusCompleted {
			continue
		}
		switch {
		case !sc.metarr && r.metarrStatus != consts.MetarrStatusPending:
			sc.errorf("%s: Metarr isn't installed, but its Metarr status is %q", u, r.metarrStatus)
		case sc.metarr && r.metarrStatus != consts.MetarrStatusDone:
			sc.errorf("%s: Metarr status is %q, want %q", u, r.metarrStatus, consts.MetarrStatusDone)
		case sc.metarr && sc.purge && r.jsonPath != "":
			sc.errorf("%s: Metarr purges metadata files, but the JSON path %q is still stored", u, r.jsonPath)
		}
		if sc.unmounted {
			continue // Its files are on the unmounted drive.
		}
		sc.checkFile(u, "video", r.videoPath, true)
		sc.checkFile(u, "JSON", r.jsonPath, false)
		if want := sc.wantDir(feed, v, sc.overrides[u]); r.videoPath != "" && filepath.Dir(r.videoPath) != want {
			sc.errorf("%s: video file %q isn't in %q", u, r.videoPath, want)
		}
	}

	// No partial downloads, or files no stored video has, are left.
	if !sc.unmounted {
		err := filepath.WalkDir(sc.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := d.Name()
			switch {
			case strings.HasSuffix(name, ".part") || strings.Contains(name, ".part-Frag") || strings.HasSuffix(name, ".ytdl") || strings.HasSuffix(name, ".temp"):
				sc.errorf("partial download %q was left behind", path)
			case liveMediaExts[strings.ToLower(filepath.Ext(name))]:
				if _, stored := files[path]; !stored && !sc.orphans[path] {
					sc.errorf("file %q isn't any stored video's", path)
				}
			}
			return nil
		})
		if err != nil {
			sc.t.Fatal(err)
		}
	}

	// The Available list shows the channel's available, failed and canceled videos.
	var listed []models.AvailableVideo
	fetchJSON(sc.t, "/api/v1/videos/available", &listed)
	inList := map[string]bool{}
	for _, v := range listed {
		if v.ChannelName == sc.name {
			inList[v.URL] = true
		}
	}
	for u, r := range rows {
		if want := slices.Contains(app.AvailableStatuses, r.status); inList[u] != want {
			sc.errorf("%s: %q, but listed as available: %t", u, r.status, inList[u])
		}
	}
	for u := range inList {
		if _, ok := rows[u]; !ok {
			sc.errorf("%s: listed as available, but not stored", u)
		}
	}

	sc.site.mu.Lock()
	got := maps.Clone(sc.site.notified)
	sc.site.mu.Unlock()
	for _, name := range []string{"all", "b"} {
		if lo, hi := sc.notified[name], sc.notified[name]+sc.maybe[name]; got[name] < lo || got[name] > hi {
			sc.errorf("notification %q sent %d times, want %d to %d", name, got[name], lo, hi)
		}
		sc.notified[name] = got[name]
	}
	clear(sc.maybe)
}

// checkFile checks a stored file exists, in the channel's directories.
func (sc *liveScenario) checkFile(u, what, path string, required bool) {
	sc.t.Helper()
	if path == "" {
		if required {
			sc.errorf("%s: downloaded, but has no %s path", u, what)
		}
		return
	}
	if !strings.HasPrefix(path, sc.dir+string(filepath.Separator)) {
		sc.errorf("%s: %s file %q is outside the channel's directories", u, what, path)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		sc.errorf("%s: %s file %q is missing or empty: %v", u, what, path, err)
	}
}

// expect checks videos in want now have one of its statuses (or were deleted, for none, or may not be stored, for a
// blank status), and other videos are as they were.
func (sc *liveScenario) expect(before, after map[string]liveRow, want map[string][]consts.DownloadStatus) {
	sc.t.Helper()
	for u, statuses := range want {
		r, ok := after[u]
		switch {
		case statuses == nil && ok:
			sc.errorf("%s: still stored after being deleted: %+v", u, r)
		case statuses != nil && !ok && !slices.Contains(statuses, ""):
			sc.errorf("%s: not stored, want status %v", u, statuses)
		case statuses != nil && ok && !slices.Contains(statuses, r.status):
			sc.errorf("%s: stored as %+v, want status %v", u, r, statuses)
		}
	}
	for u, r := range before {
		if _, changed := want[u]; changed {
			continue
		}
		if a, ok := after[u]; !ok || a != r {
			sc.errorf("%s: changed from %+v to %+v", u, r, a)
		}
	}
	for u, r := range after {
		_, had := before[u]
		if _, wanted := want[u]; !had && !wanted {
			sc.errorf("%s: stored unexpectedly: %+v", u, r)
		}
	}
}

// expectNotified expects a notification, if it's set up: sure if it must be sent, maybe if it may be.
func (sc *liveScenario) expectNotified(name string, sure, maybe bool) {
	switch {
	case !sc.notify[name]:
	case sure:
		sc.notified[name]++
	case maybe:
		sc.maybe[name]++
	}
}

// available returns the URLs of the channel's videos in the Available list, leaving out those deleted from the site if
// notGone.
func (sc *liveScenario) available(rows map[string]liveRow, notGone bool) []string {
	var out []string
	for u, r := range rows {
		v, ok := sc.site.video(u)
		if slices.Contains(app.AvailableStatuses, r.status) && (!notGone || ok && !v.gone) {
			out = append(out, u)
		}
	}
	slices.Sort(out)
	return out
}

// crawl starts a crawl through feeds as a button does, then checks each video they list that the channel hasn't seen
// was stored as its feed's settings say.
func (sc *liveScenario) crawl(feeds []string, ignore bool, path string, body any) {
	before := liveRows(sc.t, sc.id)
	want := map[string][]consts.DownloadStatus{}
	downloaders := map[string][]string{} // Feeds that should download each video.
	crawled := feeds
	if sc.unmounted && !ignore {
		crawled = nil // Crawls skip URLs whose destinations are unavailable.
	}
	sc.site.mu.Lock()
	for _, f := range crawled {
		for _, v := range sc.site.videos {
			if v.hidden || !slices.Contains(v.feeds, f) {
				continue
			}
			u := sc.site.pageURL(v.id)
			auto := sc.autoFor(f)
			if prev, had := before[u]; had && (prev.finished || prev.ignored || prev.status == consts.DLStatusAvailable ||
				!auto && (prev.status == consts.DLStatusFailed || prev.status == consts.DLStatusCanceled)) {
				continue // Seen.
			}
			status := consts.DLStatusCompleted
			switch {
			case ignore:
				status = consts.DLStatusIgnored
			case !auto:
				status = consts.DLStatusAvailable
			case v.gone:
				status = consts.DLStatusFailed
			case !sc.passes(f, v):
				status = consts.DLStatusIgnored
			default:
				downloaders[u] = append(downloaders[u], f)
			}
			if !slices.Contains(want[u], status) {
				want[u] = append(want[u], status)
			}
		}
	}
	sc.site.mu.Unlock()

	var w *httptest.ResponseRecorder
	if body == nil {
		w = sendRequest(sc.t, http.MethodPost, path, "", "")
	} else {
		w = sendJSON(sc.t, http.MethodPost, path, body)
	}
	if w.Code != http.StatusAccepted {
		sc.fatalf("starting the crawl = %d %s", w.Code, w.Body.String())
	}
	waitLive(sc.t, sc.id)
	after := liveRows(sc.t, sc.id)
	sc.expect(before, after, want)

	var downloaded, bSure, bMaybe bool
	for u := range want {
		delete(sc.overrides, u) // Crawls download without them.
	}
	for u, feeds := range downloaders {
		if after[u].status != consts.DLStatusCompleted {
			continue
		}
		downloaded = true
		bSure = bSure || slices.Equal(feeds, []string{"b"})
		bMaybe = bMaybe || slices.Contains(feeds, "b")
	}
	sc.expectNotified("all", downloaded, false)
	sc.expectNotified("b", bSure, bMaybe)
}

// download downloads videos from the Available list, with Download As overrides if o is set, and checks they downloaded
// unless deleted from the site or their destination is unavailable (an unmounted drive, or a Download As folder that
// doesn't exist), which must not be created.
func (sc *liveScenario) download(us []string, o *liveOverrides) {
	before := liveRows(sc.t, sc.id)
	missing := ""
	if o != nil {
		for _, d := range []string{o.videoDir, o.outputDir} {
			if _, err := os.Stat(d); d != "" && missing == "" && err != nil {
				missing = d
			}
		}
	}
	want := map[string][]consts.DownloadStatus{}
	ids := make([]int64, 0, len(us))
	for _, u := range us {
		ids = append(ids, before[u].id)
		want[u] = []consts.DownloadStatus{consts.DLStatusCompleted}
		if v, ok := sc.site.video(u); !ok || v.gone || sc.unmounted || missing != "" {
			want[u] = []consts.DownloadStatus{consts.DLStatusFailed}
		}
	}
	body := map[string]any{"action": "download", "video_ids": ids}
	if o != nil {
		body["overrides"] = map[string]string{"filename": o.filename, "video_dir": o.videoDir, "output_dir": o.outputDir, "meta_ops": o.metaOps}
	}
	w := sendJSON(sc.t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", sc.id), body)
	if missing != "" && w.Code != http.StatusBadRequest {
		sc.errorf("Download As to the missing folder %q = %d %s, want it refused", missing, w.Code, w.Body.String())
	}
	if w.Code == http.StatusBadRequest && o != nil && (missing != "" || o.filename != "Chosen") {
		sc.expect(before, liveRows(sc.t, sc.id), nil) // A missing folder (or a fuzzed filename) is refused up front.
		return
	}
	if w.Code != http.StatusAccepted {
		sc.fatalf("downloading %v = %d %s", us, w.Code, w.Body.String())
	}
	for _, u := range us {
		sc.overrides[u] = o
	}
	waitLive(sc.t, sc.id)
	after := liveRows(sc.t, sc.id)
	sc.expect(before, after, want)
	if _, err := os.Stat(missing); missing != "" && !sc.unmounted && err == nil {
		sc.errorf("the missing Download As folder %q was created", missing)
	}

	var downloaded, b bool
	for _, u := range us {
		if r := after[u]; r.status == consts.DLStatusCompleted {
			downloaded = true
			b = b || r.channelURL == sc.site.feedURL("b")
		}
	}
	sc.expectNotified("all", downloaded, false)
	sc.expectNotified("b", b, false)
}

// edit saves the channel from its edit page, after change alters the form and per-URL settings.
func (sc *liveScenario) edit(change func(form url.Values, perURL map[string]map[string]map[string]any)) {
	var got channelDetails
	fetchJSON(sc.t, fmt.Sprintf("/api/v1/channels/%d", sc.id), &got)
	form := editPageForm(readChannelFormFields(sc.t, "edit-channel.html"), got)
	perURL := map[string]map[string]map[string]any{}
	if err := json.Unmarshal([]byte(form.Get("url_settings")), &perURL); err != nil {
		sc.t.Fatal(err)
	}
	if change != nil {
		change(form, perURL)
	}
	data, err := json.Marshal(perURL)
	if err != nil {
		sc.t.Fatal(err)
	}
	form.Set("url_settings", string(data))
	if w := sendForm(sc.t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", sc.id), form); w.Code >= 300 {
		sc.fatalf("saving the edit page = %d %s, form %q", w.Code, w.Body.String(), form.Encode())
	}
}

// details returns the channel as the edit page loads it.
func (sc *liveScenario) details() channelDetails {
	var got channelDetails
	fetchJSON(sc.t, fmt.Sprintf("/api/v1/channels/%d", sc.id), &got)
	return got
}

// The user actions, in the order the fuzz input numbers them.
const (
	actCrawl = iota
	actCrawlOne
	actIgnoreCrawl
	actUpload
	actToggleGone
	actEditUnchanged
	actEditAuto
	actEditPauseB
	actEditToggleC
	actEditFilters
	actDownloadAll
	actDownloadAs
	actDismiss
	actDelete
	actRerunMetarr
	actManualDownload
	actAddAvailable
	actCancel
	actUnmount
)

// liveActions are what a user can do, each taking a byte choosing its details.
var liveActions = [...]struct {
	name string
	run  func(sc *liveScenario, p byte)
}{
	actCrawl: {"crawl", func(sc *liveScenario, _ byte) {
		sc.crawl(sc.feeds(false), false, fmt.Sprintf("/api/v1/channels/%d/crawl", sc.id), map[string]any{})
	}},
	actCrawlOne: {"crawl one URL", func(sc *liveScenario, p byte) {
		feeds := sc.feeds(true) // A chosen paused URL is crawled too.
		f := feeds[int(p)%len(feeds)]
		sc.crawl([]string{f}, false, fmt.Sprintf("/api/v1/channels/%d/crawl", sc.id), map[string]any{"url_ids": []int64{sc.urls[f].id}})
	}},
	actIgnoreCrawl: {"ignore crawl", func(sc *liveScenario, _ byte) {
		sc.crawl(sc.feeds(true), true, fmt.Sprintf("/api/v1/channels/%d/ignore-crawl", sc.id), nil)
	}},
	actUpload: {"upload to the site", func(sc *liveScenario, p byte) {
		feeds := [][]string{{"a"}, {"b"}, {"c"}, {"a", "b"}}[int(p/3)%4]
		sc.site.upload([]string{"cats", "dogs", "mixed"}[p%3], feeds...)
	}},
	actToggleGone: {"delete or restore a video on the site", func(sc *liveScenario, p byte) {
		sc.site.mu.Lock()
		defer sc.site.mu.Unlock()
		v := sc.site.videos[int(p&0x7f)%len(sc.site.videos)]
		v.gone = !v.gone
		v.hidden = v.gone && p&0x80 != 0 // Deleted videos may still be listed for a while.
	}},
	actEditUnchanged: {"save the edit page unchanged", func(sc *liveScenario, _ byte) {
		before, details := liveRows(sc.t, sc.id), sc.details()
		sc.edit(nil)
		if again := sc.details(); !reflect.DeepEqual(details, again) {
			sc.errorf("saving the edit page unchanged changed the channel:\nbefore: %+v\nafter: %+v", details, again)
		}
		sc.expect(before, liveRows(sc.t, sc.id), nil)
	}},
	actEditAuto: {"switch auto download", func(sc *liveScenario, _ byte) {
		before := liveRows(sc.t, sc.id)
		sc.auto = !sc.auto
		sc.edit(func(form url.Values, _ map[string]map[string]map[string]any) {
			form.Set(jsonkeys.SettingsAutoDownload, strconv.FormatBool(sc.auto))
		})
		if got := sc.details().Settings[jsonkeys.SettingsAutoDownload]; got != sc.auto {
			sc.errorf("auto download reads back as %v, want %t", got, sc.auto)
		}
		sc.expect(before, liveRows(sc.t, sc.id), nil)
	}},
	actEditPauseB: {"pause or unpause the second URL", func(sc *liveScenario, _ byte) {
		b := sc.urls["b"]
		before := liveRows(sc.t, sc.id)
		b.paused = !b.paused
		sc.edit(func(_ url.Values, perURL map[string]map[string]map[string]any) {
			u := sc.site.feedURL("b")
			if perURL[u] == nil {
				perURL[u] = map[string]map[string]any{}
			}
			if perURL[u]["settings"] == nil {
				perURL[u]["settings"] = map[string]any{}
			}
			perURL[u]["settings"]["paused"] = b.paused
		})
		if got := sc.details().URLSettings[sc.site.feedURL("b")]["settings"]["paused"] == true; got != b.paused {
			sc.errorf("the second URL reads back paused: %t, want %t", got, b.paused)
		}
		sc.expect(before, liveRows(sc.t, sc.id), nil)
	}},
	actEditToggleC: {"add or remove a third URL", func(sc *liveScenario, _ byte) {
		before, c := liveRows(sc.t, sc.id), sc.site.feedURL("c")
		_, had := sc.urls["c"]
		sc.edit(func(form url.Values, perURL map[string]map[string]map[string]any) {
			urls := strings.Fields(form.Get("urls"))
			if had {
				urls = slices.DeleteFunc(urls, func(u string) bool { return u == c })
				delete(perURL, c)
			} else {
				urls = append(urls, c)
			}
			form.Set("urls", strings.Join(urls, " "))
		})
		if had {
			delete(sc.urls, "c")
		} else {
			sc.urls["c"] = &liveURL{}
			sc.loadURLIDs()
		}
		if got := slices.Contains(sc.details().URLs, c); got == had {
			sc.errorf("the third URL is listed: %t, want %t", got, !had)
		}
		after := liveRows(sc.t, sc.id)
		for u, r := range before {
			if r.channelURL == c && had && after[u].channelURLID == 0 {
				r.channelURLID = 0 // Unlinked from the removed URL.
				before[u] = r
			}
		}
		sc.expect(before, after, nil)
	}},
	actEditFilters: {"change filters", func(sc *liveScenario, p byte) {
		before := liveRows(sc.t, sc.id)
		sc.filters = (sc.filters + 1 + int(p)) % len(liveFilterSets)
		text := sc.site.filtersText(liveFilterSets[sc.filters])
		sc.edit(func(form url.Values, _ map[string]map[string]map[string]any) {
			form.Set("filters", text)
			if text == "" {
				form.Del("filters")
			}
		})
		if got := formValue(sc.details().Settings["filters"]); got != text {
			sc.errorf("filters read back as %q, want %q", got, text)
		}
		sc.expect(before, liveRows(sc.t, sc.id), nil)
	}},
	actDownloadAll: {"download all available", func(sc *liveScenario, _ byte) {
		if us := sc.available(liveRows(sc.t, sc.id), false); len(us) > 0 {
			sc.download(us, nil)
		}
	}},
	actDownloadAs: {"Download As", func(sc *liveScenario, p byte) {
		us := sc.available(liveRows(sc.t, sc.id), false)
		if len(us) == 0 {
			return
		}
		o := &liveOverrides{}
		if p&1 != 0 {
			o.filename = strings.TrimSpace("Chosen " + sc.site.tail)
		}
		if p&2 != 0 {
			o.videoDir = filepath.Join(sc.dir, "as-videos")
			if p&0x20 != 0 {
				o.videoDir = filepath.Join(sc.dir, "as-missing")
			} else if !sc.unmounted {
				if err := os.MkdirAll(o.videoDir, 0o755); err != nil { // Chosen as an existing folder.
					sc.t.Fatal(err)
				}
			}
		}
		if p&4 != 0 {
			o.outputDir = filepath.Join(sc.dir, "as-out")
			if !sc.unmounted {
				if err := os.MkdirAll(o.outputDir, 0o755); err != nil {
					sc.t.Fatal(err)
				}
			}
		}
		if p&8 != 0 {
			o.metaOps = "artist:set:Chosen Artist"
		}
		if *o == (liveOverrides{}) {
			o.filename = "Chosen"
		}
		sc.download(us[int(p>>4)%len(us):][:1], o)
	}},
	actDismiss: {"dismiss all available", func(sc *liveScenario, _ byte) {
		before := liveRows(sc.t, sc.id)
		us := sc.available(before, false)
		if len(us) == 0 {
			return
		}
		want := map[string][]consts.DownloadStatus{}
		ids := make([]int64, 0, len(us))
		for _, u := range us {
			ids = append(ids, before[u].id)
			want[u] = []consts.DownloadStatus{consts.DLStatusIgnored}
		}
		if w := sendJSON(sc.t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", sc.id), map[string]any{"action": "dismiss", "video_ids": ids}); w.Code != http.StatusAccepted {
			sc.fatalf("dismissing = %d %s", w.Code, w.Body.String())
		}
		sc.expect(before, liveRows(sc.t, sc.id), want)
	}},
	actDelete: {"delete videos from the channel", func(sc *liveScenario, p byte) {
		before := liveRows(sc.t, sc.id)
		var candidates []string
		for u, r := range before {
			if r.status == consts.DLStatusCompleted || r.status == consts.DLStatusIgnored {
				candidates = append(candidates, u)
			}
		}
		if len(candidates) == 0 {
			return
		}
		slices.Sort(candidates)
		start := int(p>>1&0x3f) % len(candidates)
		chosen := candidates[start:min(start+1+int(p&1), len(candidates))]
		withFiles := p&0x80 != 0
		form := url.Values{"urls[]": chosen, "delete_files": {strconv.FormatBool(withFiles)}}
		if w := sendRequest(sc.t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete-videos", sc.id), "application/x-www-form-urlencoded", form.Encode()); w.Code != http.StatusOK {
			sc.fatalf("deleting %v = %d %s", chosen, w.Code, w.Body.String())
		}
		want := map[string][]consts.DownloadStatus{}
		for _, u := range chosen {
			want[u] = nil
			delete(sc.overrides, u)
			for _, path := range []string{before[u].videoPath, before[u].jsonPath} {
				switch _, err := os.Stat(path); {
				case path == "":
				case !withFiles || sc.unmounted:
					sc.orphans[path] = true // Kept, or on the unmounted drive.
				case err == nil:
					sc.errorf("%s: deleted with its files, but %q is still there", u, path)
				}
			}
		}
		sc.expect(before, liveRows(sc.t, sc.id), want)
	}},
	actRerunMetarr: {"re-run Metarr", func(sc *liveScenario, _ byte) {
		before := liveRows(sc.t, sc.id)
		var ids []int64
		want := map[string][]consts.DownloadStatus{}
		for u, r := range before {
			if r.status == consts.DLStatusCompleted && r.jsonPath != "" && r.channelURLID != 0 {
				ids = append(ids, r.id)
				want[u] = []consts.DownloadStatus{consts.DLStatusCompleted}
			}
		}
		if !sc.metarr || len(ids) == 0 {
			return
		}
		if sc.unmounted {
			want = nil // The videos' files are on the unmounted drive, so nothing changes.
		}
		done, failed := fmt.Sprintf("Completed Metarr run for channel %q", sc.name), fmt.Sprintf("errors running Metarr for channel %q", sc.name)
		n, nFailed := len(liveLogLines(done, failed)), len(liveLogLines(failed))
		if w := sendJSON(sc.t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/run-metarr", sc.id), map[string]any{"video_ids": ids, "rerun": true}); w.Code != http.StatusAccepted {
			sc.fatalf("re-running Metarr = %d %s", w.Code, w.Body.String())
		}
		waitLogged(sc.t, n, done, failed)
		waitLive(sc.t, sc.id)
		if lines := liveLogLines(failed); len(lines) > nFailed && !sc.unmounted {
			sc.errorf("re-running Metarr failed: %s", lines[len(lines)-1])
		}
		sc.expect(before, liveRows(sc.t, sc.id), want)
	}},
	actManualDownload: {"download a URL", func(sc *liveScenario, p byte) {
		before := liveRows(sc.t, sc.id)
		u := sc.site.upload([]string{"cats", "dogs", "mixed"}[p%3]) // Unlisted.
		f := []string{"manual", "a", "b"}[int(p/3)%3]
		var cuID int64
		if cu := sc.urls[f]; cu != nil {
			cuID = cu.id
		}
		subs := []string{fmt.Sprintf("manual download for channel %q", sc.name), fmt.Sprintf("downloading URLs for channel %q", sc.name),
			fmt.Sprintf("for channel %q did not start", sc.name)}
		n := len(liveLogLines(subs...))
		body := map[string]any{"videos": []map[string]any{{"url": u, "channel_url_id": cuID}}}
		w := sendJSON(sc.t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/download-urls", sc.id), body)
		if sc.unmounted { // Refused up front while its destination is unavailable.
			if w.Code != http.StatusConflict {
				sc.errorf("downloading %s with the drive unmounted = %d %s, want %d", u, w.Code, w.Body.String(), http.StatusConflict)
			}
			sc.expect(before, liveRows(sc.t, sc.id), nil)
			return
		}
		if w.Code != http.StatusAccepted {
			sc.fatalf("downloading %s = %d %s", u, w.Code, w.Body.String())
		}
		waitLogged(sc.t, n, subs...)
		waitLive(sc.t, sc.id)
		after := liveRows(sc.t, sc.id)
		sc.expect(before, after, map[string][]consts.DownloadStatus{u: {consts.DLStatusCompleted}})
		sc.expectNotified("all", after[u].status == consts.DLStatusCompleted, false)
		sc.expectNotified("b", after[u].status == consts.DLStatusCompleted && after[u].channelURL == sc.site.feedURL("b"), false)
	}},
	actAddAvailable: {"add a URL to the Available list", func(sc *liveScenario, p byte) {
		before := liveRows(sc.t, sc.id)
		u := sc.site.upload([]string{"cats", "dogs", "mixed"}[p%3]) // Unlisted.
		var cuID int64
		if cu := sc.urls[[]string{"manual", "a", "b"}[int(p/3)%3]]; cu != nil {
			cuID = cu.id
		}
		body := map[string]any{"videos": []map[string]any{{"url": u, "channel_url_id": cuID}}}
		if w := sendJSON(sc.t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available-urls", sc.id), body); w.Code != http.StatusOK {
			sc.fatalf("adding %s = %d %s", u, w.Code, w.Body.String())
		}
		sc.expect(before, liveRows(sc.t, sc.id), map[string][]consts.DownloadStatus{u: {consts.DLStatusAvailable}})
	}},
	actCancel: {"cancel a download", func(sc *liveScenario, p byte) {
		before := liveRows(sc.t, sc.id)
		us := sc.available(before, true)
		if len(us) == 0 || sc.unmounted { // Downloads don't start while the drive is unmounted.
			return
		}
		u := us[int(p)%len(us)]
		for len(sc.site.fetching) > 0 {
			<-sc.site.fetching // Left by downloaders making several requests.
		}
		sc.site.setSlow(u, true)
		defer sc.site.setSlow(u, false)
		path := fmt.Sprintf("/api/v1/channels/%d/available", sc.id)
		if w := sendJSON(sc.t, http.MethodPost, path, map[string]any{"action": "download", "video_ids": []int64{before[u].id}}); w.Code != http.StatusAccepted {
			sc.fatalf("downloading %s = %d %s", u, w.Code, w.Body.String())
		}
		sc.overrides[u] = nil
		select {
		case <-sc.site.fetching:
		case <-time.After(liveTimeout):
			sc.fatalf("%s: its file download never started", u)
		}
		if w := sendRequest(sc.t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/cancel-download/%d", sc.id, before[u].id), "", ""); w.Code != http.StatusOK {
			sc.fatalf("canceling %s = %d %s", u, w.Code, w.Body.String())
		}
		waitLive(sc.t, sc.id)
		sc.expect(before, liveRows(sc.t, sc.id), map[string][]consts.DownloadStatus{u: {consts.DLStatusCanceled}})
	}},
	actUnmount: {"unmount or remount the drive", func(sc *liveScenario, _ byte) {
		from, to := sc.dir, sc.dir+".unmounted"
		if sc.unmounted {
			from, to = to, from
		}
		if err := os.Rename(from, to); err != nil {
			sc.fatalf("moving %q to %q: %v", from, to, err)
		}
		sc.unmounted = !sc.unmounted
	}},
}

// liveSeeds cover each action at least once, with settings ranging from the defaults to every option set.
var liveSeeds = []struct {
	settings, actions []byte
	title             string
}{
	{[]byte{0}, []byte{actCrawl, 0, actUpload, 0, actUnmount, 0, actCrawl, 0, actManualDownload, 0, actUnmount, 0, actCrawl, 0, actRerunMetarr, 0, actDelete, 0x80, actCrawl, 0}, ""},
	{[]byte{1}, []byte{actCrawl, 0, actDownloadAll, 0, actUpload, 9, actCrawl, 0, actDownloadAs, 0x0f, actEditAuto, 0, actEditPauseB, 0, actUpload, 4, actCrawl, 0, actDismiss, 0},
		"AC/DC — Live: \"Encore\" <2026> 🎸"},
	{[]byte{2, 1, 0, 1, 3}, []byte{actIgnoreCrawl, 0, actUpload, 0, actCrawl, 0, actToggleGone, 5, actDownloadAll, 0, actToggleGone, 5, actEditUnchanged, 0, actCrawl, 0, actManualDownload, 0, actDownloadAll, 0},
		"日本語のタイトル {{year}} %(id)s\nsecond line"},
	{[]byte{3, 1}, []byte{actCrawl, 0, actCancel, 0, actDownloadAll, 0, actEditToggleC, 0, actCrawlOne, 2, actEditFilters, 0, actEditToggleC, 0, actAddAvailable, 4, actDownloadAs, 0x22, actRerunMetarr, 0},
		"  spaced  out  "},
	{[]byte{5, 1}, []byte{actCrawl, 0, actUnmount, 0, actDownloadAll, 0, actCrawl, 0, actUnmount, 0, actDownloadAll, 0, actUnmount, 0, actDelete, 0x81, actUnmount, 0, actCrawl, 0},
		"Ünïcödé ✓ ‘quotes’ / slashes \\ and {{video_title}}"},
	{[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, []byte{actCrawl, 0, actRerunMetarr, 0, actUpload, 1, actEditAuto, 0, actCrawl, 0, actDownloadAs, 1},
		strings.Repeat("A very long title ", 20)},
	{[]byte{0}, []byte{actCrawl, 0, actRerunMetarr, 0}, `Hello, "World"`},
}

// FuzzLiveChannel adds a channel with fuzzed settings whose URLs are local feeds of tiny videos, then does what a user
// would, in a fuzzed order: crawls, edits the channel, downloads, dismisses, deletes and cancels videos, re-runs Metarr,
// and adds video URLs, while videos are uploaded to and deleted from the site. After each action it checks the stored
// videos changed only as the channel's settings say, and that files, the Available list and notifications match.
func FuzzLiveChannel(f *testing.F) {
	for _, s := range liveSeeds {
		f.Add(s.settings, s.actions, s.title)
	}
	f.Fuzz(func(t *testing.T, settings, actions []byte, title string) {
		setupLive(t)
		sc := newLiveScenario(t, settings, title)
		sc.check(liveRows(t, sc.id))
		for i := 0; i+1 < len(actions) && i/2 < liveMaxActions; i += 2 {
			a := liveActions[int(actions[i])%len(liveActions)]
			sc.step = fmt.Sprintf("step %d (%s, %d)", i/2+1, a.name, actions[i+1])
			t.Log(sc.step)
			a.run(sc, actions[i+1])
			sc.check(liveRows(t, sc.id))
			if t.Failed() {
				return
			}
		}
	})
}
