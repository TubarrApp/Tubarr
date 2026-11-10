package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"html"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log"
	"maps"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/blocking"
	"tubarr/internal/database"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/file"
	"tubarr/internal/limits"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/queue"
	"tubarr/internal/repo"
	"tubarr/internal/state"
	"unicode"
	"unicode/utf8"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/viper"
)

// ******************************** Server fuzz ***************************************************************************************

// fuzzRoute is an API route to send fuzzed requests to. Routes that start crawls or downloads, contact outside services,
// or read or write arbitrary server paths are left out, so fuzzing stays offline and can't touch real files.
type fuzzRoute struct {
	method, path string
}

var fuzzRoutes = []fuzzRoute{
	{http.MethodPost, "/metarr-logs"},
	{http.MethodGet, "/api/v1/channels/all"},
	{http.MethodPost, "/api/v1/channels/add"},
	{http.MethodGet, "/api/v1/channels/{p}"},
	{http.MethodGet, "/api/v1/channels/{p}/latest-downloads"},
	{http.MethodGet, "/api/v1/channels/{p}/all-videos"},
	{http.MethodPut, "/api/v1/channels/{p}/update"},
	{http.MethodDelete, "/api/v1/channels/{p}/delete-videos"},
	{http.MethodPost, "/api/v1/channels/{p}/notification-seen"},
	{http.MethodPost, "/api/v1/channels/{p}/toggle-pause"},
	{http.MethodGet, "/api/v1/downloads"},
	{http.MethodPost, "/api/v1/downloads/cancel"},
	{http.MethodGet, "/api/v1/videos/available"},
	{http.MethodGet, "/api/v1/logs"},
	{http.MethodGet, "/api/v1/logs/metarr"},
	{http.MethodGet, "/api/v1/logs/level"},
	{http.MethodPost, "/api/v1/logs/level/{p}"},
	{http.MethodGet, "/api/v1/blocked-domains"},
	{http.MethodDelete, "/api/v1/blocked-domains/{p}"},
	{http.MethodGet, "/api/v1/settings/crawl-concurrency"},
	{http.MethodPut, "/api/v1/settings/crawl-concurrency"},
	{http.MethodGet, "/api/v1/settings/global-download-concurrency"},
	{http.MethodPut, "/api/v1/settings/global-download-concurrency"},
	{http.MethodGet, "/api/v1/settings/domain-limits"},
	{http.MethodPut, "/api/v1/settings/domain-limits"},
	{http.MethodDelete, "/api/v1/settings/domain-limits/{p}"},
	{http.MethodGet, "/api/v1/settings/flaresolverr-url"},
	{http.MethodPut, "/api/v1/settings/flaresolverr-url"},
	{http.MethodGet, "/api/v1/settings/scrape-config-file"},
	{http.MethodGet, "/api/v1/settings/scrape-config-file/sites"},
	{http.MethodDelete, "/api/v1/channels/{p}/delete"},
}

// testDir is the working directory for this package's tests, holding the test database and the "videos" and "json"
// folders channels are saved with (folders must exist to be saved). Removed when the tests finish.
var testDir string

// pkgDir is this package's source directory (fuzz workers start in the coordinator's temporary directory, not here).
var pkgDir string

// TestMain runs the package's tests from a temporary directory, so nothing is created in the source tree.
func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	pkgDir = filepath.Dir(file)
	dir, err := os.MkdirTemp("", "tubarr-server-test")
	if err != nil {
		log.Fatal(err)
	}
	testDir = dir
	if err := os.Chdir(dir); err != nil {
		log.Fatal(err)
	}
	for _, d := range []string{"videos", "json"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			log.Fatal(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// urlPattern matches URLs, whose slashes are part of the address rather than a file path.
var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'\[\]{},|]*`)

// pathOutsideDir reports whether s could name a path outside the working directory: a slash outside a URL (an absolute
// or nested path), or ".." anywhere.
func pathOutsideDir(s string) bool {
	return strings.Contains(s, "..") || strings.Contains(urlPattern.ReplaceAllString(s, ""), "/")
}

// fuzzServer is the router and database shared by every fuzz iteration in a process.
var fuzzServer struct {
	once    sync.Once
	handler http.Handler
	ss      *serverStore // The router's queues, for tests to queue videos the API's handlers see.
	db      *sql.DB
	dir     string
	logBuf  *bytes.Buffer
	logMu   sync.Mutex
	initErr error
}

// syncWriter makes a buffer safe for the request logger, which may write from several goroutines.
type syncWriter struct{}

func (syncWriter) Write(p []byte) (int, error) {
	fuzzServer.logMu.Lock()
	defer fuzzServer.logMu.Unlock()
	return fuzzServer.logBuf.Write(p)
}

// setupFuzzServer builds the real router on a temporary database, with one channel added through the API.
func setupFuzzServer(t *testing.T) {
	t.Helper()
	fuzzServer.once.Do(func() {
		logger.Pl.Console = io.Discard
		dir := testDir
		fuzzServer.dir = dir
		paths.HomeTubarrDir = dir
		paths.DBFilePath = filepath.Join(dir, "tubarr.db")

		// Capture the request logger, which is where chi's Recoverer reports recovered panics.
		fuzzServer.logBuf = &bytes.Buffer{}
		middleware.DefaultLogger = middleware.RequestLogger(&middleware.DefaultLogFormatter{Logger: log.New(syncWriter{}, "", 0), NoColor: true})

		dbase, err := database.InitDB()
		if err != nil {
			fuzzServer.initErr = err
			return
		}
		store, err := repo.InitStores(dbase)
		if err != nil {
			fuzzServer.initErr = err
			return
		}
		// StaticHandler fails unless a web folder sits next to the executable, which here is the test binary.
		exe, err := os.Executable()
		if err != nil {
			fuzzServer.initErr = err
			return
		}
		webDir := filepath.Join(filepath.Dir(exe), "web")
		if err := os.MkdirAll(webDir, 0o755); err != nil {
			fuzzServer.initErr = err
			return
		}
		for _, page := range []string{"index.html", "404.html"} {
			if err := os.WriteFile(filepath.Join(webDir, page), []byte("<html></html>"), 0o644); err != nil {
				fuzzServer.initErr = err
				return
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		pp := queue.NewPostProcessingQueue(store)
		ss := serverStore{
			s: store, cs: store.ChannelStore(), ds: store.DownloadStore(), vs: store.VideoStore(),
			tss: store.TubarrSettingsStore(), dq: queue.NewDownloadQueue(ctx, store, pp), pp: pp, db: dbase, ctx: ctx, cancel: cancel,
		}
		if fuzzServer.handler, err = NewRouter(ss); err != nil {
			fuzzServer.initErr = err
			return
		}
		fuzzServer.ss = &ss
		fuzzServer.db = dbase

		form := url.Values{
			"name":                           {"Fuzz Channel"},
			"urls":                           {"https://example.com/@fuzz"},
			jsonkeys.SettingsVideoDirectory:  {filepath.Join(dir, "videos")},
			jsonkeys.SettingsJSONDirectory:   {filepath.Join(dir, "json")},
			jsonkeys.SettingsMaxConcurrency:  {"1"},
			jsonkeys.SettingsCrawlFreq:       {"30"},
			jsonkeys.SettingsDownloadRetries: {"1"},
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		if w.Code >= 300 {
			fuzzServer.initErr = fmt.Errorf("adding the setup channel failed: %d %s", w.Code, w.Body.String())
		}
	})
	if fuzzServer.initErr != nil {
		t.Fatal(fuzzServer.initErr)
	}
}

// dbSnapshot returns a summary of the stored data, to tell whether a request changed it.
func dbSnapshot(t *testing.T) string {
	t.Helper()
	db := fuzzServer.db
	var b strings.Builder
	for _, table := range []string{consts.DBChannels, consts.DBChannelURLs, consts.DBNotifications, consts.DBSettings} {
		rows, err := db.Query(fmt.Sprintf("SELECT * FROM %s ORDER BY 1", table))
		if err != nil {
			t.Fatalf("snapshot of %s: %v", table, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for i, v := range vals {
				if raw, ok := v.([]byte); ok {
					vals[i] = string(raw)
				}
			}
			fmt.Fprintf(&b, "%s:%v\n", table, vals)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("reading %s: %v", table, err)
		}
		_ = rows.Close()
	}
	return b.String()
}

// TestRejectedUpdateChangesNothing tests that an update rejected for an invalid field leaves the channel unchanged.
func TestRejectedUpdateChangesNothing(t *testing.T) {
	setupFuzzServer(t)
	before := dbSnapshot(t)

	form := url.Values{"name": {"Renamed"}, jsonkeys.SettingsCrawlFreq: {"abc"}}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/1/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid crawl frequency, got %d %s", w.Code, w.Body.String())
	}
	if after := dbSnapshot(t); after != before {
		t.Errorf("rejected update still changed stored data:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestUpdateRejectsMissingOrNonHTTPURLs tests that an update can't remove every URL or add one yt-dlp could read as an option.
func TestUpdateRejectsMissingOrNonHTTPURLs(t *testing.T) {
	setupFuzzServer(t)
	base := url.Values{
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
	}
	for _, urls := range []string{"", "--exec=touch-x", "file:///etc/passwd", "example.com/@x"} {
		before := dbSnapshot(t)
		form := base.Clone()
		form.Set("urls", urls)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/1/update", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("urls %q: expected 400, got %d %s", urls, w.Code, w.Body.String())
		}
		if after := dbSnapshot(t); after != before {
			t.Errorf("urls %q: rejected update still changed stored data", urls)
		}
	}
}

// TestAddNeverCreatesDirectories tests that adding a channel with a missing folder is refused without creating it (it may
// be on an unmounted drive), and accepted once the folder exists.
func TestAddNeverCreatesDirectories(t *testing.T) {
	setupFuzzServer(t)
	videoDir := filepath.Join(fuzzServer.dir, "new-videos")
	form := url.Values{
		"name":                           {"Dir Test"},
		"urls":                           {"https://example.com/@dirtest"},
		jsonkeys.SettingsVideoDirectory:  {videoDir},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
	}
	add := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w.Code
	}

	if code := add(); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing video folder, got %d", code)
	}
	if _, err := os.Stat(videoDir); !os.IsNotExist(err) {
		t.Errorf("refused add created video directory %q (stat error: %v)", videoDir, err)
	}

	if err := os.Mkdir(videoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if code := add(); code >= 300 {
		t.Fatalf("expected the add to succeed once the folder exists, got %d", code)
	}
}

// TestCrossSiteRequestsBlocked tests that another website can't make changes through a visitor's browser (CSRF).
func TestCrossSiteRequestsBlocked(t *testing.T) {
	setupFuzzServer(t)
	tests := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"cross-site fetch", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"foreign origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"same origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"no browser headers (CLI, Metarr)", nil, http.StatusOK},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/crawl-concurrency", strings.NewReader("limit=2"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range tt.headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		if w.Code != tt.want {
			t.Errorf("%s: got %d, want %d (%s)", tt.name, w.Code, tt.want, strings.TrimSpace(w.Body.String()))
		}
	}
}

// FuzzAPI sends fuzzed requests to the API, and checks no handler panics, and that rejected requests change nothing.
func FuzzAPI(f *testing.F) {
	for i := range fuzzRoutes {
		f.Add(uint8(i), "1", "")
		f.Add(uint8(i), "1", "name=x&urls=https%3A%2F%2Fa.example&video_directory=videos&crawl_freq=abc&download_concurrency=-5")
		f.Add(uint8(i), "999999", "concurrency=99999999999999999999&url=http%3A%2F%2Flocalhost%3A8191")
		f.Add(uint8(i), "%2F..%2F", "hostname=a.example&concurrency=0&filters=title%3Acontains%3Ax%3Amust")
		f.Add(uint8(i), "-1", "url_settings=%7B%22x%22%3A1%7D&metarr_meta_ops=%3A%3A%3A")
	}
	f.Fuzz(func(t *testing.T, routeIdx uint8, param, body string) {
		setupFuzzServer(t)
		route := fuzzRoutes[int(routeIdx)%len(fuzzRoutes)]

		// Never start an ignore-run crawl, which would contact the network.
		form, err := url.ParseQuery(body)
		if err != nil {
			return
		}
		form.Del("ignore_run")

		// Only allow paths inside the test's working directory.
		for _, vals := range form {
			if slices.ContainsFunc(vals, pathOutsideDir) {
				return
			}
		}

		path := strings.ReplaceAll(route.path, "{p}", url.PathEscape(param))
		if _, err := url.ParseRequestURI(path); err != nil {
			return
		}
		req := httptest.NewRequest(route.method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		before := dbSnapshot(t)
		fuzzServer.logMu.Lock()
		fuzzServer.logBuf.Reset()
		fuzzServer.logMu.Unlock()

		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)

		fuzzServer.logMu.Lock()
		logged := fuzzServer.logBuf.String()
		fuzzServer.logMu.Unlock()
		if strings.Contains(logged, "panic") {
			t.Fatalf("%s %s with %q panicked:\n%s", route.method, path, form.Encode(), logged)
		}
		// The update route's partial writes are reproduced by TestRejectedUpdateChangesNothing, so fuzzing can look past them.
		if w.Code >= 400 && w.Code < 500 && route.path != "/api/v1/channels/{p}/update" {
			if after := dbSnapshot(t); after != before {
				t.Errorf("%s %s with %q was rejected (%d) but changed stored data", route.method, path, form.Encode(), w.Code)
			}
		}
		if w.Code >= 500 {
			t.Logf("5xx: %s %s with %q -> %d %s", route.method, path, form.Encode(), w.Code, strings.TrimSpace(w.Body.String()))
		}
	})
}

// ******************************** Available *****************************************************************************************

// TestAvailableVideosAPI tests a channel added without auto download, and listing and dismissing its available videos.
func TestAvailableVideosAPI(t *testing.T) {
	setupFuzzServer(t)
	serve := func(method, path, contentType, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}

	form := url.Values{
		"name":                           {"Review Channel"},
		"urls":                           {"https://example.com/@review"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
		jsonkeys.MetarrMetaOps:           {"artist:set:Cats"},
	}
	if w := serve(http.MethodPost, "/api/v1/channels/add", "application/x-www-form-urlencoded", form.Encode()); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}

	var chanID, cuID int64
	if err := fuzzServer.db.QueryRow("SELECT c.id, u.id FROM channels c JOIN channel_urls u ON u.channel_id = c.id WHERE c.name = 'Review Channel' AND u.url = 'https://example.com/@review'").Scan(&chanID, &cuID); err != nil {
		t.Fatal(err)
	}

	// The setting is stored and returned.
	w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d", chanID), "", "")
	var channel struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &channel); err != nil {
		t.Fatal(err)
	}
	if channel.Settings[jsonkeys.SettingsAutoDownload] != false {
		t.Errorf("expected auto_download false, got settings %v", channel.Settings)
	}

	vs := repo.GetVideoStore(fuzzServer.db)
	if _, err := vs.AddAvailableVideos([]*models.Video{
		{ChannelURLID: cuID, ChannelURL: "https://example.com/@review", URL: "https://example.com/v/a", Metadata: models.VideoMetadata{Title: "A", ThumbnailURL: "https://example.com/a.jpg"}},
		{ChannelURLID: cuID, ChannelURL: "https://example.com/@review", URL: "https://example.com/v/b", Metadata: models.VideoMetadata{Title: "B"}},
	}, chanID); err != nil {
		t.Fatal(err)
	}

	listAvailable := func() []models.AvailableVideo {
		t.Helper()
		w := serve(http.MethodGet, "/api/v1/videos/available", "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("listing available videos: %d %s", w.Code, w.Body.String())
		}
		var videos []models.AvailableVideo
		if err := json.Unmarshal(w.Body.Bytes(), &videos); err != nil {
			t.Fatal(err)
		}
		return videos
	}
	videos := listAvailable()
	if len(videos) != 2 || videos[0].ChannelName != "Review Channel" {
		t.Fatalf("expected 2 available videos from Review Channel, got %+v", videos)
	}

	// Download As is filled with the channel URL's directories and ops, and the video's earlier overrides if it has any.
	type overrideOps struct {
		Defaults models.OverrideLines  `json:"defaults"`
		Saved    *models.OverrideLines `json:"saved"`
	}
	getOverrideOps := func(videoID int64) overrideOps {
		t.Helper()
		w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videoID), "", "")
		var got overrideOps
		if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
			t.Fatalf("override ops for video %d: %d %s", videoID, w.Code, w.Body.String())
		}
		return got
	}
	if got := getOverrideOps(videos[0].ID); got.Defaults.MetaOps != "artist:set:Cats" || got.Defaults.VideoDir != form.Get(jsonkeys.SettingsVideoDirectory) || got.Saved != nil {
		t.Errorf("override ops: got %+v, want the channel URL's and nothing saved", got)
	}
	if w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d", chanID, cuID+1000), "", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for another channel's URL, got %d", w.Code)
	}
	if w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=999999", chanID, cuID), "", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown video, got %d", w.Code)
	}

	// Load fills a Download As box from an ops file.
	opsFile := filepath.Join(t.TempDir(), "meta-ops.txt")
	if err := os.WriteFile(opsFile, []byte("album:set:Pets\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loadOpsFile := func(query string) *httptest.ResponseRecorder {
		return serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d/file?%s", chanID, cuID, query), "", "")
	}
	if w := loadOpsFile("box=meta_ops&path=" + url.QueryEscape(opsFile)); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"lines":["album:set:Pets"]}` {
		t.Errorf("loading an ops file: %d %s", w.Code, w.Body.String())
	}
	if w := loadOpsFile("box=meta_ops"); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without a path, got %d", w.Code)
	}
	if w := loadOpsFile("box=meta_ops&path=" + url.QueryEscape(opsFile+".missing")); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a missing file, got %d", w.Code)
	}

	// Overrides are rejected when invalid or not for a download, leaving the videos available.
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"invalid op":  fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"meta_ops":"artist:bogus:x"}}`, videos[0].ID),
		"file as dir": fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"video_dir":%q}}`, videos[0].ID, notADir),
		"dismiss":     fmt.Sprintf(`{"action":"dismiss","video_ids":[%d],"overrides":{"filename":"x"}}`, videos[0].ID),
		"unknown key": fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"name":"x"}}`, videos[0].ID),
	} {
		if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d %s", name, w.Code, w.Body.String())
		}
	}

	// So are a filename or thumbnail for several videos, and options for videos from more than one channel URL.
	noURL, err := fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url, finished, ignored, url) VALUES (?, '', 0, 0, 'https://example.com/v/no-url')`, chanID)
	if err != nil {
		t.Fatal(err)
	}
	noURLID, _ := noURL.LastInsertId()
	for body, want := range map[string]string{
		fmt.Sprintf(`{"action":"download","video_ids":[%d,%d],"overrides":{"filename":"x"}}`, videos[0].ID, videos[1].ID):                          "not several",
		fmt.Sprintf(`{"action":"download","video_ids":[%d,%d],"overrides":{"thumbnail":"https://example.com/t.jpg"}}`, videos[0].ID, videos[1].ID): "not several",
		fmt.Sprintf(`{"action":"download","video_ids":[%d,%d],"overrides":{"meta_ops":"album:set:Pets"}}`, videos[0].ID, noURLID):                  "one channel URL",
	} {
		if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", body); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: got %d %s, want 400 saying %q", body, w.Code, w.Body.String(), want)
		}
	}
	if remaining := listAvailable(); len(remaining) != 2 {
		t.Fatalf("expected rejected overrides to leave 2 videos available, got %d", len(remaining))
	}

	// Download As reports a video already taken elsewhere, rather than looking like it worked.
	if _, err := vs.ClaimVideos(chanID, []int64{videos[0].ID}, app.AvailableStatuses, consts.DLStatusQueued); err != nil {
		t.Fatal(err)
	}
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"filename":"x"}}`, videos[0].ID)); w.Code != http.StatusConflict {
		t.Errorf("already downloading: expected 409, got %d %s", w.Code, w.Body.String())
	}

	// So does Download As with only the channel URL's options, though they aren't kept.
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"download","video_ids":[%d],"overrides":{"meta_ops":"artist:set:Cats"}}`, videos[0].ID)); w.Code != http.StatusConflict {
		t.Errorf("already downloading, with the channel URL's options: expected 409, got %d %s", w.Code, w.Body.String())
	}

	// Download As for several videos from one channel URL gets past its checks too.
	if _, err := vs.ClaimVideos(chanID, []int64{videos[1].ID}, app.AvailableStatuses, consts.DLStatusQueued); err != nil {
		t.Fatal(err)
	}
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"download","video_ids":[%d,%d],"overrides":{"meta_ops":"album:set:Pets"}}`, videos[0].ID, videos[1].ID)); w.Code != http.StatusConflict {
		t.Errorf("several videos already downloading: expected 409, got %d %s", w.Code, w.Body.String())
	}
	if _, err := vs.ClaimVideos(chanID, []int64{videos[1].ID}, []consts.DownloadStatus{consts.DLStatusQueued}, consts.DLStatusAvailable); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.ClaimVideos(chanID, []int64{videos[0].ID}, []consts.DownloadStatus{consts.DLStatusQueued}, consts.DLStatusFailed); err != nil {
		t.Fatal(err)
	}

	// A video's earlier overrides come with the channel URL's defaults.
	metaOps, err := parsing.ParseMetaOps([]string{"album:set:Pets"})
	if err != nil {
		t.Fatal(err)
	}
	if err := vs.SetVideoOverrides([]int64{videos[0].ID}, &models.VideoOverrides{Filename: "Chosen", OutputDir: "/chosen", MetaOps: metaOps}); err != nil {
		t.Fatal(err)
	}
	if got := getOverrideOps(videos[0].ID); got.Saved == nil || got.Saved.Filename != "Chosen" || got.Saved.OutputDir != "/chosen" || got.Saved.MetaOps != "album:set:Pets" || got.Defaults.MetaOps != "artist:set:Cats" {
		t.Errorf("earlier overrides: got %+v", got)
	}

	// Available marks the video with them customized.
	for _, v := range listAvailable() {
		if v.Customized != (v.ID == videos[0].ID) {
			t.Errorf("video %d: got customized %t", v.ID, v.Customized)
		}
	}

	// Unknown actions are rejected, and dismissing removes the video from the list.
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"delete","video_ids":[%d]}`, videos[0].ID)); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an unknown action, got %d", w.Code)
	}
	w = serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), "application/json", fmt.Sprintf(`{"action":"dismiss","video_ids":[%d]}`, videos[0].ID))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("dismissing: %d %s", w.Code, w.Body.String())
	}
	if remaining := listAvailable(); len(remaining) != 1 || remaining[0].ID != videos[1].ID {
		t.Errorf("expected only video %d to remain, got %+v", videos[1].ID, remaining)
	}

	// Edit & Run stores a downloaded video's edited options before running Metarr, only for a video waiting for it.
	res, err := fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
		VALUES (?, ?, 'https://example.com/@review', 1, 0, 'https://example.com/v/done', ?)`, chanID, cuID, consts.MetarrStatusFailed)
	if err != nil {
		t.Fatal(err)
	}
	pendingID, _ := res.LastInsertId()
	runMetarr := func(body string) int {
		return serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/run-metarr", chanID), "application/json", body).Code
	}
	out := filepath.Join(fuzzServer.dir, "videos") // An output folder must exist.
	if code := runMetarr(fmt.Sprintf(`{"overrides":{"output_dir":%q}}`, out)); code != http.StatusBadRequest {
		t.Errorf("overrides without videos: got %d, want 400", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"meta_ops":"artist:bogus:x"}}`, pendingID)); code != http.StatusBadRequest {
		t.Errorf("invalid op: got %d, want 400", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{},"metarr":{"metarr_rename_style":"bogus"}}`, pendingID)); code != http.StatusBadRequest {
		t.Errorf("invalid Metarr arg: got %d, want 400", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"output_dir":%q}}`, videos[1].ID, out)); code != http.StatusConflict {
		t.Errorf("video not waiting for Metarr: got %d, want 409", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"output_dir":%q,"meta_ops":"album:set:Pets"},"metarr":{"metarr_output_ext":"mkv"}}`, pendingID, out)); code != http.StatusAccepted {
		t.Fatalf("edit & run: got %d, want 202", code)
	}
	if o, err := vs.GetVideoOverrides(pendingID); err != nil || o == nil || o.OutputDir != out || len(o.MetaOps) != 1 || o.OutputExt != "mkv" {
		t.Errorf("edit & run stored overrides %+v, %v", o, err)
	}

	// Edit & Run with only the channel URL's options, its video directory typed in, clears the video's own.
	res, err = fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
		VALUES (?, ?, 'https://example.com/@review', 1, 0, 'https://example.com/v/reverted', ?)`, chanID, cuID, consts.MetarrStatusFailed)
	if err != nil {
		t.Fatal(err)
	}
	revertedID, _ := res.LastInsertId()
	if err := vs.SetVideoOverrides([]int64{revertedID}, &models.VideoOverrides{Filename: "Chosen"}); err != nil {
		t.Fatal(err)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"meta_ops":"artist:set:Cats","video_dir":%q}}`, revertedID, out)); code != http.StatusAccepted {
		t.Fatalf("edit & run with the channel URL's options: got %d, want 202", code)
	}
	if o, err := vs.GetVideoOverrides(revertedID); err != nil || o != nil {
		t.Errorf("edit & run with the channel URL's options kept overrides %+v, %v", o, err)
	}

	// Edit & Re-Run takes a video Metarr already processed, only as a re-run of given videos.
	res, err = fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
		VALUES (?, ?, 'https://example.com/@review', 1, 0, 'https://example.com/v/processed', ?)`, chanID, cuID, consts.MetarrStatusDone)
	if err != nil {
		t.Fatal(err)
	}
	doneID, _ := res.LastInsertId()
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"meta_ops":"album:set:Fixed"}}`, doneID)); code != http.StatusConflict {
		t.Errorf("edit & run on a processed video: got %d, want 409", code)
	}
	if code := runMetarr(`{"rerun":true}`); code != http.StatusBadRequest {
		t.Errorf("re-run without videos: got %d, want 400", code)
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d],"overrides":{"meta_ops":"album:set:Fixed"},"rerun":true}`, doneID)); code != http.StatusAccepted {
		t.Fatalf("edit & re-run: got %d, want 202", code)
	}
	if o, err := vs.GetVideoOverrides(doneID); err != nil || o == nil || len(o.MetaOps) != 1 || o.MetaOps[0].OpValue != "Fixed" {
		t.Errorf("edit & re-run stored overrides %+v, %v", o, err)
	}

	// Edit & Re-Process gives several processed videos from one channel URL the same options, but no filename or custom
	// thumbnail, and not videos from another channel URL. Fresh videos, as the runs above may still hold theirs.
	addFinished := func(videoURL string, channelURLID any, status consts.MetarrStatus) int64 {
		t.Helper()
		res, err := fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
			VALUES (?, ?, 'https://example.com/@review', 1, 0, ?, ?)`, chanID, channelURLID, videoURL, status)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	firstID := addFinished("https://example.com/v/several-1", cuID, consts.MetarrStatusDone)
	secondID := addFinished("https://example.com/v/several-2", cuID, consts.MetarrStatusDone)
	otherURLID := addFinished("https://example.com/v/several-no-url", nil, consts.MetarrStatusDone)
	for _, tc := range []struct {
		body string
		code int
		want string
	}{
		{fmt.Sprintf(`{"video_ids":[%d,%d],"overrides":{"filename":"x"},"rerun":true}`, firstID, secondID), http.StatusBadRequest, "not several"},
		{fmt.Sprintf(`{"video_ids":[%d,%d],"overrides":{"thumbnail":"https://example.com/t.jpg"},"rerun":true}`, firstID, secondID), http.StatusBadRequest, "not several"},
		{fmt.Sprintf(`{"video_ids":[%d,%d],"overrides":{"meta_ops":"album:set:Both"},"rerun":true}`, firstID, otherURLID), http.StatusBadRequest, "one channel URL"},
		{fmt.Sprintf(`{"video_ids":[%d,%d],"overrides":{"meta_ops":"album:set:Both"}}`, firstID, secondID), http.StatusConflict, "isn't waiting"},
	} {
		w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/run-metarr", chanID), "application/json", tc.body)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s: got %d %s, want %d saying %q", tc.body, w.Code, w.Body.String(), tc.code, tc.want)
		}
	}
	for _, id := range []int64{firstID, secondID, otherURLID} {
		if o, err := vs.GetVideoOverrides(id); err != nil || o != nil {
			t.Errorf("rejected edits stored overrides %+v, %v for video %d", o, err, id)
		}
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d,%d],"overrides":{"meta_ops":"album:set:Both"},"rerun":true}`, firstID, secondID)); code != http.StatusAccepted {
		t.Fatalf("edit & re-run on several videos: got %d, want 202", code)
	}
	for _, id := range []int64{firstID, secondID} {
		if o, err := vs.GetVideoOverrides(id); err != nil || o == nil || len(o.MetaOps) != 1 || o.MetaOps[0].OpValue != "Both" {
			t.Errorf("edit & re-run on several videos stored overrides %+v, %v for video %d", o, err, id)
		}
	}

	// Edit & Post-Process does the same for several videos not yet processed.
	pendingIDs := []int64{
		addFinished("https://example.com/v/several-pending", cuID, consts.MetarrStatusPending),
		addFinished("https://example.com/v/several-failed", cuID, consts.MetarrStatusFailed),
	}
	if code := runMetarr(fmt.Sprintf(`{"video_ids":[%d,%d],"overrides":{"meta_ops":"album:set:Pending"}}`, pendingIDs[0], pendingIDs[1])); code != http.StatusAccepted {
		t.Fatalf("edit & run on several videos: got %d, want 202", code)
	}
	for _, id := range pendingIDs {
		if o, err := vs.GetVideoOverrides(id); err != nil || o == nil || len(o.MetaOps) != 1 || o.MetaOps[0].OpValue != "Pending" {
			t.Errorf("edit & run on several videos stored overrides %+v, %v for video %d", o, err, id)
		}
	}

	// The channel's downloaded videos mark those with overrides customized, for the edit dialog to warn they're replaced.
	w = serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/all-videos", chanID), "", "")
	var downloaded []models.Video
	if err := json.Unmarshal(w.Body.Bytes(), &downloaded); w.Code != http.StatusOK || err != nil {
		t.Fatalf("listing downloaded videos: %d %s", w.Code, w.Body.String())
	}
	for _, v := range downloaded {
		if want := slices.Contains(append([]int64{firstID, secondID, doneID, pendingID}, pendingIDs...), v.ID); v.Customized != want {
			t.Errorf("downloaded video %d: got customized %t, want %t", v.ID, v.Customized, want)
		}
	}
}

// TestUnavailableDestinationsRefused tests that downloads which can't reach their folders are refused when asked for,
// rather than failing later or being dropped: a Download As folder that doesn't exist, and a URL to download while the
// channel's drive is unmounted.
func TestUnavailableDestinationsRefused(t *testing.T) {
	setupFuzzServer(t)
	videoDir := filepath.Join(t.TempDir(), "videos")
	if err := os.Mkdir(videoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"name":                           {"Unmounted"},
		"urls":                           {"https://example.com/@unmounted"},
		jsonkeys.SettingsVideoDirectory:  {videoDir},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
	}
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	var chanID, cuID int64
	if err := fuzzServer.db.QueryRow("SELECT c.id, u.id FROM channels c JOIN channel_urls u ON u.channel_id = c.id WHERE c.name = 'Unmounted'").Scan(&chanID, &cuID); err != nil {
		t.Fatal(err)
	}
	defer deleteChannelNamed(t, "Unmounted")

	// Download As to a folder that doesn't exist.
	vs := repo.GetVideoStore(fuzzServer.db)
	if _, err := vs.AddAvailableVideos([]*models.Video{{ChannelURLID: cuID, ChannelURL: "https://example.com/@unmounted", URL: "https://example.com/v/u1"}}, chanID); err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM videos WHERE channel_id = ? AND url = 'https://example.com/v/u1'", chanID).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(videoDir, "missing")
	body := map[string]any{"action": "download", "video_ids": []int64{videoID}, "overrides": map[string]string{"video_dir": missing}}
	if w := sendJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/available", chanID), body); w.Code != http.StatusBadRequest {
		t.Errorf("Download As to a missing folder: %d %s, want %d", w.Code, w.Body.String(), http.StatusBadRequest)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Errorf("Download As created the missing folder %q", missing)
	}

	// A URL to download while the drive holding the video folder is unmounted.
	if err := os.Remove(videoDir); err != nil {
		t.Fatal(err)
	}
	body = map[string]any{"videos": []map[string]any{{"url": "https://example.com/v/u2", "channel_url_id": 0}}}
	if w := sendJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/download-urls", chanID), body); w.Code != http.StatusConflict {
		t.Errorf("download URL with the drive unmounted: %d %s, want %d", w.Code, w.Body.String(), http.StatusConflict)
	}
	if _, err := os.Stat(videoDir); err == nil {
		t.Errorf("the unmounted video folder %q was recreated", videoDir)
	}
}

// ******************************** Channel form fuzz *********************************************************************************

// channelFormBools are the channel page fields set by a checkbox or a dropdown, which can't hold free text. Only the
// dropdowns, which leave the setting to the profile, can be blank.
var channelFormBools = []string{"paused", "use_global_cookies", "auto_download"}

// channelFormFields are a channel page's field names, read from the page.
type channelFormFields struct {
	settings, metarr       []string
	urlSettings, urlMetarr []string
	urlNumbers             map[string]bool
}

// readChannelFormFields reads a channel page's field names.
func readChannelFormFields(t *testing.T, page string) channelFormFields {
	t.Helper()
	src := readBuiltPage(t, page)
	return channelFormFields{
		settings:    objectKeys(t, src, "formData: {", "settings: {"),
		metarr:      objectKeys(t, src, "formData: {", "metarr: {"),
		urlSettings: slices.Concat(arrayStrings(t, src, "settingsFields"), perURLFixedKeys),
		urlMetarr:   arrayStrings(t, src, "metarrFields"),
		urlNumbers:  boundKeys(bindings(src), "urlEntry.custom."),
	}
}

// fuzzedChannelForm is a channel form built from fuzz input, with the value each field was sent as.
type fuzzedChannelForm struct {
	form    url.Values
	sent    map[string]string // Main form fields, by key.
	sentURL map[string]string // Per-URL fields for wiringURLB, by key.
	junk    map[string]bool   // Fields sent the fuzzed text, whose read back value isn't checked.
}

// buildFuzzedChannelForm builds a form as the page's submitForm does, each field taking from choices one of: blank, the
// first sample, the second sample, or junk.
func buildFuzzedChannelForm(f channelFormFields, samples map[string][2]string, choices []byte, junk string) fuzzedChannelForm {
	out := fuzzedChannelForm{form: url.Values{}, sent: map[string]string{}, sentURL: map[string]string{}, junk: map[string]bool{}}
	i := 0
	pick := func(key string) (string, bool) {
		c := byte(0)
		if len(choices) > 0 {
			c = choices[i%len(choices)]
		}
		i++
		s := samples[key]
		switch {
		case key == "paused":
			return s[c%2], false
		case slices.Contains(channelFormBools, key):
			return []string{"", s[0], s[1]}[c%3], false
		}
		switch c % 4 {
		case 1:
			return s[0], false
		case 2:
			return s[1], false
		case 3:
			return junk, true
		}
		return "", false
	}

	for _, k := range slices.Concat(f.settings, f.metarr) {
		v, isJunk := pick(k)
		out.sent[k], out.junk[k] = v, isJunk
		switch {
		case k == "paused":
			if v == "true" {
				out.form.Set(k, v)
			}
		case v != "":
			out.form.Set(k, v)
		}
	}

	urlSettings, urlMetarr := map[string]any{}, map[string]any{}
	for _, k := range slices.Concat(f.urlSettings, f.urlMetarr) {
		v, isJunk := pick(k)
		if v == "" {
			continue // The page leaves out blank per-URL fields, so they inherit the channel's.
		}
		out.sentURL[k], out.junk["url:"+k] = v, isJunk
		if slices.Contains(f.urlSettings, k) {
			urlSettings[k] = urlSettingValue(k, v, f.urlNumbers[k])
		} else {
			urlMetarr[k] = urlSettingValue(k, v, f.urlNumbers[k])
		}
	}
	perURL, _ := json.Marshal(map[string]any{wiringURLB: map[string]any{"settings": urlSettings, "metarr": urlMetarr}})
	out.form.Set("url_settings", string(perURL))

	for _, k := range []string{"name", "auth_details", "notifications"} {
		v, isJunk := pick(k)
		out.sent[k], out.junk[k] = v, isJunk
		if v != "" {
			out.form.Set(k, v)
		}
	}
	out.form.Set("urls", wiringURLA+" "+wiringURLB)
	return out
}

// editPageForm builds the form the edit page sends when a channel is loaded and saved without changes.
func editPageForm(f channelFormFields, got channelDetails) url.Values {
	form := url.Values{}
	for _, k := range f.settings {
		v := got.Settings[k]
		if k == "channel_config_file" {
			v = got.ChannelConfigFile
		}
		switch s := formValue(v); {
		case k == "paused":
			if s == "true" {
				form.Set(k, s)
			}
		case s != "":
			form.Set(k, s)
		}
	}
	if got.ProfileID != 0 {
		form.Set("profile_id", strconv.FormatInt(got.ProfileID, 10))
	}
	for _, k := range f.metarr {
		if s := formValue(got.Metarr[k]); s != "" && (k != "metarr_max_cpu_usage" || s != "0") {
			form.Set(k, s)
		}
	}

	perURL := map[string]any{}
	for u, sections := range got.URLSettings {
		settings, metarr := map[string]any{}, map[string]any{}
		for k, v := range sections.Settings {
			if v != nil && v != "" && slices.Contains(f.urlSettings, k) {
				settings[k] = v
			}
		}
		for k, v := range sections.Metarr {
			switch {
			case v == nil || v == "" || !slices.Contains(f.urlMetarr, k):
			case k == "metarr_max_cpu_usage" && v == float64(0):
			case k == "metarr_transcode_video_transcode_codecs" || k == "metarr_transcode_audio_codecs" || k == "metarr_transcode_video_args":
				metarr[k] = formValue(v) // The page shows Metarr codec and encoder arg lists as text, and sends them so.
			default:
				metarr[k] = v
			}
		}
		if len(settings) > 0 || len(metarr) > 0 || sections.ProfileID != 0 {
			entry := map[string]any{"settings": settings, "metarr": metarr}
			if sections.ProfileID != 0 { // The page sends a URL's profile only when it has one.
				entry["profile_id"] = sections.ProfileID
			}
			perURL[u] = entry
		}
	}
	data, _ := json.Marshal(perURL)
	form.Set("url_settings", string(data))

	var auth []map[string]string
	for _, a := range got.AuthDetails {
		if a.Username != "" && a.LoginURL != "" {
			auth = append(auth, map[string]string{"channel_url": a.ChannelURL, "username": a.Username, "password": "", "login_url": a.LoginURL})
		}
	}
	if len(auth) > 0 {
		data, _ = json.Marshal(auth)
		form.Set("auth_details", string(data))
	}
	form.Set("name", got.Name)
	form.Set("urls", strings.Join(got.URLs, " "))
	if len(got.Notifications) > 0 {
		data, _ = json.Marshal(got.Notifications)
		form.Set("notifications", string(data))
	}
	return form
}

// FuzzChannelForm saves fuzzed combinations of channel page fields, as the add or edit page sends them, and checks:
// nothing panics or fails with a server error, a rejected save changes nothing, an accepted save reads back as sent
// (a blank field reading back blank), and loading the channel on the edit page then saving it unchanged is accepted
// and changes nothing.
func FuzzChannelForm(f *testing.F) {
	for _, edit := range []bool{false, true} {
		for _, choices := range [][]byte{{0}, {1}, {2}, {0, 1}, {0, 2}, {1, 2}, {2, 1, 0}, {3, 1}, {3, 2, 1, 0}} {
			for _, junk := range []string{"", " ", "0", "-1", "abc", "99999999999999999999", "a|b", "{{year}}", "<x>", "\n\n", "título"} {
				f.Add(edit, choices, junk)
			}
		}
	}
	f.Fuzz(func(t *testing.T, edit bool, choices []byte, junk string) {
		setupFuzzServer(t)
		if pathOutsideDir(junk) {
			return // Junk can't name a path outside the test directory.
		}
		samples := formSamples(t)
		page := "add-channel.html"
		if edit {
			page = "edit-channel.html"
		}
		fields := readChannelFormFields(t, page)
		fuzzed := buildFuzzedChannelForm(fields, samples, choices, junk)

		save := func(what string, form url.Values, id int64) (code int, body string) {
			t.Helper()
			fuzzServer.logMu.Lock()
			fuzzServer.logBuf.Reset()
			fuzzServer.logMu.Unlock()
			method, target := http.MethodPost, "/api/v1/channels/add"
			if id != 0 {
				method, target = http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id)
			}
			w := sendForm(t, method, target, form)
			fuzzServer.logMu.Lock()
			logged := fuzzServer.logBuf.String()
			fuzzServer.logMu.Unlock()
			if strings.Contains(logged, "panic") {
				t.Fatalf("%s panicked with %q:\n%s", what, form.Encode(), logged)
			}
			if w.Code >= 500 {
				t.Errorf("%s failed with a server error %d %s, form %q", what, w.Code, strings.TrimSpace(w.Body.String()), form.Encode())
			}
			return w.Code, strings.TrimSpace(w.Body.String())
		}
		channelID := func(name string) int64 {
			var id int64
			_ = fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", name).Scan(&id)
			return id
		}
		deleteChannel := func(id int64) {
			if w := sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", ""); w.Code != http.StatusNoContent {
				t.Errorf("deleting the fuzzed channel = %d %s", w.Code, w.Body.String())
			}
		}

		// The edit page edits a channel added from the first samples.
		var id int64
		if edit {
			base := buildFuzzedChannelForm(readChannelFormFields(t, "add-channel.html"), samples, []byte{1}, "")
			base.form.Set("name", "Fuzz Form Edit")
			if code, body := save("adding the channel to edit", base.form, 0); code >= 300 {
				t.Fatalf("adding the channel to edit = %d %s", code, body)
			}
			id = channelID("Fuzz Form Edit")
			defer deleteChannel(id)
		}

		before := dbSnapshot(t)
		code, body := save(page, fuzzed.form, id)
		if code >= 300 {
			if after := dbSnapshot(t); after != before {
				t.Errorf("%s: a rejected save (%d %s) still changed stored data, form %q\nbefore:\n%s\nafter:\n%s", page, code, body, fuzzed.form.Encode(), before, after)
			}
			return
		}
		if strings.TrimSpace(fuzzed.sent["video_directory"]) == "" {
			t.Errorf("%s: saving without the required video directory was accepted, form %q", page, fuzzed.form.Encode())
		}
		if !edit && strings.TrimSpace(fuzzed.sent["name"]) == "" {
			t.Errorf("%s: adding a channel without the required name was accepted, form %q", page, fuzzed.form.Encode())
		}
		if !edit {
			if id = channelID(fuzzed.sent["name"]); id == 0 {
				t.Fatalf("%s: the add was accepted but no channel is named %q", page, fuzzed.sent["name"])
			}
			defer deleteChannel(id)
		}

		// Each field reads back as sent, other than junk, which validation may normalize.
		var got channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
		check := func(key, sent string, read any) {
			t.Helper()
		}
		for _, k := range fields.settings {
			if fuzzed.junk[k] {
				continue
			}
			switch k {
			case "channel_config_file":
				check(k, fuzzed.sent[k], got.ChannelConfigFile)
			default:
				check(k, fuzzed.sent[k], got.Settings[k])
			}
		}
		for _, k := range fields.metarr {
			if !fuzzed.junk[k] {
				check(k, fuzzed.sent[k], got.Metarr[k])
			}
		}
		for k, v := range fuzzed.sentURL {
			if fuzzed.junk["url:"+k] {
				continue
			}
			section, _ := got.URLSettings[wiringURLB].Metarr, got.Metarr[k]
			if slices.Contains(fields.urlSettings, k) {
				section, _ = got.URLSettings[wiringURLB].Settings, got.Settings[k]
			}
			// Per-URL settings matching the channel's aren't stored, so the URL then reads the channel's.
			read := section[k]
			check("per-URL "+k, v, read)
		}
		if !fuzzed.junk["name"] && fuzzed.sent["name"] != "" && got.Name != fuzzed.sent["name"] {
			t.Errorf("%s: name was sent as %q but reads back as %q", page, fuzzed.sent["name"], got.Name)
		}
		if !fuzzed.junk["notifications"] {
			if want := notificationKeys(fuzzed.sent["notifications"]); !sameSet(notificationKeys(got.Notifications), want) {
				t.Errorf("%s: notifications were sent as %q but read back as %+v", page, want, got.Notifications)
			}
		}

		// Loading the channel on the edit page and saving it unchanged must work, and change nothing.
		resave := editPageForm(readChannelFormFields(t, "edit-channel.html"), got)
		if code, body := save("saving the edit page unchanged", resave, id); code >= 300 {
			t.Fatalf("%s: loading the saved channel on the edit page and saving it unchanged was rejected: %d %s\nform %q",
				page, code, body, resave.Encode())
		}
		var again channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &again)
		if !reflect.DeepEqual(got, again) {
			a, _ := json.MarshalIndent(got, "", " ")
			b, _ := json.MarshalIndent(again, "", " ")
			t.Errorf("%s: saving the edit page unchanged changed the channel:\nbefore: %s\nafter: %s", page, a, b)
		}
	})
}

// ******************************** Channel list **************************************************************************************

// listedChannel is a channel as the channel list returns it.
type listedChannel struct {
	Name     string         `json:"name"`
	Profile  string         `json:"profile"`
	Settings map[string]any `json:"settings"`
}

// TestChannelListShowsProfileSettings tests that the channel list shows the settings in effect, its profile's filling in
// those the channel leaves unset, and names the profile.
func TestChannelListShowsProfileSettings(t *testing.T) {
	setupFuzzServer(t)
	dir := filepath.Join(fuzzServer.dir, "list-profile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", url.Values{"name": {"List Profile"}, "crawl_freq": {"45"}, "video_directory": {dir}}); w.Code != http.StatusCreated {
		t.Fatalf("adding the profile = %d %s", w.Code, w.Body.String())
	}
	var profileID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", "List Profile").Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
	})

	form := url.Values{"name": {"List Channel"}, "urls": {"https://example.com/@list"}, "profile_id": {fmt.Sprint(profileID)}}
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code >= 300 {
		t.Fatalf("adding the channel = %d %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { deleteChannelNamed(t, "List Channel") })

	var channels []listedChannel
	fetchJSON(t, "/api/v1/channels/all", &channels)
	i := slices.IndexFunc(channels, func(c listedChannel) bool { return c.Name == "List Channel" })
	if i < 0 {
		t.Fatal("the channel isn't listed")
	}
	got := channels[i]
	if got.Profile != "List Profile" {
		t.Errorf("profile listed as %q, want %q", got.Profile, "List Profile")
	}
	if g := formValue(got.Settings["crawl_freq"]); g != "45" {
		t.Errorf("crawl frequency listed as %q, want the profile's 45", g)
	}
	if g := formValue(got.Settings["video_directory"]); g != dir {
		t.Errorf("video directory listed as %q, want the profile's %q", g, dir)
	}
}

// ******************************** Channels ******************************************************************************************

// TestActivityListsNeverNull tests the activity lists are empty rather than null when nothing is running, as the web UI
// calls includes on them.
func TestActivityListsNeverNull(t *testing.T) {
	setupFuzzServer(t)
	w := sendRequest(t, http.MethodGet, "/api/v1/channels/activity", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("activity = %d %s", w.Code, w.Body.String())
	}
	var activity map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &activity); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"jobs", "stopping", "metarr", "metarr_videos"} {
		if list, ok := activity[key]; !ok || string(list) == "null" {
			t.Errorf("activity %q = %s, want a list", key, list)
		}
	}
}

// TestDeleteChannelStopsItsCrawl tests that deleting a channel stops its running crawl.
func TestDeleteChannelStopsItsCrawl(t *testing.T) {
	setupFuzzServer(t)
	form := url.Values{
		"name":                           {"Delete Me"},
		"urls":                           {"https://example.com/@deleteme"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, req)
	if w.Code >= 300 {
		t.Fatalf("adding the channel failed: %d %s", w.Code, w.Body.String())
	}
	var id int64
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", consts.QChanID, consts.DBChannels, consts.QChanName)
	if err := fuzzServer.db.QueryRow(query, "Delete Me").Scan(&id); err != nil {
		t.Fatal(err)
	}

	ctx, unlock, ok := state.TryLockCrawl(context.Background(), id) // Stands in for a running crawl.
	if !ok {
		t.Fatal("couldn't lock the new channel's crawl")
	}
	defer unlock()

	w = httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	if ctx.Err() == nil {
		t.Error("deleting the channel left its running crawl going")
	}
}

// TestEditRenamesChannelURL tests an edited channel URL keeps its videos, and orphaned videos are reconnected.
func TestEditRenamesChannelURL(t *testing.T) {
	setupFuzzServer(t)
	const mobile, desktop = "https://m.example.com/@renamed/videos", "https://www.example.com/@renamed/videos"
	form := func(name, u string) url.Values {
		return url.Values{
			"name":                           {name},
			"urls":                           {u},
			jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
			jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
			jsonkeys.SettingsMaxConcurrency:  {"1"},
			jsonkeys.SettingsCrawlFreq:       {"30"},
			jsonkeys.SettingsDownloadRetries: {"1"},
			jsonkeys.SettingsAutoDownload:    {"false"},
		}
	}
	send := func(method, path string, values url.Values) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		if w.Code >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
	}
	s, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	// addWithVideo adds a channel with the mobile address, and a crawled video for it.
	addWithVideo := func(name string) (chanID, cuID, videoID int64) {
		t.Helper()
		send(http.MethodPost, "/api/v1/channels/add", form(name, mobile))
		c, _, err := s.ChannelStore().GetChannelModel(consts.QChanName, name, true)
		if err != nil {
			t.Fatal(err)
		}
		cu := c.URLModels[0]
		videos := []*models.Video{{ChannelURLID: cu.ID, ChannelURL: cu.URL, URL: "https://example.com/v/" + name}}
		if _, err := s.VideoStore().AddAvailableVideos(videos, c.ID); err != nil {
			t.Fatal(err)
		}
		return c.ID, cu.ID, videos[0].ID
	}
	videoChannelURL := func(videoID int64) (id sql.NullInt64, u string) {
		t.Helper()
		if err := fuzzServer.db.QueryRow("SELECT channel_url_id, channel_url FROM videos WHERE id = ?", videoID).Scan(&id, &u); err != nil {
			t.Fatal(err)
		}
		return id, u
	}

	// The edit page sends the address change as a rename.
	chanID, cuID, videoID := addWithVideo("Renamed In Place")
	edit := form("Renamed In Place", desktop)
	edit.Set("url_renames", fmt.Sprintf(`{%q:%q}`, mobile, desktop))
	send(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", chanID), edit)
	var renamedID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = ? AND url = ?", chanID, desktop).Scan(&renamedID); err != nil || renamedID != cuID {
		t.Errorf("renamed URL has ID %d (%v), want its original %d", renamedID, err, cuID)
	}
	if id, u := videoChannelURL(videoID); id.Int64 != cuID || u != desktop {
		t.Errorf("video's channel URL = %d %q, want %d %q", id.Int64, u, cuID, desktop)
	}

	// Without the rename the old URL is replaced, orphaning its video until it's reconnected by address.
	chanID, _, videoID = addWithVideo("Replaced")
	send(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", chanID), form("Replaced", desktop))
	if id, _ := videoChannelURL(videoID); id.Valid {
		t.Fatalf("video kept channel URL %d after its URL was replaced", id.Int64)
	}
	if _, err := s.VideoStore().ReconnectOrphanedVideos(); err != nil {
		t.Fatal(err)
	}
	var newID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = ? AND url = ?", chanID, desktop).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	if id, u := videoChannelURL(videoID); id.Int64 != newID || u != desktop {
		t.Errorf("orphaned video reconnected to %d %q, want %d %q", id.Int64, u, newID, desktop)
	}
}

// ******************************** Compression ***************************************************************************************

// TestResponsesCompressed tests that a browser accepting gzip gets it, with the same content, and that a client not
// accepting it gets plain responses.
func TestResponsesCompressed(t *testing.T) {
	setupFuzzServer(t)
	const path = "/api/v1/channels/all"

	get := func(encoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s with Accept-Encoding %q: status %d", path, encoding, w.Code)
		}
		return w
	}

	plain := get("")
	if got := plain.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("no gzip asked for, got Content-Encoding %q", got)
	}
	if !json.Valid(plain.Body.Bytes()) {
		t.Fatalf("plain response isn't JSON: %q", plain.Body.String())
	}

	zipped := get("gzip")
	if got := zipped.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("gzip asked for, got Content-Encoding %q", got)
	}
	zr, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unzipped, plain.Body.Bytes()) {
		t.Errorf("unzipped response differs from the plain one:\n%s\n%s", unzipped, plain.Body.String())
	}
}

// TestStaticFilesCompressedAndCached tests that a static script is compressed while the browser can still revalidate
// its cached copy, and that images aren't compressed again.
func TestStaticFilesCompressedAndCached(t *testing.T) {
	setupFuzzServer(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	webDir := filepath.Join(filepath.Dir(exe), "web")
	script := strings.Repeat("console.log('compress me');\n", 500)
	if err := os.WriteFile(filepath.Join(webDir, "zz-compress.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "zz-compress.png"), []byte("\x89PNG\r\n\x1a\n"+script), 0o644); err != nil {
		t.Fatal(err)
	}

	get := func(path, ifModifiedSince string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		if ifModifiedSince != "" {
			req.Header.Set("If-Modified-Since", ifModifiedSince)
		}
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}

	w := get("/zz-compress.js", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("script: status %d, Content-Encoding %q, want 200 and gzip", w.Code, w.Header().Get("Content-Encoding"))
	}
	if w.Body.Len() >= len(script)/2 {
		t.Errorf("script sent as %d of its %d bytes", w.Body.Len(), len(script))
	}
	lastModified := w.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("script has no Last-Modified to revalidate with")
	}

	later := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	if w := get("/zz-compress.js", later); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Errorf("revalidating the script: status %d with %d bytes, want 304 and none", w.Code, w.Body.Len())
	}

	if w := get("/zz-compress.png", ""); w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "" {
		t.Errorf("image: status %d, Content-Encoding %q, want 200 and none", w.Code, w.Header().Get("Content-Encoding"))
	}
}

// ******************************** Concurrency limits ********************************************************************************

// TestConcurrencySettingsResizeInPlace tests that saving a global limit changes the semaphore in use, counting running
// work, rather than leaving it behind.
func TestConcurrencySettingsResizeInPlace(t *testing.T) {
	setupFuzzServer(t)

	for path, sem := range map[string]*limits.Semaphore{
		"/api/v1/settings/crawl-concurrency":           limits.GlobalCrawls,
		"/api/v1/settings/global-download-concurrency": limits.GlobalDownloads,
		"/api/v1/settings/post-process-concurrency":    limits.GlobalPostProcessing,
	} {
		t.Run(path, func(t *testing.T) {
			setLimit := func(limit string) {
				t.Helper()
				if w := sendForm(t, http.MethodPut, path, url.Values{"limit": {limit}}); w.Code != http.StatusOK {
					t.Fatalf("setting %s to %s = %d %s", path, limit, w.Code, w.Body.String())
				}
			}
			setLimit("1")
			t.Cleanup(func() { setLimit("0") })

			// The limit is used up by one running.
			if !sem.TryAcquire() {
				t.Fatal("no slot free under a limit of 1")
			}
			defer sem.Release()
			if sem.TryAcquire() {
				sem.Release()
				t.Fatal("second slot given out under a limit of 1")
			}

			// Saving it again leaves it used up.
			setLimit("1")
			if sem.TryAcquire() {
				sem.Release()
				t.Fatal("re-saving the limit gave out an extra slot")
			}

			// Raising it frees one more, with the first still counted.
			setLimit("2")
			if !sem.TryAcquire() {
				t.Fatal("raising the limit freed no slot")
			}
			defer sem.Release()
			if sem.TryAcquire() {
				sem.Release()
				t.Fatal("raising the limit stopped counting the running slot")
			}
		})
	}
}

// TestDomainLimitSettingKeepsOtherKindsSlots tests that changing a domain's crawl limit leaves its download slots counted.
func TestDomainLimitSettingKeepsOtherKindsSlots(t *testing.T) {
	setupFuzzServer(t)

	const (
		path     = "/api/v1/settings/domain-limits"
		host     = "resize.limit-test.example"
		videoURL = "https://resize.limit-test.example/video"
	)
	setLimits := func(download, crawl string) {
		t.Helper()
		if w := sendForm(t, http.MethodPut, path, url.Values{"hostname": {host}, "download": {download}, "crawl": {crawl}}); w.Code != http.StatusOK {
			t.Fatalf("setting domain limits = %d %s", w.Code, w.Body.String())
		}
	}
	setLimits("1", "1")
	t.Cleanup(func() { setLimits("0", "0") })

	sem, err := limits.DomainDownloads.Acquire(t.Context(), videoURL)
	if err != nil {
		t.Fatal(err)
	}
	defer sem.Release()

	// Only the crawl limit changes, and the download slot is still taken.
	setLimits("1", "5")
	if sem.TryAcquire() {
		sem.Release()
		t.Fatal("changing the crawl limit gave out an extra download slot")
	}
}

// ******************************** Config files **************************************************************************************

// TestConfigFileResponse tests that the edit page is told which settings a channel's config file sets.
func TestConfigFileResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel.yaml")
	body := "channel-name: chan\ncrawl-freq: 30\nmetarr-output-ext: mkv\n" +
		"url-settings:\n  'https://example.com/@a':\n    settings:\n      auto-download: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := newConfigFileResponse(path)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	if want := []string{"crawl-freq", "metarr-output-ext"}; !slices.Equal(got.Settings, want) {
		t.Errorf("settings %v, want %v", got.Settings, want)
	}
	if want := []string{"auto-download"}; !slices.Equal(got.URLSettings["https://example.com/@a"], want) {
		t.Errorf("URL settings %v, want %v for https://example.com/@a", got.URLSettings, want)
	}

	if got := newConfigFileResponse(filepath.Join(t.TempDir(), "missing.yaml")); got.Error == "" {
		t.Error("no error for a missing config file")
	}
}

// TestConfigFileResponseListsNameAndProfile tests a name and profile the file sets are listed with its other settings.
func TestConfigFileResponseListsNameAndProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel.yaml")
	body := "channel-name: chan\nprofile: Shared\ncrawl-freq: 30\n" +
		"url-settings:\n" +
		"  'https://example.com/@a':\n    name: Cats\n    profile: Shared\n    settings:\n      auto-download: true\n    metarr:\n      metarr-output-ext: mkv\n" +
		"  'https://example.com/@b':\n    name: Dogs\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := newConfigFileResponse(path)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	if want := []string{"crawl-freq", "profile"}; !slices.Equal(got.Settings, want) {
		t.Errorf("settings %v, want %v", got.Settings, want)
	}
	if want := []string{"name", "profile", "auto-download", "metarr-output-ext"}; !slices.Equal(got.URLSettings["https://example.com/@a"], want) {
		t.Errorf("URL settings %v, want %v", got.URLSettings["https://example.com/@a"], want)
	}
	if want := []string{"name"}; !slices.Equal(got.URLSettings["https://example.com/@b"], want) {
		t.Errorf("URL settings %v, want %v", got.URLSettings["https://example.com/@b"], want)
	}
}

// TestScrapeConfigContentOpenError tests the rules editor is given a readable error for a file it can't open.
func TestScrapeConfigContentOpenError(t *testing.T) {
	logger.Pl.Console = io.Discard
	viper.Set(keys.ScrapeConfigFile, filepath.Join(t.TempDir(), "missing.toml"))
	t.Cleanup(func() { viper.Set(keys.ScrapeConfigFile, "") })

	w := httptest.NewRecorder()
	(&serverStore{}).handleGetScrapeConfigContent(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := w.Body.String()
	if !strings.Contains(body, "failed to open scrape config file") || strings.Contains(body, `"Op"`) {
		t.Errorf("body = %q, want a readable error", body)
	}
}

// TestScrapeConfigContentEndpoints tests loading and saving the scrape config file through the web UI's editor endpoints.
func TestScrapeConfigContentEndpoints(t *testing.T) {
	logger.Pl.Console = io.Discard
	p := filepath.Join(t.TempDir(), "rules.toml")
	original := "[[sites]]\ndomain = \"a.com\"\n  [sites.crawl]\n  selector = \"a\"\n"
	if err := os.WriteFile(p, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.Set(keys.ScrapeConfigFile, p)
	t.Cleanup(func() { viper.Set(keys.ScrapeConfigFile, "") })
	ss := &serverStore{}

	get := func() map[string]string {
		w := httptest.NewRecorder()
		ss.handleGetScrapeConfigContent(w, httptest.NewRequest(http.MethodGet, "/", nil))
		var m map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatalf("GET: %v (%s)", err, w.Body.String())
		}
		return m
	}
	put := func(content, version string) (int, map[string]any) {
		form := url.Values{"content": {content}, "version": {version}}
		r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		ss.handleSetScrapeConfigContent(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}

	loaded := get()
	if loaded["path"] != p || loaded["content"] != original || loaded["version"] == "" {
		t.Fatalf("unexpected GET response: %+v", loaded)
	}

	// Invalid rules are rejected without touching the file.
	if code, _ := put("[[sites]]\ndomain = \"b.com\"\n", loaded["version"]); code != http.StatusBadRequest {
		t.Errorf("invalid save: got HTTP %d, want 400", code)
	}
	if got, _ := os.ReadFile(p); string(got) != original {
		t.Errorf("expected the file to be untouched after an invalid save, got %q", got)
	}

	// Valid rules are saved, returning the new version.
	updated := strings.Replace(original, "a.com", "c.com", 1)
	code, saved := put(updated, loaded["version"])
	if code != http.StatusOK {
		t.Fatalf("valid save: got HTTP %d, want 200", code)
	}
	if got, _ := os.ReadFile(p); string(got) != updated {
		t.Errorf("expected the file to be saved, got %q", got)
	}

	// Saving from the outdated copy is refused, but the version from the last save works.
	if code, _ := put(original, loaded["version"]); code != http.StatusConflict {
		t.Errorf("stale save: got HTTP %d, want 409", code)
	}
	if code, _ := put(original, saved["version"].(string)); code != http.StatusOK {
		t.Errorf("save with the latest version: got HTTP %d, want 200", code)
	}

	// With no file set, there is nothing to edit.
	viper.Set(keys.ScrapeConfigFile, "")
	if loaded := get(); loaded["path"] != "" {
		t.Errorf("expected no path, got %q", loaded["path"])
	}
	if code, _ := put(original, ""); code != http.StatusBadRequest {
		t.Errorf("save with no file set: got HTTP %d, want 400", code)
	}
}

// ******************************** Crawl config **************************************************************************************

// crawlConfigChannel adds a due channel reading a config file, with a fake yt-dlp finding no videos, returning its ID and
// the fake's run record and the config file's path.
func crawlConfigChannel(t *testing.T, name string) (id int64, runs, cfg string) {
	t.Helper()
	setupFuzzServer(t)

	// yt-dlp finds nothing.
	bin := t.TempDir()
	runs = filepath.Join(bin, "runs")
	script := "#!/bin/sh\necho run >> '" + runs + "'\necho '{\"entries\":[]}'\n"
	if err := os.WriteFile(filepath.Join(bin, sharedconsts.ProgramYTDLP), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	abstractions.Set(keys.SkipAllWaits, true)
	t.Cleanup(func() { abstractions.Set(keys.SkipAllWaits, nil) })

	cfg = wiringFile(t, name+".yaml", "channel-name: "+name+"\n")
	form := webChannelForm(t, name, "https://example.com/@"+strings.ToLower(name))
	form.Set("channel_config_file", cfg)
	id = addWebChannel(t, form)

	// Due, as last crawled long ago.
	if _, err := fuzzServer.db.Exec("UPDATE channel_urls SET last_scan = ? WHERE channel_id = ?", time.Now().Add(-48*time.Hour), id); err != nil {
		t.Fatal(err)
	}
	return id, runs, cfg
}

// crawlConfigStore returns a server store over the test database.
func crawlConfigStore(t *testing.T) *serverStore {
	t.Helper()
	store, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	pp := queue.NewPostProcessingQueue(store)
	return &serverStore{
		s:  store,
		cs: store.ChannelStore(),
		ds: store.DownloadStore(),
		vs: store.VideoStore(),
		dq: queue.NewDownloadQueue(t.Context(), store, pp),
		pp: pp,
		db: fuzzServer.db,
	}
}

// waitForCrawl waits until the fake yt-dlp has run and the channel's crawl is over.
func waitForCrawl(t *testing.T, id int64, runs string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for {
		_, err := os.Stat(runs)
		active, stopping := state.CrawlingChannels()
		if err == nil && !slices.Contains(active, id) && !slices.Contains(stopping, id) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("the crawl never ran or finished")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestScheduledCrawlReadsConfigFile tests the web scheduler applies an edited config file when a crawl is due.
func TestScheduledCrawlReadsConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ScheduledConfig")
	if err := os.WriteFile(cfg, []byte("channel-name: ScheduledConfig\ncrawl-freq: 77\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	crawlConfigStore(t).crawlDueChannels(t.Context(), map[int64]string{})
	waitForCrawl(t, id, runs)

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	if v := formValue(got.Settings["crawl_freq"]); v != "77" {
		t.Errorf("crawl_freq = %q after the scheduled crawl, want the file's 77", v)
	}
}

// TestScheduledCrawlHonorsPauseInConfigFile tests a config file pausing the channel stops the crawl due.
func TestScheduledCrawlHonorsPauseInConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ScheduledPause")
	if err := os.WriteFile(cfg, []byte("channel-name: ScheduledPause\n"+keys.Pause+": true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	crawlConfigStore(t).crawlDueChannels(t.Context(), map[int64]string{})

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	if v := formValue(got.Settings["paused"]); v != "true" {
		t.Errorf("paused = %q, want the file's true", v)
	}
	active, _ := state.CrawlingChannels()
	if _, err := os.Stat(runs); err == nil || slices.Contains(active, id) {
		t.Error("the paused channel was crawled")
	}
}

// TestScheduledCrawlUnpausesFromConfigFile tests a config file can unpause a channel saved as paused.
func TestScheduledCrawlUnpausesFromConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ScheduledUnpause")
	if w := sendRequest(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/toggle-pause", id), "", ""); w.Code != http.StatusOK {
		t.Fatalf("pausing = %d %s", w.Code, w.Body.String())
	}
	if err := os.WriteFile(cfg, []byte("channel-name: ScheduledUnpause\n"+keys.Pause+": false\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	crawlConfigStore(t).crawlDueChannels(t.Context(), map[int64]string{})
	waitForCrawl(t, id, runs)
}

// TestCrawlButtonReadsConfigFile tests the Crawl button applies an edited config file.
func TestCrawlButtonReadsConfigFile(t *testing.T) {
	id, runs, cfg := crawlConfigChannel(t, "ButtonConfig")
	if err := os.WriteFile(cfg, []byte("channel-name: ButtonConfig\ncrawl-freq: 88\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if w := sendRequest(t, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/crawl", id), "", ""); w.Code != http.StatusAccepted {
		t.Fatalf("crawl = %d %s", w.Code, w.Body.String())
	}
	waitForCrawl(t, id, runs)

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	if v := formValue(got.Settings["crawl_freq"]); v != "88" {
		t.Errorf("crawl_freq = %q after the crawl, want the file's 88", v)
	}
}

// ******************************** Follow ops ****************************************************************************************

// TestEditAndRunFollowsChannelURLOps tests Edit & Run keeps the channel URL's ops with a video's, so reopening it fills
// in the ops files' lines fresh, keeping a line added for the video.
func TestEditAndRunFollowsChannelURLOps(t *testing.T) {
	setupFuzzServer(t)
	serve := func(method, path, contentType, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	form := url.Values{
		"name":                           {"Follow Ops Channel"},
		"urls":                           {"https://example.com/@followops"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
		jsonkeys.MetarrMetaOps:           {"artist:set:Cats"},
		jsonkeys.MetarrMetaOpsFile:       {write("meta.txt", "album:set:Old\n")},
		jsonkeys.MetarrFilenameOpsFile:   {write("filename.txt", "suffix: [Old]\n")},
	}
	if w := serve(http.MethodPost, "/api/v1/channels/add", "application/x-www-form-urlencoded", form.Encode()); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	var chanID, cuID int64
	if err := fuzzServer.db.QueryRow("SELECT c.id, u.id FROM channels c JOIN channel_urls u ON u.channel_id = c.id WHERE c.name = 'Follow Ops Channel' AND u.url = 'https://example.com/@followops'").Scan(&chanID, &cuID); err != nil {
		t.Fatal(err)
	}
	res, err := fuzzServer.db.Exec(`INSERT INTO videos (channel_id, channel_url_id, channel_url, finished, ignored, url, metarr_status)
		VALUES (?, ?, 'https://example.com/@followops', 1, 0, 'https://example.com/v/follow', ?)`, chanID, cuID, consts.MetarrStatusFailed)
	if err != nil {
		t.Fatal(err)
	}
	videoID, _ := res.LastInsertId()

	type overrideOps struct {
		Defaults models.OverrideLines  `json:"defaults"`
		Saved    *models.OverrideLines `json:"saved"`
	}
	open := func() overrideOps {
		t.Helper()
		w := serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videoID), "", "")
		var got overrideOps
		if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
			t.Fatalf("override ops: %d %s", w.Code, w.Body.String())
		}
		return got
	}

	// Edit & Run with the meta ops left as the channel URL's and a filename op added.
	first := open()
	body, err := json.Marshal(map[string]any{
		"video_ids": []int64{videoID},
		"overrides": models.OverrideLines{MetaOps: first.Defaults.MetaOps, FilenameOps: first.Defaults.FilenameOps + "\nprefix:[Mine] "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := serve(http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/run-metarr", chanID), "application/json", string(body)); w.Code != http.StatusAccepted {
		t.Fatalf("edit & run: %d %s", w.Code, w.Body.String())
	}
	o, err := repo.GetVideoStore(fuzzServer.db).GetVideoOverrides(videoID)
	if err != nil || o == nil || o.ChannelURLOps == nil || o.ChannelURLOps.MetaOps != first.Defaults.MetaOps {
		t.Fatalf("stored overrides %+v, %v, want the channel URL's ops kept with them", o, err)
	}

	// The ops files change.
	write("meta.txt", "album:set:New\n")
	write("filename.txt", "suffix: [New]\n")
	second := open()
	if !strings.Contains(second.Defaults.MetaOps, "album:set:New") {
		t.Fatalf("channel URL's meta ops %q, want the file's new op", second.Defaults.MetaOps)
	}
	if second.Saved == nil {
		t.Fatal("reopened with nothing saved")
	}
	if second.Saved.MetaOps != second.Defaults.MetaOps {
		t.Errorf("meta ops left as the channel URL's: got %q, want its ops now, %q", second.Saved.MetaOps, second.Defaults.MetaOps)
	}
	if want := "prefix:[Mine] \nsuffix: [New]"; second.Saved.FilenameOps != want {
		t.Errorf("filename ops with an added op: got %q, want %q, the added op kept and the file's suffix swapped", second.Saved.FilenameOps, want)
	}
}

// ******************************** Form wiring ***************************************************************************************

// The web pages name every field and request key in plain HTML and JavaScript, separately from the handlers. These
// tests fail when the two drift apart.
const (
	wiringURLA = "https://example.com/@wiring-a"
	wiringURLB = "https://example.com/@wiring-b"
)

// perURLFixedKeys are the per-URL fields the channel pages send whether or not custom settings are on.
var perURLFixedKeys = []string{"paused", "use_global_cookies", "auto_download"}

// perURLMetarrBools are the per-URL Metarr choices the channel pages send as booleans, with the custom settings.
var perURLMetarrBools = []string{"metarr_purge_original_video", "metarr_write_nfo", "metarr_auto_post_process"}

// replaceTicks are the Replace checkboxes under each list, which channels and URLs have but profiles don't.
var replaceTicks = []string{
	"filters_replace_inherited", "move_ops_replace_inherited",
	"metarr_meta_ops_replace_inherited", "metarr_filtered_meta_ops_replace_inherited",
	"metarr_filename_ops_replace_inherited", "metarr_filtered_filename_ops_replace_inherited",
}

// ---- Page parsing ----

// webRoot is the repository's web folder.
func webRoot() string {
	return filepath.Join(pkgDir, "..", "..", "web")
}

// readWebFile returns a file from the web folder.
func readWebFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(webRoot(), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// readBuiltPage returns a page as it's served, with its includes filled in.
func readBuiltPage(t *testing.T, name string) string {
	t.Helper()
	built, err := fillIncludes(webRoot(), []byte(readWebFile(t, name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(built)
}

// webPages returns every page and partial in the web folder, by path relative to it.
func webPages(t *testing.T) map[string]string {
	t.Helper()
	pages := map[string]string{}
	for _, pattern := range []string{"*.html", filepath.Join("partials", "*.html")} {
		matches, err := filepath.Glob(filepath.Join(webRoot(), pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			rel, err := filepath.Rel(webRoot(), m)
			if err != nil {
				t.Fatal(err)
			}
			pages[rel] = readWebFile(t, rel)
		}
	}
	return pages
}

// binding is an x-model binding: the model path it writes, and whether it writes a number.
type binding struct {
	path   string
	number bool
}

var xModelRe = regexp.MustCompile(`x-model((?:\.[a-z]+)*)="([\w.]+)"`)

// bindings returns a page's x-model bindings.
func bindings(src string) []binding {
	var out []binding
	for _, m := range xModelRe.FindAllStringSubmatch(src, -1) {
		out = append(out, binding{path: m[2], number: slices.Contains(strings.Split(m[1], "."), "number")})
	}
	return out
}

// boundKeys returns the keys bound directly under prefix, and whether each binds as a number.
func boundKeys(bs []binding, prefix string) map[string]bool {
	keys := map[string]bool{}
	for _, b := range bs {
		if k, ok := strings.CutPrefix(b.path, prefix); ok && !strings.Contains(k, ".") {
			keys[k] = keys[k] || b.number
		}
	}
	return keys
}

var objectKeyRe = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*:`)

// objectKeys returns the top-level keys of the JavaScript object literal opened by marker (ending in "{"), the first
// one after anchor. The models it reads hold one key per line.
func objectKeys(t *testing.T, src, anchor, marker string) []string {
	t.Helper()
	start := strings.Index(src, anchor)
	if start < 0 {
		t.Fatalf("page has no %q", anchor)
	}
	i := strings.Index(src[start:], marker)
	if i < 0 {
		t.Fatalf("page has no %q after %q", marker, anchor)
	}
	open := start + i + len(marker) - 1
	depth, end := 0, -1
	for j := open; j < len(src) && end < 0; j++ {
		switch src[j] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
		if depth == 0 {
			end = j
		}
	}
	if end < 0 {
		t.Fatalf("object %q after %q never closes", marker, anchor)
	}

	var keys []string
	depth = 0
	for _, line := range strings.Split(src[open+1:end], "\n") {
		if m := objectKeyRe.FindStringSubmatch(line); m != nil && depth == 0 {
			keys = append(keys, m[1])
		}
		depth += strings.Count(line, "{") + strings.Count(line, "[") - strings.Count(line, "}") - strings.Count(line, "]")
	}
	return keys
}

// arrayStrings returns the strings in the JavaScript array assigned to const name.
func arrayStrings(t *testing.T, src, name string) []string {
	t.Helper()
	m := regexp.MustCompile(`const ` + name + ` = \[([^\]]*)\]`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("page has no array %q", name)
	}
	var out []string
	for _, s := range regexp.MustCompile(`'(\w+)'`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, s[1])
	}
	return out
}

// perURLKeys returns a channel page's per-URL model keys, checking the add page's two copies agree.
func perURLKeys(t *testing.T, page, src string) []string {
	t.Helper()
	if page == "edit-channel.html" {
		return objectKeys(t, src, "getEmptyCustomSettings() {", "return {")
	}
	first := objectKeys(t, src, "formData: {", "custom: {")
	reportDiff(t, page, "first URL's per-URL model", first, "per-URL model in addURL()", objectKeys(t, src, "addURL() {", "custom: {"))
	return first
}

// jsonTags returns the JSON names of a struct's fields.
func jsonTags(v any) []string {
	var tags []string
	typ := reflect.TypeOf(v)
	for i := range typ.NumField() {
		if name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
			tags = append(tags, name)
		}
	}
	return tags
}

// fieldKinds returns the kind of each struct field by JSON name, looking through pointers.
func fieldKinds(vs ...any) map[string]reflect.Kind {
	kinds := map[string]reflect.Kind{}
	for _, v := range vs {
		typ := reflect.TypeOf(v)
		for i := range typ.NumField() {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			kinds[name] = ft.Kind()
		}
	}
	return kinds
}

// reportDiff reports keys found in only one of two lists.
func reportDiff(t *testing.T, page, aName string, a []string, bName string, b []string) {
	t.Helper()
	for _, k := range a {
		if !slices.Contains(b, k) {
			t.Errorf("%s: %q is in the %s but not the %s", page, k, aName, bName)
		}
	}
	for _, k := range b {
		if !slices.Contains(a, k) {
			t.Errorf("%s: %q is in the %s but not the %s", page, k, bName, aName)
		}
	}
}

// without returns list without key.
func without(list []string, key string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(k string) bool { return k == key })
}

// ---- Requests ----

// sendRequest sends a request to the shared test server.
func sendRequest(t *testing.T, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, req)
	return w
}

// sendForm sends a URL-encoded form, as the pages' URLSearchParams bodies are.
func sendForm(t *testing.T, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return sendRequest(t, method, target, "application/x-www-form-urlencoded", form.Encode())
}

// sendJSON sends a JSON body.
func sendJSON(t *testing.T, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return sendRequest(t, method, target, "application/json", string(data))
}

// fetchJSON decodes a JSON GET response into v, failing unless the API answered it.
func fetchJSON(t *testing.T, target string, v any) {
	t.Helper()
	w := sendRequest(t, http.MethodGet, target, "", "")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("GET %s = %d %q: %s", target, w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
}

// wiringFile writes a file under the test directory's wiring folder and returns its path.
func wiringFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(fuzzServer.dir, "wiring", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// deleteChannelNamed deletes a channel through the API.
func deleteChannelNamed(t *testing.T, name string) {
	t.Helper()
	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", name).Scan(&id); err != nil {
		t.Errorf("channel %q wasn't added: %v", name, err)
		return
	}
	if w := sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", ""); w.Code != http.StatusNoContent {
		t.Errorf("deleting channel %q = %d %s", name, w.Code, w.Body.String())
	}
}

// ---- Values ----

// normalizeLines drops blank lines and carriage returns, keeping trailing spaces (an operation's value may end in one).
func normalizeLines(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSuffix(line, "\r"); strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// formValue renders a value read back from the API as the page shows it in its form.
func formValue(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return normalizeLines(v)
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			parts = append(parts, formValue(p))
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprint(v)
}

// sameFormValue reports whether a value read back matches the one sent, where a zero value may be left out.
func sameFormValue(sent, got string) bool {
	sent = normalizeLines(sent)
	return sent == got || (got == "" && (sent == "false" || sent == "0"))
}

// sameSet reports whether two lists hold the same strings in any order.
func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// notificationsJSON renders notifications as the channel pages send them, each a channel URL (blank for all) and notify URL.
func notificationsJSON(rows ...[2]string) string {
	ns := make([]models.Notification, 0, len(rows))
	for _, r := range rows {
		ns = append(ns, models.Notification{ChannelURL: r[0], NotifyURL: r[1]})
	}
	data, _ := json.Marshal(ns)
	return string(data)
}

// notificationKeys renders notifications sent as JSON, or read back, comparably as "channel URL notify URL".
func notificationKeys[T string | []models.Notification](v T) []string {
	var ns []models.Notification
	switch v := any(v).(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &ns)
	case []models.Notification:
		ns = v
	}
	keys := make([]string, 0, len(ns))
	for _, n := range ns {
		keys = append(keys, n.ChannelURL+" "+n.NotifyURL)
	}
	return keys
}

// urlSettingValue types a per-URL value as the channel pages put it in their url_settings JSON.
func urlSettingValue(key, v string, number bool) any {
	switch {
	case slices.Contains(perURLFixedKeys, key) || slices.Contains(perURLMetarrBools, key) || slices.Contains(replaceTicks, key):
		return v == "true"
	case key == "ytdlp_preferred_video_codecs" || key == "ytdlp_preferred_audio_codecs":
		return strings.Split(v, "\n")
	case number:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return v
}

// formSamples holds a value for every channel page field, for adding a channel then editing it. Values are as the
// pages send them, chosen to pass validation and read back unchanged. Per-URL settings use them the other way round.
func formSamples(t *testing.T) map[string][2]string {
	t.Helper()
	sub := func(name string) string {
		dir := filepath.Join(fuzzServer.dir, "wiring", name)
		if err := os.MkdirAll(dir, 0o755); err != nil { // Folders must exist to be saved.
			t.Fatal(err)
		}
		return dir
	}
	file := func(name, content string) string { return wiringFile(t, name, content) }
	auth := func(user, pass, login string) string {
		data, err := json.Marshal([]map[string]string{{"channel_url": wiringURLA, "username": user, "password": pass, "login_url": login}})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return map[string][2]string{
		// Request fields outside the models.
		"name":         {"Wiring Add", "Wiring Edit"},
		"auth_details": {auth("user-a", "pass-a", "https://example.com/login"), auth("user-b", "pass-b", "https://example.com/login-b")},
		"notifications": {
			notificationsJSON([2]string{"", "http://plex.local:32400/library/sections/1/refresh?X-Plex-Token=abc"}, [2]string{wiringURLB, "http://notify.local/hook"}),
			notificationsJSON([2]string{"", "http://jellyfin.local/Library/Refresh"}, [2]string{"", "http://bare.local/hook"}),
		},

		// Settings.
		"video_directory":              {sub("videos-a"), sub("videos-b")},
		"json_directory":               {sub("json-a"), sub("json-b")},
		"download_concurrency":         {"2", "3"},
		"crawl_freq":                   {"45", "60"},
		"download_retries":             {"2", "4"},
		"max_filesize":                 {"100m", "2g"},
		"max_resolution":               {"1080", "720"},
		"from_date":                    {"20240101", "20240201"},
		"to_date":                      {"20241231", "20250131"},
		"filter_file":                  {file("filters-a.txt", "title:contains:cat:must\n"), file("filters-b.txt", "title:contains:dog:must\n")},
		"filters":                      {"title:contains:cat:must\ntitle:omits:dog:any", "description:contains:fish:must"},
		"move_ops_file":                {file("moves-a.txt", "title:contains:cats||/cats\n"), file("moves-b.txt", "title:contains:dogs||/dogs\n")},
		"move_ops":                     {"title:contains:cats: the | movie||" + sub("cats"), "title:omits:dogs&&duration:more-than:600||" + sub("dogs")},
		"ytdlp_output_ext":             {"mp4", "mkv"},
		"cookies_from_browser":         {"firefox", "chrome"},
		"external_downloader":          {"aria2c", "axel"},
		"external_downloader_args":     {"-x 4", "-n 2"},
		"extra_ytdlp_video_args":       {"--no-mtime", "--no-part"},
		"extra_ytdlp_meta_args":        {"--no-mtime", "--no-part"},
		"extra_ytdlp_crawl_args":       {"--playlist-end 50", "--playlist-end 20"},
		"use_global_cookies":           {"true", "false"},
		"paused":                       {"true", "false"},
		"auto_download":                {"false", "true"},
		"channel_config_file":          {file("config-a.yaml", "channel-name: Wiring\n"), file("config-b.yaml", "channel-name: Wiring\n")},
		"ytdlp_preferred_video_codecs": {"av1\nvp9", "h264"},
		"ytdlp_preferred_audio_codecs": {"opus", "aac\nopus"},
		"filters_replace_inherited":    {"true", "false"},
		"move_ops_replace_inherited":   {"false", "true"},

		// Metarr.
		"metarr_output_directory":                 {sub("out-a") + "/{{year}}", sub("out-b")},
		"metarr_output_ext":                       {"mp4", "mkv"},
		"metarr_rename_style":                     {"spaces", "underscores"},
		"metarr_concurrency":                      {"2", "3"},
		"metarr_max_cpu_usage":                    {"50", "75"},
		"metarr_min_free_mem":                     {"1G", "500M"},
		"metarr_gpu":                              {"auto", "auto"},
		"metarr_transcode_gpu_node":               {file("render-a", ""), file("render-b", "")},
		"metarr_transcode_video_transcode_codecs": {"h264:hevc", "vp9:h264"},
		"metarr_transcode_audio_codecs":           {"aac:opus", "flac:mp3"},
		"metarr_transcode_video_args":             {"hevc:-crf 23", "av1:-crf 35 -preset 6\nvp9:-crf 31"},
		"metarr_extra_ffmpeg_args":                {"-threads 2", "-threads 4"},
		"metarr_filename_ops":                     {"prefix:[CAT] ", "suffix: [DOG]"},
		"metarr_filename_ops_file":                {file("filename-ops-a.txt", "prefix:[A] \n"), file("filename-ops-b.txt", "prefix:[B] \n")},
		"metarr_filtered_filename_ops":            {"title:contains:cat||prefix:[CAT] ", "title:contains:dog&&duration:more-than:600||suffix: [DOG]"},
		"metarr_filtered_filename_ops_file":       {file("filtered-filename-ops-a.txt", "title:contains:a||prefix:[A] \n"), file("filtered-filename-ops-b.txt", "title:contains:b||prefix:[B] \n")},
		"metarr_meta_ops":                         {"title:set:Cat Title", "artist:set:Dog Band"},
		"metarr_meta_ops_file":                    {file("meta-ops-a.txt", "title:set:A\n"), file("meta-ops-b.txt", "title:set:B\n")},
		"metarr_filtered_meta_ops":                {"title:contains:ep 5 | cat: finale||artist:set:Cat", "title:contains:dog&&duration:more-than:600||artist:set:Dog"},
		"metarr_filtered_meta_ops_file":           {file("filtered-meta-ops-a.txt", "title:contains:a||artist:set:A\n"), file("filtered-meta-ops-b.txt", "title:contains:b||artist:set:B\n")},
		"metarr_purge_metafile":                   {"json", "all"},
		"metarr_purge_original_video":             {"false", "true"},
		"metarr_write_nfo":                        {"true", "false"},
		"metarr_auto_post_process":                {"false", "true"},

		// Replace checkboxes.
		"metarr_meta_ops_replace_inherited":              {"true", "false"},
		"metarr_filtered_meta_ops_replace_inherited":     {"false", "true"},
		"metarr_filename_ops_replace_inherited":          {"true", "false"},
		"metarr_filtered_filename_ops_replace_inherited": {"false", "true"},
	}
}

// ---- Channel forms ----

// TestChannelFormModels tests that the add and edit pages bind, hold and send the field names the server reads and
// returns.
func TestChannelFormModels(t *testing.T) {
	settingsTags := jsonTags(settingsResponse{})
	metarrTags := jsonTags(metarrArgsResponse{})
	urlTags := slices.Concat(jsonTags(settingsInput{}), jsonTags(metarrArgsInput{}))
	urlKinds := fieldKinds(settingsInput{}, metarrArgsInput{})
	if !slices.Contains(jsonTags(channelDetailResponse{}), "channel_config_file") {
		t.Error("channel details don't include channel_config_file, so the edit page can't show it")
	}

	for _, page := range []string{"add-channel.html", "edit-channel.html"} {
		src := readBuiltPage(t, page)
		bs := bindings(src)
		topKeys := objectKeys(t, src, "channelForm()", "formData: {")
		settingsKeys := objectKeys(t, src, "formData: {", "settings: {")
		metarrKeys := objectKeys(t, src, "formData: {", "metarr: {")
		urlKeys := perURLKeys(t, page, src)
		urlFields := slices.Concat(arrayStrings(t, src, "settingsFields"), arrayStrings(t, src, "metarrFields"), perURLFixedKeys, perURLMetarrBools)

		for _, model := range []struct {
			prefix string
			keys   []string
		}{
			{"formData.", topKeys},
			{"formData.settings.", settingsKeys},
			{"formData.metarr.", metarrKeys},
			{"urlEntry.custom.", urlKeys},
		} {
			bound := boundKeys(bs, model.prefix)
			for k := range bound {
				if !slices.Contains(model.keys, k) {
					t.Errorf("%s: a field is bound to %s%s, which isn't in that model, so it never loads or sends", page, model.prefix, k)
				}
			}
			if model.prefix == "formData." {
				continue // Its settings, metarr and urls entries hold the other fields.
			}
			for _, k := range model.keys {
				if _, ok := bound[k]; !ok {
					t.Errorf("%s: model key %s%s has no field", page, model.prefix, k)
				}
			}
		}
		reportDiff(t, page, "per-URL field lists", urlFields, "per-URL model", urlKeys)
		reportDiff(t, page, "settings model", without(settingsKeys, "channel_config_file"), "settings the server returns", settingsTags)
		reportDiff(t, page, "Metarr model", metarrKeys, "Metarr settings the server returns", metarrTags)
		reportDiff(t, page, "per-URL model", urlKeys, "per-URL settings the server reads", urlTags)

		// Per-URL settings are sent as JSON, so a field binding a number must reach a number field, and others not.
		for k, number := range boundKeys(bs, "urlEntry.custom.") {
			kind, ok := urlKinds[k]
			if !ok {
				continue // Reported above.
			}
			if numeric := kind == reflect.Int || kind == reflect.Float64; number != numeric {
				t.Errorf("%s: per-URL field %q binds as a number: %t, but the server reads it as %s", page, k, number, kind)
			}
		}

		// Notifications are sent as JSON.
		if !regexp.MustCompile(`payload\.notifications = notificationsJSON\(`).MatchString(src) {
			t.Errorf("%s: notifications aren't sent as JSON", page)
		}

		// Profiles are chosen by ID, the channel's sent as a form field and each URL's in its url_settings entry.
		for _, path := range []string{"formData.profile_id", "urlEntry.profile_id"} {
			if !slices.ContainsFunc(bs, func(b binding) bool { return b.path == path }) {
				t.Errorf("%s: no field is bound to %s", page, path)
			}
		}
		if !regexp.MustCompile(`payload\.profile_id = this\.formData\.profile_id`).MatchString(src) {
			t.Errorf("%s: the channel's profile isn't sent", page)
		}
		if !regexp.MustCompile(`\.profile_id = Number\(urlEntry\.profile_id\)`).MatchString(src) {
			t.Errorf("%s: URL profiles aren't sent", page)
		}
	}
}

// TestChannelListPagesRead tests the channel list pages only read fields the channel list returns.
func TestChannelListPagesRead(t *testing.T) {
	returned := map[string][]string{
		"":         slices.Concat(jsonTags(channelResponse{}), []string{"expanded", "videos"}), // The home page sets these.
		"settings": jsonTags(settingsResponse{}),
		"metarr":   jsonTags(metarrArgsResponse{}),
	}
	read := regexp.MustCompile(`(?:^|[^\w-])channel\.(?:(settings|metarr)\??\.)?([a-z_]+)`)
	for _, page := range []string{"list-channels.html", "index.html"} {
		for _, m := range read.FindAllStringSubmatch(readWebFile(t, page), -1) {
			if !slices.Contains(returned[m[1]], m[2]) {
				t.Errorf("%s reads channel.%s, which the channel list doesn't return", page, strings.TrimPrefix(m[1]+"."+m[2], "."))
			}
		}
	}
}

// channelDetails is a channel as the edit page loads it.
type channelDetails struct {
	Name              string                       `json:"name"`
	ProfileID         int64                        `json:"profile_id"`
	URLs              []string                     `json:"urls"`
	ChannelConfigFile string                       `json:"channel_config_file"`
	Settings          map[string]any               `json:"settings"`
	Metarr            map[string]any               `json:"metarr"`
	AuthDetails       []authDetailResponse         `json:"auth_details"`
	URLSettings       map[string]urlSettingsDetail `json:"url_settings"`
	Notifications     []models.Notification        `json:"notifications"`
}

// urlSettingsDetail is a channel URL's own settings and profile, as the edit page loads them.
type urlSettingsDetail struct {
	ProfileID int64          `json:"profile_id"`
	Settings  map[string]any `json:"settings"`
	Metarr    map[string]any `json:"metarr"`
}

// TestChannelFormRoundTrip adds a channel the way the add page does, then edits it the way the edit page does, with
// every field filled, and checks each value reads back.
func TestChannelFormRoundTrip(t *testing.T) {
	setupFuzzServer(t)
	samples := formSamples(t)
	used := map[string]bool{}

	// Profiles for the channel and URL B to choose, deleted after the channel.
	profileIDs := make([]int64, 2)
	for i, name := range []string{"Wiring Round Trip A", "Wiring Round Trip B"} {
		if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", url.Values{"name": {name}}); w.Code != http.StatusCreated {
			t.Fatalf("adding profile %q = %d %s", name, w.Code, w.Body.String())
		}
		if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", name).Scan(&profileIDs[i]); err != nil {
			t.Fatal(err)
		}
		profileID := profileIDs[i]
		t.Cleanup(func() {
			sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
		})
	}

	var id int64
	for step, page := range []string{"add-channel.html", "edit-channel.html"} {
		src := readBuiltPage(t, page)
		settingsKeys := objectKeys(t, src, "formData: {", "settings: {")
		metarrKeys := objectKeys(t, src, "formData: {", "metarr: {")
		urlSettingsFields := slices.Concat(arrayStrings(t, src, "settingsFields"), perURLFixedKeys)
		urlMetarrFields := slices.Concat(arrayStrings(t, src, "metarrFields"), perURLMetarrBools)
		urlNumbers := boundKeys(bindings(src), "urlEntry.custom.")

		value := func(key string, perURL bool) string {
			s, ok := samples[key]
			if !ok {
				t.Fatalf("%s: field %q has no value in formSamples: add one so its wiring is tested", page, key)
			}
			used[key] = true
			if perURL {
				return s[1-step]
			}
			return s[step]
		}

		// Built as submitForm builds it.
		form := url.Values{}
		for _, k := range slices.Concat(settingsKeys, metarrKeys) {
			switch v := value(k, false); {
			case k == "paused":
				if v == "true" {
					form.Set(k, v)
				}
			case v != "":
				form.Set(k, v)
			}
		}
		form.Set("profile_id", strconv.FormatInt(profileIDs[step], 10))
		urlSettings, urlMetarr := map[string]any{}, map[string]any{}
		for _, k := range urlSettingsFields {
			urlSettings[k] = urlSettingValue(k, value(k, true), urlNumbers[k])
		}
		for _, k := range urlMetarrFields {
			urlMetarr[k] = urlSettingValue(k, value(k, true), urlNumbers[k])
		}
		perURL, err := json.Marshal(map[string]any{wiringURLB: map[string]any{"profile_id": profileIDs[1-step], "settings": urlSettings, "metarr": urlMetarr}})
		if err != nil {
			t.Fatal(err)
		}
		form.Set("url_settings", string(perURL))
		form.Set("name", value("name", false))
		form.Set("urls", wiringURLA+" "+wiringURLB)
		form.Set("auth_details", value("auth_details", false))
		form.Set("notifications", value("notifications", false))

		var w *httptest.ResponseRecorder
		if step == 0 {
			w = sendForm(t, http.MethodPost, "/api/v1/channels/add", form)
		} else {
			w = sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form)
		}
		if w.Code >= 300 {
			t.Fatalf("%s: saving = %d %s", page, w.Code, w.Body.String())
		}
		if step == 0 {
			if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", value("name", false)).Scan(&id); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", "") })
		}

		var got channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
		check := func(what, key, sent string, read any) {
			t.Helper()
			if g := formValue(read); !sameFormValue(sent, g) {
				t.Errorf("%s: %s %q was sent as %q but reads back as %q", page, what, key, sent, g)
			}
		}
		for _, k := range settingsKeys {
			if k == "channel_config_file" {
				check("channel field", k, value(k, false), got.ChannelConfigFile)
				continue
			}
			check("setting", k, value(k, false), got.Settings[k])
		}
		for _, k := range metarrKeys {
			check("Metarr setting", k, value(k, false), got.Metarr[k])
		}
		for _, k := range urlSettingsFields {
			check("per-URL setting", k, value(k, true), got.URLSettings[wiringURLB].Settings[k])
		}
		for _, k := range urlMetarrFields {
			check("per-URL Metarr setting", k, value(k, true), got.URLSettings[wiringURLB].Metarr[k])
		}
		check("channel field", "name", value("name", false), got.Name)
		if got.ProfileID != profileIDs[step] {
			t.Errorf("%s: the channel's profile was sent as %d but reads back as %d", page, profileIDs[step], got.ProfileID)
		}
		if p := got.URLSettings[wiringURLB].ProfileID; p != profileIDs[1-step] {
			t.Errorf("%s: URL B's profile was sent as %d but reads back as %d", page, profileIDs[1-step], p)
		}
		if !sameSet(got.URLs, []string{wiringURLA, wiringURLB}) {
			t.Errorf("%s: URLs read back as %v", page, got.URLs)
		}
		if want := notificationKeys(value("notifications", false)); !sameSet(notificationKeys(got.Notifications), want) {
			t.Errorf("%s: notifications were sent as %q but read back as %+v", page, want, got.Notifications)
		}
		var sentAuth []map[string]string
		if err := json.Unmarshal([]byte(value("auth_details", false)), &sentAuth); err != nil {
			t.Fatal(err)
		}
		if len(got.AuthDetails) != 1 || got.AuthDetails[0].ChannelURL != wiringURLA || got.AuthDetails[0].Username != sentAuth[0]["username"] ||
			got.AuthDetails[0].LoginURL != sentAuth[0]["login_url"] || !got.AuthDetails[0].HasPassword {
			t.Errorf("%s: login was sent as %v but reads back as %+v", page, sentAuth, got.AuthDetails)
		}
	}
	for key := range samples {
		if !used[key] {
			t.Errorf("formSamples has %q, which no channel page field uses", key)
		}
	}
}

// profileDetails is a profile as the profiles page lists it.
type profileDetails struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Settings map[string]any `json:"settings"`
	Metarr   map[string]any `json:"metarr"`
}

// TestProfileFormRoundTrip adds a profile the way the profiles page does, then edits it, with every field filled, and
// checks each value reads back.
func TestProfileFormRoundTrip(t *testing.T) {
	setupFuzzServer(t)
	samples := formSamples(t)
	src := readBuiltPage(t, "profiles.html")
	settingsKeys := objectKeys(t, src, "function emptyProfileForm()", "settings: {")
	metarrKeys := objectKeys(t, src, "function emptyProfileForm()", "metarr: {")

	// Every model key has a field, and every field a model key, other than the channel-only ones the page hides.
	bs := bindings(src)
	for _, model := range []struct {
		prefix string
		keys   []string
		hidden []string
	}{
		{"formData.settings.", settingsKeys, slices.Concat([]string{"paused", "channel_config_file"}, replaceTicks)},
		{"formData.metarr.", metarrKeys, replaceTicks},
	} {
		bound := boundKeys(bs, model.prefix)
		for k := range bound {
			if !slices.Contains(model.keys, k) && !slices.Contains(model.hidden, k) {
				t.Errorf("profiles.html: a field is bound to %s%s, which isn't in that model, so it never loads or sends", model.prefix, k)
			}
		}
		for _, k := range model.keys {
			if _, ok := bound[k]; !ok {
				t.Errorf("profiles.html: model key %s%s has no field", model.prefix, k)
			}
		}
	}
	channelOnly := func(k string) bool { return k == "paused" || slices.Contains(replaceTicks, k) }
	reportDiff(t, "profiles.html", "settings model", settingsKeys, "settings the server returns", slices.DeleteFunc(jsonTags(settingsResponse{}), channelOnly))
	reportDiff(t, "profiles.html", "Metarr model", metarrKeys, "Metarr settings the server returns", slices.DeleteFunc(jsonTags(metarrArgsResponse{}), channelOnly))

	var id int64
	for step, name := range []string{"Wiring Profile A", "Wiring Profile B"} {
		value := func(key string) string {
			s, ok := samples[key]
			if !ok {
				t.Fatalf("profiles.html: field %q has no value in formSamples: add one so its wiring is tested", key)
			}
			return s[step]
		}

		// Built as save builds it.
		form := url.Values{"name": {name}}
		for _, k := range slices.Concat(settingsKeys, metarrKeys) {
			if v := value(k); v != "" {
				form.Set(k, v)
			}
		}
		var w *httptest.ResponseRecorder
		if step == 0 {
			w = sendForm(t, http.MethodPost, "/api/v1/profiles/add", form)
		} else {
			w = sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/profiles/%d/update", id), form)
		}
		if w.Code >= 300 {
			t.Fatalf("%s: saving = %d %s", name, w.Code, w.Body.String())
		}
		if step == 0 {
			if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", name).Scan(&id); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", id), "", "") })
		}

		var profiles []profileDetails
		fetchJSON(t, "/api/v1/profiles/all", &profiles)
		i := slices.IndexFunc(profiles, func(p profileDetails) bool { return p.ID == id })
		if i < 0 {
			t.Fatalf("%s: saved profile %d isn't listed", name, id)
		}
		got := profiles[i]
		if got.Name != name {
			t.Errorf("profile name was sent as %q but reads back as %q", name, got.Name)
		}
		for _, k := range settingsKeys {
			if g := formValue(got.Settings[k]); !sameFormValue(value(k), g) {
				t.Errorf("%s: setting %q was sent as %q but reads back as %q", name, k, value(k), g)
			}
		}
		for _, k := range metarrKeys {
			if g := formValue(got.Metarr[k]); !sameFormValue(value(k), g) {
				t.Errorf("%s: Metarr setting %q was sent as %q but reads back as %q", name, k, value(k), g)
			}
		}
	}
}

// TestEditChangesURLPassword tests that a new password entered on the edit page replaces the saved one.
func TestEditChangesURLPassword(t *testing.T) {
	setupFuzzServer(t)
	base := url.Values{
		"name":                 {"Password Change"},
		"urls":                 {"https://example.com/@password"},
		"video_directory":      {filepath.Join(fuzzServer.dir, "videos")},
		"download_concurrency": {"1"}, "crawl_freq": {"30"}, "download_retries": {"1"},
	}
	authFor := func(pass string) string {
		return `[{"channel_url":"https://example.com/@password","username":"user","password":"` + pass + `","login_url":"https://example.com/login"}]`
	}
	form := base
	form.Set("auth_details", authFor("old-pass"))
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code != http.StatusCreated {
		t.Fatalf("adding = %d %s", w.Code, w.Body.String())
	}
	defer deleteChannelNamed(t, "Password Change")

	cs, err := repo.GetChannelStore(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	saved := func() string {
		c, _, err := cs.GetChannelModel(consts.QChanName, "Password Change", false)
		if err != nil {
			t.Fatal(err)
		}
		return c.URLModels[0].Login.Password
	}
	if got := saved(); got != "old-pass" {
		t.Fatalf("saved password = %q, want old-pass", got)
	}

	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", "Password Change").Scan(&id); err != nil {
		t.Fatal(err)
	}
	edit := func(pass string) {
		form.Set("auth_details", authFor(pass))
		if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusOK {
			t.Fatalf("editing = %d %s", w.Code, w.Body.String())
		}
	}
	edit("new-pass")
	if got := saved(); got != "new-pass" {
		t.Errorf("after entering a new password, the saved one is %q", got)
	}
	edit("") // Passwords aren't sent to the edit page, so a blank one keeps the saved one.
	if got := saved(); got != "new-pass" {
		t.Errorf("after saving with a blank password, the saved one is %q", got)
	}
}

// ---- Other page requests ----

// pageRequest is a request a page makes, other than the channel forms' add and edit.
type pageRequest struct {
	path   string             // As written in the page, with ${...} parts as {}.
	keys   []string           // Body or query keys the page sends, which must appear near the path in a page using it.
	replay func(t *testing.T) // Sends the request as the page does and checks the server used it. Nil when another test covers it.
}

var (
	apiPathRe      = regexp.MustCompile("[`'\"](/api/v1/[^`'\"]*)[`'\"]")
	templatePartRe = regexp.MustCompile(`\$\{[^}]*\}`)
)

// keysNearPath reports whether some quoted occurrence of path in src has every key nearby, as a whole word.
func keysNearPath(src, path string, keys []string) bool {
	for _, loc := range regexp.MustCompile("[`'\"]"+regexp.QuoteMeta(path)+"[`'\"]").FindAllStringIndex(src, -1) {
		window := src[max(0, loc[0]-1200):min(len(src), loc[1]+800)]
		if !slices.ContainsFunc(keys, func(k string) bool {
			return !regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(k) + `([^\w-]|$)`).MatchString(window)
		}) {
			return true
		}
	}
	return false
}

// routed returns a replay checking the API itself answers a request with want (0 for any status below 500), rather
// than it falling through to the page server.
func routed(method, path string, want int) func(t *testing.T) {
	return func(t *testing.T) {
		w := sendRequest(t, method, path, "", "")
		if strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || (want != 0 && w.Code != want) || w.Code >= 500 {
			t.Errorf("%s %s = %d %q %s, want %d from the API", method, path, w.Code, w.Header().Get("Content-Type"), w.Body.String(), want)
		}
	}
}

// readsJSON returns a replay checking a GET answers with JSON.
func readsJSON(path string) func(t *testing.T) {
	return func(t *testing.T) {
		var v any
		fetchJSON(t, path, &v)
	}
}

// strictJSON returns a replay sending body, then checking an unknown key added to it is refused.
func strictJSON(method, path, body string, want int) func(t *testing.T) {
	return func(t *testing.T) {
		if w := sendRequest(t, method, path, "application/json", body); w.Code != want {
			t.Errorf("%s %s with %s = %d %s, want %d", method, path, body, w.Code, w.Body.String(), want)
		}
		checkUnknownKeyRefused(t, method, path, body)
	}
}

// checkUnknownKeyRefused checks a JSON body with an unknown key added is refused, naming the key, so a renamed key
// can't be silently dropped.
func checkUnknownKeyRefused(t *testing.T, method, path, body string) {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	fields["unknown_key"] = 1
	if w := sendJSON(t, method, path, fields); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown_key") {
		t.Errorf("%s %s with an unknown key = %d %s, want 400 naming it", method, path, w.Code, w.Body.String())
	}
}

// limitSetting returns a replay setting a concurrency limit as the pages do, reading it back, then restoring it.
func limitSetting(path string) func(t *testing.T) {
	return func(t *testing.T) {
		var before, after struct {
			Limit int `json:"limit"`
		}
		fetchJSON(t, path, &before)
		if w := sendForm(t, http.MethodPut, path, url.Values{"limit": {"7"}}); w.Code != http.StatusOK {
			t.Fatalf("setting %s = %d %s", path, w.Code, w.Body.String())
		}
		fetchJSON(t, path, &after)
		if after.Limit != 7 {
			t.Errorf("%s set to 7 reads back as %d", path, after.Limit)
		}
		sendForm(t, http.MethodPut, path, url.Values{"limit": {strconv.Itoa(before.Limit)}})
	}
}

// fuzzChannelURLID returns the ID of the shared test channel's URL.
func fuzzChannelURLID(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = 1 AND url = 'https://example.com/@fuzz'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// setScrapeConfig sets the scrape config file as the Site Rules page does, cleared when the test ends.
func setScrapeConfig(t *testing.T) string {
	t.Helper()
	path := wiringFile(t, "scrape-rules.toml", "[[sites]]\ndomain = \"wiring.example\"\n\n  [[sites.selectors]]\n  field = \"title\"\n  selector = \"h1\"\n")
	if w := sendForm(t, http.MethodPut, "/api/v1/settings/scrape-config-file", url.Values{"path": {path}}); w.Code != http.StatusOK {
		t.Fatalf("setting the scrape config file = %d %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { sendForm(t, http.MethodPut, "/api/v1/settings/scrape-config-file", url.Values{"path": {""}}) })
	return path
}

// pageRequests lists every request the pages make, with how to replay it.
func pageRequests() []pageRequest {
	const (
		get, post, put, del = http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete
	)
	return []pageRequest{
		// Login requests, replayed against the real login manager in serverauth.
		{path: "/api/v1/auth/login", keys: []string{"username", "password"}},
		{path: "/api/v1/auth/logout"},
		{path: "/api/v1/auth/setup", keys: []string{"username", "password"}},
		{path: "/api/v1/auth/status"},
		{path: "/api/v1/settings/auth", keys: []string{"auth_mode", "skip_login", "allowed_hosts", "trusted_proxies"}},
		{path: "/api/v1/settings/auth/api-key"},
		{path: "/api/v1/settings/auth/password", keys: []string{"current_password", "username", "new_password"}},

		// Channel forms, replayed by TestChannelFormRoundTrip.
		{path: "/api/v1/channels/add"},
		{path: "/api/v1/channels/{}/update"},

		// Profile form, replayed by TestProfileFormRoundTrip.
		{path: "/api/v1/profiles/add", keys: []string{"name"}},
		{path: "/api/v1/profiles/{}/update", keys: []string{"name"}},
		{path: "/api/v1/profiles/all", replay: readsJSON("/api/v1/profiles/all")},
		{path: "/api/v1/profiles/{}/delete", replay: func(t *testing.T) {
			if w := sendForm(t, post, "/api/v1/profiles/add", url.Values{"name": {"Wiring Delete"}}); w.Code != http.StatusCreated {
				t.Fatalf("adding a profile = %d %s", w.Code, w.Body.String())
			}
			var profileID int64
			if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", "Wiring Delete").Scan(&profileID); err != nil {
				t.Fatal(err)
			}
			if w := sendRequest(t, del, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", ""); w.Code != http.StatusNoContent {
				t.Fatalf("deleting a profile = %d %s", w.Code, w.Body.String())
			}
			var count int
			if err := fuzzServer.db.QueryRow("SELECT COUNT(*) FROM profiles WHERE id = ?", profileID).Scan(&count); err != nil || count != 0 {
				t.Errorf("deleted profile still saved (count %d, %v)", count, err)
			}
		}},

		{path: "/api/v1/blocked-domains", replay: readsJSON("/api/v1/blocked-domains")},
		{path: "/api/v1/blocked-domains/{}?context={}", keys: []string{"context"}, replay: func(t *testing.T) {
			if err := blocking.BlockDomain(fuzzServer.db, "wiring.example", vars.BlockContextCookie); err != nil {
				t.Fatal(err)
			}
			if w := sendRequest(t, del, "/api/v1/blocked-domains/wiring.example?context=cookie", "", ""); w.Code != http.StatusOK {
				t.Fatalf("unblocking = %d %s", w.Code, w.Body.String())
			}
			if blocked, _, _ := blocking.IsBlocked("wiring.example", vars.BlockContextCookie); blocked {
				t.Error("domain is still blocked")
			}
		}},
		{path: "/api/v1/blocked-domains/{}", replay: func(t *testing.T) {
			for _, blockCtx := range []vars.BlockContext{vars.BlockContextCookie, vars.BlockContextAuth} {
				if err := blocking.BlockDomain(fuzzServer.db, "wiring.example", blockCtx); err != nil {
					t.Fatal(err)
				}
			}
			if w := sendRequest(t, del, "/api/v1/blocked-domains/wiring.example", "", ""); w.Code != http.StatusOK {
				t.Fatalf("unblocking all contexts = %d %s", w.Code, w.Body.String())
			}
			if _, ok := blocking.GetAllBlockedDomains()["wiring.example"]; ok {
				t.Error("domain is still blocked")
			}
		}},
		{path: "/api/v1/channels/{}/{}", keys: []string{"stop-queue", "abort"}, replay: func(t *testing.T) {
			routed(post, "/api/v1/channels/1/stop-queue", http.StatusConflict)(t) // Routed, with nothing running.
			routed(post, "/api/v1/channels/1/abort", http.StatusConflict)(t)
		}},
		{path: "/api/v1/channels/{}", replay: readsJSON("/api/v1/channels/1")},
		{path: "/api/v1/channels/activity", replay: readsJSON("/api/v1/channels/activity")},
		{path: "/api/v1/channels/add-from-directory", keys: []string{"add_from_config_dir"}, replay: func(t *testing.T) {
			path := wiringFile(t, filepath.Join("config-dir", "channel.yaml"), "channel-name: Wiring From Folder\nchannel-urls:\n  - https://example.com/@wiring-folder\nvideo-directory: "+filepath.Join(fuzzServer.dir, "videos")+"\n")
			if w := sendForm(t, post, "/api/v1/channels/add-from-directory", url.Values{"add_from_config_dir": {filepath.Dir(path)}}); w.Code != http.StatusCreated {
				t.Fatalf("adding from a folder = %d %s", w.Code, w.Body.String())
			}
			deleteChannelNamed(t, "Wiring From Folder")
		}},
		{path: "/api/v1/channels/add-from-file", keys: []string{"add_from_config_file"}, replay: func(t *testing.T) {
			path := wiringFile(t, "channel.yaml", "channel-name: Wiring From File\nchannel-urls:\n  - https://example.com/@wiring-file\nvideo-directory: "+filepath.Join(fuzzServer.dir, "videos")+"\n")
			if w := sendForm(t, post, "/api/v1/channels/add-from-file", url.Values{"add_from_config_file": {path}}); w.Code != http.StatusCreated {
				t.Fatalf("adding from a file = %d %s", w.Code, w.Body.String())
			}
			deleteChannelNamed(t, "Wiring From File")
		}},
		{path: "/api/v1/channels/all", replay: readsJSON("/api/v1/channels/all")},
		{path: "/api/v1/channels/{}/all-videos", replay: readsJSON("/api/v1/channels/1/all-videos")},
		{path: "/api/v1/channels/{}/override-ops/{}?video={}", keys: []string{"video"}, replay: func(t *testing.T) {
			readsJSON(fmt.Sprintf("/api/v1/channels/1/override-ops/%d", fuzzChannelURLID(t)))(t)
		}},
		{path: "/api/v1/videos/{}/thumbnail", replay: routed(post, "/api/v1/videos/999999/thumbnail", http.StatusNotFound)}, // Uploads: TestThumbnailUpload.
		{path: "/api/v1/channels/{}/override-ops/{}/file?{}", keys: []string{"box", "path"}, replay: func(t *testing.T) {
			path := wiringFile(t, "meta-ops.txt", "album:set:Wiring\n")
			readsJSON(fmt.Sprintf("/api/v1/channels/1/override-ops/%d/file?box=meta_ops&path=%s", fuzzChannelURLID(t), url.QueryEscape(path)))(t)
		}},
		{path: "/api/v1/channels/{}/available", keys: []string{"action", "video_ids", "overrides", "metarr", "auth"}, replay: func(t *testing.T) {
			vs := repo.GetVideoStore(fuzzServer.db)
			const videoURL = "https://example.com/v/wiring-available"
			if _, err := vs.AddAvailableVideos([]*models.Video{{ChannelURLID: fuzzChannelURLID(t), ChannelURL: "https://example.com/@fuzz", URL: videoURL, Metadata: models.VideoMetadata{Title: "Wiring"}}}, 1); err != nil {
				t.Fatal(err)
			}
			var videoID int64
			if err := fuzzServer.db.QueryRow("SELECT id FROM videos WHERE url = ?", videoURL).Scan(&videoID); err != nil {
				t.Fatal(err)
			}
			strictJSON(post, "/api/v1/channels/1/available", fmt.Sprintf(`{"action":"dismiss","video_ids":[%d]}`, videoID), http.StatusAccepted)(t)
			var status string
			if err := fuzzServer.db.QueryRow("SELECT status FROM downloads WHERE video_id = ?", videoID).Scan(&status); err != nil || status != string(consts.DLStatusIgnored) {
				t.Errorf("dismissed video's status = %q (%v), want %q", status, err, consts.DLStatusIgnored)
			}
		}},
		{path: "/api/v1/channels/{}/crawl", keys: []string{"url_ids"}, replay: routed(post, "/api/v1/channels/999999/crawl", http.StatusNotFound)},
		{path: "/api/v1/channels/{}/delete", replay: routed(del, "/api/v1/channels/999999/delete", http.StatusNotFound)},
		{path: "/api/v1/channels/{}/delete-videos", keys: []string{"urls[]", "delete_files"}, replay: func(t *testing.T) {
			vs := repo.GetVideoStore(fuzzServer.db)
			cuID := fuzzChannelURLID(t)
			addFinished := func(videoURL string) string {
				path := wiringFile(t, filepath.Base(videoURL)+".mp4", "video")
				v := &models.Video{ChannelURLID: cuID, ChannelURL: "https://example.com/@fuzz", URL: videoURL, Metadata: models.VideoMetadata{Title: "Wiring"}, Files: models.VideoFiles{VideoFilePath: path}}
				v.MarkVideoAsComplete()
				if _, err := vs.AddVideo(v, 1, cuID); err != nil {
					t.Fatal(err)
				}
				if err := vs.UpdateVideo(v, 1); err != nil {
					t.Fatal(err)
				}
				return path
			}
			deleteVideo := func(videoURL string, deleteFiles bool) {
				form := url.Values{"urls[]": {videoURL}}
				if deleteFiles {
					form.Set("delete_files", "true")
				}
				if w := sendForm(t, del, "/api/v1/channels/1/delete-videos", form); w.Code != http.StatusOK {
					t.Fatalf("deleting %s = %d %s", videoURL, w.Code, w.Body.String())
				}
			}
			kept := addFinished("https://example.com/v/wiring-keep")
			keptNFO := wiringFile(t, "wiring-keep.nfo", "nfo")
			deleteVideo("https://example.com/v/wiring-keep", false)
			if _, err := os.Stat(kept); err != nil {
				t.Errorf("deleting a video without delete_files removed its file: %v", err)
			}
			if _, err := os.Stat(keptNFO); err != nil {
				t.Errorf("deleting a video without delete_files removed its NFO file: %v", err)
			}
			removed := addFinished("https://example.com/v/wiring-remove")
			removedNFO := wiringFile(t, "wiring-remove.nfo", "nfo")
			deleteVideo("https://example.com/v/wiring-remove", true)
			if _, err := os.Stat(removed); !os.IsNotExist(err) {
				t.Errorf("deleting a video with delete_files kept its file (stat error: %v)", err)
			}
			if _, err := os.Stat(removedNFO); !os.IsNotExist(err) {
				t.Errorf("deleting a video with delete_files kept its NFO file (stat error: %v)", err)
			}
		}},
		{path: "/api/v1/channels/{}/download-urls", keys: []string{"videos", "url", "channel_url_id", "auth"}, replay: func(t *testing.T) {
			w := sendJSON(t, post, "/api/v1/channels/1/download-urls", map[string]any{"videos": []models.ManualVideo{{URL: "not a url"}}})
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not a url") {
				t.Errorf("downloading an invalid URL = %d %s, want 400 naming it", w.Code, w.Body.String())
			}
			badAuth := []models.ManualVideo{{URL: "https://example.com/v/auth", Auth: &models.ManualAuth{Username: "user", LoginURL: "not a url"}}}
			if w := sendJSON(t, post, "/api/v1/channels/1/download-urls", map[string]any{"videos": badAuth}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "authentication") {
				t.Errorf("downloading with an invalid login URL = %d %s, want 400", w.Code, w.Body.String())
			}
			goodAuth := []models.ManualVideo{{URL: "https://example.com/v/auth", Auth: &models.ManualAuth{Username: "user", LoginURL: "https://example.com/login"}}}
			if w := sendJSON(t, post, "/api/v1/channels/1/available-urls", map[string]any{"videos": goodAuth}); w.Code != http.StatusBadRequest {
				t.Errorf("sending a video with authentication to Available = %d %s, want 400 as it isn't stored", w.Code, w.Body.String())
			}
			checkUnknownKeyRefused(t, post, "/api/v1/channels/1/download-urls", `{"videos":[{"url":"not a url"}]}`)
		}},
		{path: "/api/v1/channels/{}/available-urls", keys: []string{"videos", "url", "channel_url_id"}, replay: func(t *testing.T) {
			const videoURL = "https://example.com/v/wiring-sent"
			body := fmt.Sprintf(`{"videos":[{"url":%q,"channel_url_id":%d}]}`, videoURL, fuzzChannelURLID(t))
			var got struct {
				Videos   []models.Video `json:"videos"`
				Existing []string       `json:"existing"`
			}
			for i, want := range []int{1, 0} { // Sending it again finds it already in the channel.
				w := sendRequest(t, post, "/api/v1/channels/1/available-urls", "application/json", body)
				if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil || len(got.Videos) != want || len(got.Existing) != 1-want {
					t.Fatalf("sending to Available (try %d) = %d %s, want %d added", i+1, w.Code, w.Body.String(), want)
				}
			}
			var status string
			if err := fuzzServer.db.QueryRow("SELECT d.status FROM downloads d JOIN videos v ON v.id = d.video_id WHERE v.url = ?", videoURL).Scan(&status); err != nil || status != string(consts.DLStatusAvailable) {
				t.Errorf("sent video's status = %q (%v), want %q", status, err, consts.DLStatusAvailable)
			}
			checkUnknownKeyRefused(t, post, "/api/v1/channels/1/available-urls", body)
		}},
		{path: "/api/v1/channels/{}/ignore-crawl", replay: routed(post, "/api/v1/channels/999999/ignore-crawl", http.StatusNotFound)},
		{path: "/api/v1/channels/{}/latest-downloads", replay: readsJSON("/api/v1/channels/1/latest-downloads")},
		{path: "/api/v1/channels/{}/notification-seen", replay: routed(post, "/api/v1/channels/1/notification-seen", http.StatusOK)},
		{path: "/api/v1/channels/{}/run-metarr", keys: []string{"video_ids", "overrides", "metarr", "rerun"}, replay: strictJSON(post, "/api/v1/channels/999999/run-metarr", `{"video_ids":[1]}`, http.StatusNotFound)},
		{path: "/api/v1/channels/{}/stop-metarr", keys: []string{"video_ids"}, replay: strictJSON(post, "/api/v1/channels/1/stop-metarr", `{"video_ids":[999999]}`, http.StatusConflict)},
		{path: "/api/v1/channels/{}/toggle-pause", replay: func(t *testing.T) {
			var first, second struct {
				Paused bool `json:"paused"`
			}
			for _, v := range []any{&first, &second} {
				w := sendRequest(t, post, "/api/v1/channels/1/toggle-pause", "", "")
				if w.Code != http.StatusOK {
					t.Fatalf("toggling pause = %d %s", w.Code, w.Body.String())
				}
				if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
					t.Fatal(err)
				}
			}
			if first.Paused == second.Paused {
				t.Error("toggling pause twice didn't change it")
			}
		}},
		{path: "/api/v1/downloads", replay: readsJSON("/api/v1/downloads")},
		{path: "/api/v1/downloads/cancel", keys: []string{"video_ids"}, replay: strictJSON(post, "/api/v1/downloads/cancel", `{"video_ids":[999999]}`, http.StatusNotFound)},
		{path: "/api/v1/logs", replay: routed(get, "/api/v1/logs", 0)},
		{path: "/api/v1/logs/level", replay: readsJSON("/api/v1/logs/level")},
		{path: "/api/v1/logs/level/{}", replay: func(t *testing.T) {
			var before, after struct {
				Level int `json:"level"`
			}
			fetchJSON(t, "/api/v1/logs/level", &before)
			if w := sendRequest(t, post, "/api/v1/logs/level/3", "", ""); w.Code != http.StatusOK {
				t.Fatalf("setting the log level = %d %s", w.Code, w.Body.String())
			}
			fetchJSON(t, "/api/v1/logs/level", &after)
			if after.Level != 3 {
				t.Errorf("log level set to 3 reads back as %d", after.Level)
			}
			sendRequest(t, post, fmt.Sprintf("/api/v1/logs/level/%d", before.Level), "", "")
		}},
		{path: "/api/v1/logs/metarr", replay: routed(get, "/api/v1/logs/metarr", http.StatusOK)},
		{path: "/api/v1/post-processing", replay: readsJSON("/api/v1/post-processing")},
		{path: "/api/v1/settings/crawl-concurrency", keys: []string{"limit"}, replay: limitSetting("/api/v1/settings/crawl-concurrency")},
		{path: "/api/v1/settings/global-download-concurrency", keys: []string{"limit"}, replay: limitSetting("/api/v1/settings/global-download-concurrency")},
		{path: "/api/v1/settings/post-process-concurrency", keys: []string{"limit"}, replay: limitSetting("/api/v1/settings/post-process-concurrency")},
		{path: "/api/v1/settings/domain-limits", keys: []string{"hostname", "download", "crawl"}, replay: func(t *testing.T) {
			if w := sendForm(t, put, "/api/v1/settings/domain-limits", url.Values{"hostname": {"media.wiring-limit.example"}, "download": {"2"}, "crawl": {"1"}}); w.Code != http.StatusOK {
				t.Fatalf("setting a domain limit = %d %s", w.Code, w.Body.String())
			}
			defer sendRequest(t, del, "/api/v1/settings/domain-limits/wiring-limit.example", "", "")
			var limits map[string]models.DomainLimit
			fetchJSON(t, "/api/v1/settings/domain-limits", &limits)
			if got := limits["wiring-limit.example"]; got != (models.DomainLimit{Download: 2, Crawl: 1}) {
				t.Errorf("domain limit reads back as %+v", got)
			}
		}},
		{path: "/api/v1/settings/domain-limits/{}", replay: func(t *testing.T) {
			sendForm(t, put, "/api/v1/settings/domain-limits", url.Values{"hostname": {"media.wiring-remove.example"}, "download": {"2"}})
			if w := sendRequest(t, del, "/api/v1/settings/domain-limits/media.wiring-remove.example", "", ""); w.Code != http.StatusOK {
				t.Fatalf("removing a domain limit = %d %s", w.Code, w.Body.String())
			}
			var limits map[string]models.DomainLimit
			fetchJSON(t, "/api/v1/settings/domain-limits", &limits)
			if _, ok := limits["wiring-remove.example"]; ok {
				t.Error("removing a domain limit by the name it was entered as left it saved")
			}
		}},
		{path: "/api/v1/settings/flaresolverr-url", keys: []string{"url"}, replay: func(t *testing.T) {
			if w := sendForm(t, put, "/api/v1/settings/flaresolverr-url", url.Values{"url": {"http://localhost:8191"}}); w.Code != http.StatusOK {
				t.Fatalf("setting the FlareSolverr URL = %d %s", w.Code, w.Body.String())
			}
			defer sendForm(t, put, "/api/v1/settings/flaresolverr-url", url.Values{"url": {""}})
			var got struct {
				URL string `json:"url"`
			}
			fetchJSON(t, "/api/v1/settings/flaresolverr-url", &got)
			if got.URL != "http://localhost:8191" {
				t.Errorf("FlareSolverr URL reads back as %q", got.URL)
			}
		}},
		{path: "/api/v1/settings/flaresolverr-url/test", keys: []string{"url"}, replay: func(t *testing.T) {
			// Nothing listens on port 1, so the test fails at connecting: proof the URL was read.
			if w := sendForm(t, post, "/api/v1/settings/flaresolverr-url/test", url.Values{"url": {"http://127.0.0.1:1"}}); w.Code != http.StatusBadGateway {
				t.Errorf("testing an unreachable FlareSolverr = %d %s, want 502", w.Code, w.Body.String())
			}
		}},
		{path: "/api/v1/settings/scrape-config-file", keys: []string{"path"}, replay: func(t *testing.T) {
			path := setScrapeConfig(t)
			var got struct {
				Path string `json:"path"`
			}
			fetchJSON(t, "/api/v1/settings/scrape-config-file", &got)
			if got.Path != path {
				t.Errorf("scrape config file reads back as %q, want %q", got.Path, path)
			}
		}},
		{path: "/api/v1/settings/scrape-config-file/content", keys: []string{"content", "version"}, replay: func(t *testing.T) {
			setScrapeConfig(t)
			var current struct {
				Content string `json:"content"`
				Version string `json:"version"`
			}
			fetchJSON(t, "/api/v1/settings/scrape-config-file/content", &current)
			if w := sendForm(t, put, "/api/v1/settings/scrape-config-file/content", url.Values{"content": {current.Content}, "version": {current.Version}}); w.Code != http.StatusOK {
				t.Errorf("saving the scrape config = %d %s", w.Code, w.Body.String())
			}
			if w := sendForm(t, put, "/api/v1/settings/scrape-config-file/content", url.Values{"content": {current.Content}, "version": {"stale"}}); w.Code != http.StatusConflict {
				t.Errorf("saving the scrape config with a stale version = %d, want 409", w.Code)
			}
		}},
		{path: "/api/v1/settings/scrape-config-file/reload", replay: routed(post, "/api/v1/settings/scrape-config-file/reload", http.StatusOK)},
		{path: "/api/v1/settings/scrape-config-file/sites", replay: readsJSON("/api/v1/settings/scrape-config-file/sites")},
		{path: "/api/v1/settings/scrape-config-file/test", keys: []string{"url"}, replay: func(t *testing.T) {
			// No rules match this address, so nothing is fetched.
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test", url.Values{"url": {"http://127.0.0.1:1/video"}}); w.Code != http.StatusOK {
				t.Errorf("testing scrape rules = %d %s", w.Code, w.Body.String())
			}
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test", nil); w.Code != http.StatusBadRequest {
				t.Errorf("testing scrape rules without a URL = %d, want 400", w.Code)
			}
		}},
		{path: "/api/v1/settings/scrape-config-file/test-crawl", keys: []string{"url"}, replay: func(t *testing.T) {
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test-crawl", url.Values{"url": {"http://127.0.0.1:1/channel"}}); w.Code != http.StatusOK {
				t.Errorf("testing crawl rules = %d %s", w.Code, w.Body.String())
			}
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test-crawl", nil); w.Code != http.StatusBadRequest {
				t.Errorf("testing crawl rules without a URL = %d, want 400", w.Code)
			}
		}},
		{path: "/api/v1/settings/tls", keys: []string{"cert", "key"}, replay: func(t *testing.T) {
			if os.Getenv(sharedconsts.EnvTubarrWebTLSCert) != "" {
				t.Skip("certificate set by " + sharedconsts.EnvTubarrWebTLSCert)
			}
			pem, err := generateSelfSigned("localhost")
			if err != nil {
				t.Fatal(err)
			}
			path := wiringFile(t, "cert.pem", string(pem))
			if w := sendForm(t, put, "/api/v1/settings/tls", url.Values{"cert": {path}, "key": {""}}); w.Code != http.StatusOK {
				t.Fatalf("setting the certificate = %d %s", w.Code, w.Body.String())
			}
			defer sendForm(t, put, "/api/v1/settings/tls", url.Values{"cert": {""}, "key": {""}})
			var got struct {
				Cert string `json:"cert"`
			}
			fetchJSON(t, "/api/v1/settings/tls", &got)
			if got.Cert != path {
				t.Errorf("certificate reads back as %q, want %q", got.Cert, path)
			}
		}},
		{path: "/api/v1/settings/tls/enabled", keys: []string{"enabled"}, replay: func(t *testing.T) {
			if os.Getenv(sharedconsts.EnvTubarrWebHTTPS) != "" {
				t.Skip("HTTPS set by " + sharedconsts.EnvTubarrWebHTTPS)
			}
			if w := sendForm(t, put, "/api/v1/settings/tls/enabled", url.Values{"enabled": {"true"}}); w.Code != http.StatusOK {
				t.Fatalf("turning HTTPS on = %d %s", w.Code, w.Body.String())
			}
			defer sendForm(t, put, "/api/v1/settings/tls/enabled", url.Values{"enabled": {"false"}})
			var got struct {
				Enabled bool `json:"enabled"`
			}
			fetchJSON(t, "/api/v1/settings/tls", &got)
			if !got.Enabled {
				t.Error("HTTPS turned on reads back as off")
			}
		}},
		{path: "/api/v1/videos/available", replay: readsJSON("/api/v1/videos/available")},
		{path: "/api/v1/videos/available?ignored=1", replay: readsJSON("/api/v1/videos/available?ignored=1")},
	}
}

// TestPageRequestsWired tests that every API request the pages make is listed in pageRequests, sends the keys listed
// with it, and reaches a route that uses them.
func TestPageRequestsWired(t *testing.T) {
	setupFuzzServer(t)

	found := map[string][]string{} // Path -> pages requesting it.
	sources := map[string]string{}
	for name, src := range webPages(t) {
		src = templatePartRe.ReplaceAllString(src, "{}")
		sources[name] = src
		for _, m := range apiPathRe.FindAllStringSubmatch(src, -1) {
			if !slices.Contains(found[m[1]], name) {
				found[m[1]] = append(found[m[1]], name)
			}
		}
	}

	requests := pageRequests()
	listed := map[string]bool{}
	for _, r := range requests {
		listed[r.path] = true
	}
	for path, names := range found {
		if !listed[path] {
			t.Errorf("%v request %s, which pageRequests doesn't list: add it so its wiring is tested", names, path)
		}
	}

	for _, r := range requests {
		names, ok := found[r.path]
		if !ok {
			t.Errorf("pageRequests lists %s, which no page requests", r.path)
			continue
		}
		if len(r.keys) > 0 && !slices.ContainsFunc(names, func(n string) bool { return keysNearPath(sources[n], r.path, r.keys) }) {
			t.Errorf("%v don't send all of %v with %s", names, r.keys, r.path)
		}
		if r.replay != nil {
			t.Run(strings.TrimPrefix(r.path, "/api/v1/"), r.replay)
		}
	}
}

// ******************************** Live channel **************************************************************************************

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
	if os.Getenv("TUBARR_LIVE_TESTS") == "" {
		t.Skip("set TUBARR_LIVE_TESTS=1 to run real channels through yt-dlp and Metarr")
	}
	for _, tool := range []string{sharedconsts.ProgramYTDLP, command.DownloaderFFmpeg} {
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
	{{value: "dogs"}},
	{{value: "dogs", omits: true}, {value: "cats"}},
}

// liveFiltersText writes filters as the channel pages send them.
func liveFiltersText(filters []liveFilter) string {
	lines := make([]string, 0, len(filters))
	for _, f := range filters {
		op, cond := "contains", "must"
		if f.omits {
			op = "omits"
		}
		if f.any {
			cond = "any"
		}
		lines = append(lines, fmt.Sprintf("title:%s:%s:%s", op, f.value, cond))
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
		if idle && busy == 0 && !slices.Contains(activity["jobs"], channelID) && !slices.Contains(activity["metarr"], channelID) {
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
	_, metarrErr := exec.LookPath(sharedconsts.ProgramMetarr)
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
	set("filters", liveFiltersText(liveFilterSets[sc.filters]))
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
	set("max_resolution", pick("", "720"))
	switch pickIndex(3) {
	case 1:
		sc.moves = []liveMove{{value: "cats", dir: filepath.Join(dir, "cats")}}
		form.Set("move_ops", "title:contains:cats||"+sc.moves[0].dir)
	case 2:
		sc.moves = []liveMove{{feed: "b", value: "dogs", dir: filepath.Join(dir, "dogs")}} // Set in the second URL's own settings, below.
	}
	outputDir := pick("", filepath.Join(dir, "metarr"), filepath.Join(dir, "metarr", "{{year}}"))
	set("metarr_output_directory", outputDir)
	sc.outputDir = strings.ReplaceAll(outputDir, "{{year}}", "2026")
	set("metarr_output_ext", pick("", "mkv", "mp4"))
	set("metarr_rename_style", pick("", "underscores", "spaces", "fixes-only", "skip"))
	set("metarr_filename_ops", pick("", "prefix:[Live] ", "suffix: [Live]"))
	set("metarr_meta_ops", pick("", "artist:set:Live Band"))
	set("metarr_filtered_meta_ops", pick("", "title:contains:cats||artist:set:Cat Band"))
	set("metarr_filtered_filename_ops", pick("", "title:contains:dogs||prefix:[Dog] "))
	purge := pick("", "json", "all")
	set("metarr_purge_metafile", purge)
	sc.purge = purge != ""
	set("metarr_concurrency", pick("", "1", "2"))
	set("metarr_transcode_video_transcode_codecs", pick(liveTranscodes("libx265", "h264:hevc", "libvpx-vp9", "h264:vp9")...))
	set("metarr_transcode_audio_codecs", pick(liveTranscodes("libmp3lame", "aac:mp3", "libopus", "aac:opus")...))
	set("metarr_transcode_video_args", pick("", "hevc:-crf 28"))
	set("metarr_extra_ffmpeg_args", pick("", "-threads 1"))
	var notifications [][2]string
	n := pickIndex(4)
	if sc.notify["all"] = n&1 != 0; sc.notify["all"] {
		notifications = append(notifications, [2]string{"", site.srv.URL + "/notify/all"})
	}
	if sc.notify["b"] = n&2 != 0; sc.notify["b"] {
		notifications = append(notifications, [2]string{site.feedURL("b"), site.srv.URL + "/notify/b"})
	}
	if len(notifications) > 0 {
		set("notifications", notificationsJSON(notifications...))
	}

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
		bSettings["filters"] = liveFiltersText(b.filters)
	}
	if len(sc.moves) > 0 && sc.moves[0].feed == "b" {
		bSettings["move_ops"] = "title:contains:" + sc.moves[0].value + "||" + sc.moves[0].dir
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
	if err := rows.Err(); err != nil {
		sc.t.Fatal(err)
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
			if r.channelURLID != before[u].channelURLID || r.channelURL != before[u].channelURL {
				sc.errorf("video %q moved from channel URL %d %q to %d %q when downloaded from Available",
					u, before[u].channelURLID, before[u].channelURL, r.channelURLID, r.channelURL)
			}
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
		if got := sc.details().URLSettings[sc.site.feedURL("b")].Settings["paused"] == true; got != b.paused {
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
		text := liveFiltersText(liveFilterSets[sc.filters])
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
		waitLive(sc.t, sc.id) // In the download queue before the request returns.
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
		if w := sendJSON(sc.t, http.MethodPost, "/api/v1/downloads/cancel", map[string]any{"video_ids": []int64{before[u].id}}); w.Code != http.StatusOK {
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

// ******************************** Max CPU *******************************************************************************************

// TestMaxCPURoundTrip tests the edit form gets back a Max CPU its 0-100 field can hold, or the browser won't submit the
// form, and that saving it again keeps it.
func TestMaxCPURoundTrip(t *testing.T) {
	setupFuzzServer(t)
	form := webChannelForm(t, "Max CPU Round Trip", "https://example.com/@max-cpu")
	form.Set("metarr_max_cpu_usage", "100")
	id := addWebChannel(t, form)

	for _, save := range []string{"100", "80.5"} {
		form.Set("metarr_max_cpu_usage", save)
		if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusOK {
			t.Fatalf("saving %s = %d %s", save, w.Code, w.Body.String())
		}
		var got channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
		if v := formValue(got.Metarr["metarr_max_cpu_usage"]); v != save {
			t.Errorf("saved %s, the form gets back %q", save, v)
		}
	}
}

// TestMaxCPUFieldsTakeDecimals tests the forms' Max CPU fields allow decimals, or the browser won't submit the form.
func TestMaxCPUFieldsTakeDecimals(t *testing.T) {
	field := regexp.MustCompile(`(?s)<input[^>]*metarr_max_cpu_usage[^>]*>`)
	for _, partial := range []string{"settings-fields.html", "url-settings-fields.html"} {
		page, err := os.ReadFile(filepath.Join(pkgDir, "..", "..", "web", "partials", partial))
		if err != nil {
			t.Fatal(err)
		}
		found := field.Find(page)
		if found == nil {
			t.Errorf("%s has no Max CPU field", partial)
			continue
		}
		if !regexp.MustCompile(`step="any"`).Match(found) {
			t.Errorf("%s: Max CPU field has no step=\"any\": %s", partial, found)
		}
	}
}

// ******************************** Metarr logs ***************************************************************************************

// TestPostMetarrLogsTagsChannel tests Metarr's lines are tagged with the channel in the log URL, other lines kept as
// sent, and the reply is Tubarr's debug level.
func TestPostMetarrLogsTagsChannel(t *testing.T) {
	vars.MetarrLogsMutex.Lock()
	saved := vars.MetarrLogs
	vars.MetarrLogs = nil
	vars.MetarrLogsMutex.Unlock()
	t.Cleanup(func() {
		vars.MetarrLogsMutex.Lock()
		vars.MetarrLogs = saved
		vars.MetarrLogsMutex.Unlock()
	})

	body := `{"level":"info","message":"renamed"}` + "\nnot json\n" + `{"level":"warn","channel":"Dogs","message":"kept"}` + "\n"
	req := httptest.NewRequest(http.MethodPost, "/metarr-logs?channel=Cats+%26+Co", strings.NewReader(body))
	w := httptest.NewRecorder()
	(&serverStore{}).handlePostMetarrLogs(w, req)
	if got, want := w.Body.String(), strconv.Itoa(logging.Level()); got != want {
		t.Errorf("reply = %q, want the debug level %q", got, want)
	}

	vars.MetarrLogsMutex.RLock()
	lines := slices.Clone(vars.MetarrLogs)
	vars.MetarrLogsMutex.RUnlock()
	if len(lines) != 3 {
		t.Fatalf("stored %d lines, want 3", len(lines))
	}
	var tagged map[string]any
	if err := json.Unmarshal(lines[0], &tagged); err != nil {
		t.Fatal(err)
	}
	if tagged["channel"] != "Cats & Co" || tagged["message"] != "renamed" {
		t.Errorf("first line = %s, want it tagged Cats & Co", lines[0])
	}
	if string(lines[1]) != "not json\n" {
		t.Errorf("plain line = %q, want it kept as sent", lines[1])
	}
	if !strings.Contains(string(lines[2]), `"channel":"Dogs"`) {
		t.Errorf("already tagged line = %s, want its own channel kept", lines[2])
	}
}

// ******************************** Move op directories *******************************************************************************

// TestMoveOpDirectoryChecked tests saving a channel refuses a new move op directory with bad template tags, as it does a
// video directory, since one with reversed braces would otherwise reach the directory parser when a video matches.
func TestMoveOpDirectoryChecked(t *testing.T) {
	setupFuzzServer(t)
	const u = "https://example.com/@move-op-dir"
	form := webChannelForm(t, "Move Op Dir", u)
	id := addWebChannel(t, form)
	dir := testDir

	// Channel level.
	for _, tc := range []struct {
		name, moveDir string
		want          int
	}{
		{"plain directory", dir, http.StatusOK},
		{"known tag", dir + "/{{channel_name}}", http.StatusOK},
		{"reversed braces", dir + "/}}x{{", http.StatusBadRequest},
		{"unknown tag", dir + "/{{nope}}", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form.Set("move_ops", "title:contains:x||"+tc.moveDir)
			if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != tc.want {
				t.Errorf("saving = %d %s, want %d", w.Code, w.Body.String(), tc.want)
			}
		})
	}

	// Channel URL level.
	form.Del("move_ops")
	setURLSettings(t, form, u, map[string]any{"move_ops": "title:contains:x||" + dir + "/}}x{{"})
	if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusBadRequest {
		t.Errorf("saving it for the URL = %d %s, want %d", w.Code, w.Body.String(), http.StatusBadRequest)
	}
}

// ******************************** Output directory tags *****************************************************************************

// TestOutputDirectoryTagsOnSave tests which directories accept the output directory tags when a channel is saved.
func TestOutputDirectoryTagsOnSave(t *testing.T) {
	setupFuzzServer(t)
	dir := t.TempDir()
	const (
		channelTag = "{{channel_output_directory}}/Shorts"
		urlTag     = "{{channel_url_output_directory}}/Shorts"
		year       = "/{{year}}"
	)
	type fields = map[string]any

	tests := []struct {
		name        string
		video       string
		jsonDir     string
		output      string
		urlSettings fields
		want        bool
	}{
		// Video and JSON directories accept neither tag, nor a Metarr tag.
		{name: "channel video, channel tag", video: channelTag},
		{name: "channel video, URL tag", video: urlTag},
		{name: "channel JSON, channel tag", video: dir, jsonDir: channelTag},
		{name: "channel JSON, URL tag", video: dir, jsonDir: urlTag},
		{name: "channel JSON, Metarr tag", video: dir, jsonDir: dir + year},
		{name: "URL video, channel tag", video: dir, urlSettings: fields{"settings": fields{"video_directory": channelTag}}},
		{name: "URL video, URL tag", video: dir, urlSettings: fields{"settings": fields{"video_directory": urlTag}}},
		{name: "URL JSON, channel tag", video: dir, urlSettings: fields{"settings": fields{"json_directory": channelTag}}},
		{name: "URL JSON, URL tag", video: dir, urlSettings: fields{"settings": fields{"json_directory": urlTag}}},
		{name: "URL JSON, Metarr tag", video: dir, urlSettings: fields{"settings": fields{"json_directory": dir + year}}},

		// Metarr output directories accept both on a channel URL, and only the channel's on a channel.
		{name: "channel output, channel tag", video: dir, output: channelTag, want: true},
		{name: "channel output, URL tag", video: dir, output: urlTag},
		{name: "URL output, channel tag", video: dir, urlSettings: fields{"metarr": fields{"metarr_output_directory": channelTag}}, want: true},
		{name: "URL output, URL tag", video: dir, urlSettings: fields{"metarr": fields{"metarr_output_directory": urlTag}}, want: true},
		{name: "URL output, channel tag with a Metarr tag in the channel's", video: dir, output: dir + year, urlSettings: fields{"metarr": fields{"metarr_output_directory": channelTag}}, want: true},
	}
	for i, tt := range tests {
		name := fmt.Sprintf("tags%d", i)
		form := url.Values{
			"name":                           {name},
			"urls":                           {"https://example.com/@" + name},
			jsonkeys.SettingsVideoDirectory:  {tt.video},
			jsonkeys.SettingsJSONDirectory:   {tt.jsonDir},
			jsonkeys.MetarrOutputDirectory:   {tt.output},
			jsonkeys.SettingsCrawlFreq:       {"30"},
			jsonkeys.SettingsDownloadRetries: {"1"},
			jsonkeys.SettingsAutoDownload:    {"false"},
		}
		if tt.urlSettings != nil {
			b, err := json.Marshal(fields{"https://example.com/@" + name: tt.urlSettings})
			if err != nil {
				t.Fatal(err)
			}
			form.Set("url_settings", string(b))
		}
		w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form)
		if saved := w.Code < 300; saved != tt.want {
			t.Errorf("%s: got %d %s, want saved: %t", tt.name, w.Code, w.Body.String(), tt.want)
		}
		if w.Code < 300 {
			deleteChannelNamed(t, name)
		}
	}
}

// TestDownloadAsOutputDirectoryTags tests only Download As's post-process directory accepts the output directory tags.
func TestDownloadAsOutputDirectoryTags(t *testing.T) {
	for _, tag := range []string{"{{channel_output_directory}}/Once", "{{channel_url_output_directory}}/Once"} {
		if _, err := parseOverrideLines(&models.OverrideLines{VideoDir: tag}, nil); err == nil {
			t.Errorf("video directory %q was accepted", tag)
		}
		if _, err := parseOverrideLines(&models.OverrideLines{JSONDir: tag}, nil); err == nil {
			t.Errorf("JSON directory %q was accepted", tag)
		}
		if o, err := parseOverrideLines(&models.OverrideLines{OutputDir: tag}, nil); err != nil || o.OutputDir != tag {
			t.Errorf("post-process directory %q: got %+v, %v", tag, o, err)
		}
	}
}

// ******************************** Overrides *****************************************************************************************

// TestParseOverrideLinesChecksMetarrArgs tests that Download As options are checked and normalized as saved settings are.
func TestParseOverrideLinesChecksMetarrArgs(t *testing.T) {
	bad := "xyz"
	if _, err := parseOverrideLines(&models.OverrideLines{}, &metarrArgsInput{OutputExt: &bad}); err == nil {
		t.Error("accepted Metarr output extension xyz")
	}

	upper := "MKV"
	o, err := parseOverrideLines(&models.OverrideLines{}, &metarrArgsInput{OutputExt: &upper})
	if err != nil {
		t.Fatal(err)
	}
	if o.OutputExt != "mkv" {
		t.Errorf("Metarr output extension %q not lowercased, got %q", upper, o.OutputExt)
	}
}

// TestParseOverrideLinesYtdlpOptions tests that Download As yt-dlp options are read one codec per line, normalized and
// checked as saved settings are.
func TestParseOverrideLinesYtdlpOptions(t *testing.T) {
	o, err := parseOverrideLines(&models.OverrideLines{
		YtdlpOutputExt:            "mkv",
		MaxFilesize:               "2G",
		MaxResolution:             "4K",
		ExtraYTDLPVideoArgs:       " --no-mtime ",
		YTDLPPreferredVideoCodecs: "AV1\n\nvp9",
		YTDLPPreferredAudioCodecs: "opus",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.YtdlpOutputExt != "mkv" || o.MaxFilesize != "2g" || o.MaxResolution != "2160" || o.ExtraYTDLPVideoArgs != "--no-mtime" ||
		strings.Join(o.YTDLPPreferredVideoCodecs, ",") != "av1,vp9" || strings.Join(o.YTDLPPreferredAudioCodecs, ",") != "opus" {
		t.Errorf("got %+v", o)
	}

	for _, bad := range []models.OverrideLines{{YtdlpOutputExt: "xyz"}, {MaxFilesize: "lots"}, {MaxResolution: "huge"}, {YTDLPPreferredVideoCodecs: "betamax"}} {
		if _, err := parseOverrideLines(&bad, nil); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

// TestParseOverrideLinesTurnOffOptions tests the dialog's choices that turn off a channel URL's setting are accepted, as
// its body decodes.
func TestParseOverrideLinesTurnOffOptions(t *testing.T) {
	var input metarrArgsInput
	body := `{"metarr_output_ext":"same","metarr_rename_style":"skip","metarr_purge_metafile":"keep","metarr_purge_original_video":false}`
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatal(err)
	}
	o, err := parseOverrideLines(&models.OverrideLines{}, &input)
	if err != nil {
		t.Fatal(err)
	}
	if o.OutputExt != "same" || o.RenameStyle != "skip" || o.MetaPurge != "keep" || o.PurgeOriginalVideo == nil || *o.PurgeOriginalVideo {
		t.Errorf("got %+v", o.MetarrArgs)
	}
}

// TestCheckOverridesFit tests that Download As options are checked together with the channel URL's they leave blank, and
// that its transcode boxes replace the channel URL's.
func TestCheckOverridesFit(t *testing.T) {
	c := &models.Channel{URLModels: []*models.ChannelURL{
		{ID: 1, ChanURLMetarrArgs: &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}},
		{ID: 2, ChanURLMetarrArgs: &models.MetarrArgs{OutputExt: "webm"}},
		{ID: 3},
		{ID: 4, ChanURLMetarrArgs: &models.MetarrArgs{ExtraFFmpegArgs: "-af volume=0.5", TranscodeAudioCodecs: []string{"aac"}}},
		{ID: 5, ChanURLMetarrArgs: &models.MetarrArgs{TranscodeGPU: "cuda", TranscodeVideoArgs: []string{"av1:-preset p5"}}},
	}}
	tests := []struct {
		name  string
		urlID int64
		o     models.MetarrArgs
		fits  bool
	}{
		{"webm over the URL's h264", 1, models.MetarrArgs{OutputExt: "webm", TranscodeVideoCodecs: []string{"h264"}}, false},
		{"webm with the URL's h264 removed", 1, models.MetarrArgs{OutputExt: "webm"}, true},
		{"webm replacing the URL's h264 with vp9", 1, models.MetarrArgs{OutputExt: "webm", TranscodeVideoCodecs: []string{"vp9"}}, true},
		{"h264 under the URL's webm", 2, models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}, false},
		{"mp4 replacing the URL's webm", 2, models.MetarrArgs{OutputExt: "mp4", TranscodeVideoCodecs: []string{"h264"}}, true},
		{"URL with no Metarr args", 3, models.MetarrArgs{OutputExt: "webm"}, true},
		{"a copy line", 1, models.MetarrArgs{TranscodeVideoArgs: []string{"copy:-crf 20"}}, false},
		{"an NVENC preset with no GPU", 1, models.MetarrArgs{TranscodeVideoArgs: []string{"av1:-preset p5"}}, false},
		{"an NVENC preset under the URL's GPU", 5, models.MetarrArgs{TranscodeVideoArgs: []string{"av1:-preset p5"}}, true},
		{"the URL's NVENC preset with the GPU turned off", 5, models.MetarrArgs{TranscodeGPU: "none", TranscodeVideoArgs: []string{"av1:-preset p5"}}, false},
		{"the URL's NVENC preset removed with the GPU turned off", 5, models.MetarrArgs{TranscodeGPU: "none"}, true},
		{"copied audio with the URL's audio filter", 4, models.MetarrArgs{TranscodeAudioCodecs: []string{"copy"}, ExtraFFmpegArgs: "-af volume=0.5"}, false},
		{"copied audio with the URL's audio filter removed", 4, models.MetarrArgs{TranscodeAudioCodecs: []string{"copy"}}, true},
		{"another audio codec with the URL's audio filter", 4, models.MetarrArgs{TranscodeAudioCodecs: []string{"ac3"}, ExtraFFmpegArgs: "-af volume=0.5"}, true},
	}
	for _, tt := range tests {
		err := checkOverridesFit(c.URLByID(tt.urlID), &models.VideoOverrides{MetarrArgs: tt.o})
		if (err == nil) != tt.fits {
			t.Errorf("%s: got error %v, want fits %t", tt.name, err, tt.fits)
		}
	}
}

// TestMatchesChannelURL tests that Download As options are only overrides where they differ from the channel URL's, so
// the dialog's defaults sent back unchanged aren't kept.
func TestMatchesChannelURL(t *testing.T) {
	metaOps, err := parsing.ParseMetaOps([]string{"title:prefix:[Cats] "})
	if err != nil {
		t.Fatal(err)
	}
	filteredMetaOps, err := parsing.ParseFilteredMetaOps([]string{"title:contains:cat||genre:set:Cats"})
	if err != nil {
		t.Fatal(err)
	}
	filenameOps, err := parsing.ParseFilenameOps([]string{"date-tag:prefix:ymd"})
	if err != nil {
		t.Fatal(err)
	}
	filteredFilenameOps, err := parsing.ParseFilteredFilenameOps([]string{"title:contains:cat||prefix:[Cats] "})
	if err != nil {
		t.Fatal(err)
	}
	cu := &models.ChannelURL{
		ID:              1,
		ChanURLSettings: &models.Settings{VideoDir: "/videos", YtdlpOutputExt: "mkv"},
		ChanURLMetarrArgs: &models.MetarrArgs{
			MetaOps:             metaOps,
			FilteredMetaOps:     filteredMetaOps,
			FilenameOps:         filenameOps,
			FilteredFilenameOps: filteredFilenameOps,
			OutputExt:           "mp4",
			OutputDir:           "/out",
		},
	}
	cu.Levels = []models.SettingsLevel{{Settings: cu.ChanURLSettings, MetarrArgs: cu.ChanURLMetarrArgs}} // As loading it would.
	c := &models.Channel{URLModels: []*models.ChannelURL{cu}}

	// The dialog's defaults sent back as Revert to Defaults leaves them: the channel URL's ops, the rest blank.
	defaults, err := metarr.ChannelURLOverrideLines(cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := parseOverrideLines(&models.OverrideLines{
		MetaOps:             defaults.MetaOps,
		FilteredMetaOps:     defaults.FilteredMetaOps,
		FilenameOps:         defaults.FilenameOps,
		FilteredFilenameOps: defaults.FilteredFilenameOps,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !matchesChannelURL(cu, defaults, sent) {
		t.Fatalf("the dialog's defaults %+v didn't match the channel URL's", defaults)
	}

	// Anything else set must be the channel URL's own value.
	tests := []struct {
		name    string
		o       models.VideoOverrides
		matches bool
	}{
		{"the channel URL's values typed in", models.VideoOverrides{VideoDir: "/videos", YtdlpOutputExt: "mkv", MetarrArgs: models.MetarrArgs{OutputExt: "mp4"}}, true},
		{"a filename", models.VideoOverrides{Filename: "Chosen"}, false},
		{"a stripped thumbnail", models.VideoOverrides{StripThumbnail: true}, false},
		{"another video directory", models.VideoOverrides{VideoDir: "/elsewhere"}, false},
		{"preferred video codecs", models.VideoOverrides{YTDLPPreferredVideoCodecs: []string{"av1"}}, false},
		{"another output extension", models.VideoOverrides{MetarrArgs: models.MetarrArgs{OutputExt: "mkv"}}, false},
		{"video encoder args", models.VideoOverrides{MetarrArgs: models.MetarrArgs{TranscodeVideoArgs: []string{"av1:-crf 35"}}}, false},
		{"the channel URL's output directory, which beats move ops", models.VideoOverrides{MetarrArgs: models.MetarrArgs{OutputDir: "/out"}}, false},
	}
	yes, no := true, false
	for _, tt := range tests {
		o := tt.o
		o.MetaOps = sent.MetaOps
		o.FilteredMetaOps = sent.FilteredMetaOps
		o.FilenameOps = sent.FilenameOps
		o.FilteredFilenameOps = sent.FilteredFilenameOps
		if got := matchesChannelURL(cu, defaults, &o); got != tt.matches {
			t.Errorf("%s: got match %t, want %t", tt.name, got, tt.matches)
		}
	}

	// A Write NFO Files choice is an override only if it differs from what the channel URL does, unset meaning No.
	for _, tt := range []struct {
		name     string
		url, got *bool
		matches  bool
	}{
		{"unset on both", nil, nil, true},
		{"Yes over unset", nil, &yes, false},
		{"No over unset", nil, &no, true},
		{"unset over Yes", &yes, nil, true},
		{"Yes over Yes", &yes, &yes, true},
		{"No over Yes", &yes, &no, false},
		{"Yes over No", &no, &yes, false},
		{"No over No", &no, &no, true},
	} {
		cu.ChanURLMetarrArgs.WriteNFO = tt.url
		o := *sent
		o.WriteNFO = tt.got
		if got := matchesChannelURL(cu, defaults, &o); got != tt.matches {
			t.Errorf("Write NFO Files, %s: got match %t, want %t", tt.name, got, tt.matches)
		}
	}
	cu.ChanURLMetarrArgs.WriteNFO = nil

	// A Keep Original Video Files choice is an override only if it differs from what the channel URL does, unset meaning
	// the original is deleted.
	for _, tt := range []struct {
		name     string
		url, got *bool
		matches  bool
	}{
		{"unset on both", nil, nil, true},
		{"kept over unset", nil, &no, false},
		{"deleted over unset", nil, &yes, true},
		{"unset over kept", &no, nil, true},
		{"kept over kept", &no, &no, true},
		{"deleted over kept", &no, &yes, false},
		{"kept over deleted", &yes, &no, false},
		{"deleted over deleted", &yes, &yes, true},
	} {
		cu.ChanURLMetarrArgs.PurgeOriginalVideo = tt.url
		o := *sent
		o.PurgeOriginalVideo = tt.got
		if got := matchesChannelURL(cu, defaults, &o); got != tt.matches {
			t.Errorf("Keep Original Video Files, %s: got match %t, want %t", tt.name, got, tt.matches)
		}
	}
	cu.ChanURLMetarrArgs.PurgeOriginalVideo = nil

	// The op boxes replace the channel URL's ops, so emptying or adding to them makes overrides.
	moreOps, err := parsing.ParseMetaOps([]string{"title:prefix:[Cats] ", "album:set:Pets"})
	if err != nil {
		t.Fatal(err)
	}
	for name, ops := range map[string][]models.MetaOps{"emptied": nil, "added to": moreOps} {
		o := *sent
		o.MetaOps = ops
		if matchesChannelURL(cu, defaults, &o) {
			t.Errorf("meta ops %s matched the channel URL's", name)
		}
	}

	// The transcode boxes replace the channel URL's too, so they only match when sent back as loaded.
	videoCodecs := []string{"h264:av1"}
	audioCodecs := []string{"aac"}
	videoArgs := []string{"av1:-pix_fmt yuv420p10le"}
	extraArgs := "-map_metadata 0"
	cu.ChanURLMetarrArgs.TranscodeVideoCodecs = videoCodecs
	cu.ChanURLMetarrArgs.TranscodeAudioCodecs = audioCodecs
	cu.ChanURLMetarrArgs.TranscodeVideoArgs = videoArgs
	cu.ChanURLMetarrArgs.ExtraFFmpegArgs = extraArgs
	for _, tt := range []struct {
		name    string
		video   []string
		audio   []string
		args    []string
		extra   string
		matches bool
	}{
		{"sent back as loaded", videoCodecs, audioCodecs, videoArgs, extraArgs, true},
		{"video codecs emptied", nil, audioCodecs, videoArgs, extraArgs, false},
		{"audio codecs emptied", videoCodecs, nil, videoArgs, extraArgs, false},
		{"encoder args emptied", videoCodecs, audioCodecs, nil, extraArgs, false},
		{"extra args emptied", videoCodecs, audioCodecs, videoArgs, "", false},
		{"encoder args edited", videoCodecs, audioCodecs, []string{"av1:-crf 40"}, extraArgs, false},
	} {
		o := *sent
		o.TranscodeVideoCodecs = tt.video
		o.TranscodeAudioCodecs = tt.audio
		o.TranscodeVideoArgs = tt.args
		o.ExtraFFmpegArgs = tt.extra
		if got := matchesChannelURL(cu, defaults, &o); got != tt.matches {
			t.Errorf("transcode boxes, %s: got match %t, want %t", tt.name, got, tt.matches)
		}
	}
}

// TestManualAvailableOverrideOps tests Download As opens for a manual video whose channel URL is gone, using the channel's
// settings as its download does.
func TestManualAvailableOverrideOps(t *testing.T) {
	setupFuzzServer(t)
	serve := func(method, path, contentType, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}

	form := url.Values{
		"name":                           {"Manual Override Channel"},
		"urls":                           {"https://example.com/@manualoverride"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.MetarrMetaOps:           {"artist:set:Cats"},
	}
	if w := serve(http.MethodPost, "/api/v1/channels/add", "application/x-www-form-urlencoded", form.Encode()); w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	s, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	c, found, err := s.ChannelStore().GetChannelModel(consts.QChanName, "Manual Override Channel", true)
	if err != nil || !found {
		t.Fatalf("loading channel: %v %v", found, err)
	}

	if _, _, err := app.AddManualAvailable(s, c, []models.ManualVideo{{URL: "https://example.com/v/manual"}}); err != nil {
		t.Fatal(err)
	}
	// Saving a channel's edit form once deleted its manual downloads URL, which isn't listed in the form, leaving its
	// videos with no channel URL.
	if _, err := fuzzServer.db.Exec("DELETE FROM channel_urls WHERE channel_id = ? AND is_manual = 1", c.ID); err != nil {
		t.Fatal(err)
	}

	w := serve(http.MethodGet, "/api/v1/videos/available", "", "")
	var videos []models.AvailableVideo
	if err := json.Unmarshal(w.Body.Bytes(), &videos); err != nil {
		t.Fatal(err)
	}
	var v *models.AvailableVideo
	for i := range videos {
		if videos[i].URL == "https://example.com/v/manual" {
			v = &videos[i]
		}
	}
	if v == nil {
		t.Fatalf("manual video isn't listed as available: %+v", videos)
	}

	w = serve(http.MethodGet, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", v.ChannelID, v.ChannelURLID, v.ID), "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("override ops for a manual video (channel URL ID %d): %d %s", v.ChannelURLID, w.Code, w.Body.String())
	}
	var got struct {
		Defaults models.OverrideLines `json:"defaults"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Defaults.MetaOps != "artist:set:Cats" {
		t.Errorf("manual video's Download As defaults = %+v, want the channel's ops", got.Defaults)
	}
}

// ******************************** Queue limits **************************************************************************************

// slotHoldingYtdlp puts a fake yt-dlp in PATH that never finishes a "hold-slot" video's metadata, finding nothing else.
func slotHoldingYtdlp(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *hold-slot*) exec sleep 60 ;; esac\necho '{\"entries\":[]}'\n"
	if err := os.WriteFile(filepath.Join(bin, sharedconsts.ProgramYTDLP), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	abstractions.Set(keys.SkipAllWaits, true)
	t.Cleanup(func() { abstractions.Set(keys.SkipAllWaits, nil) })
}

// queueWaitingVideo adds a one-at-a-time channel and queues two of its videos: one holds the slot, the other waits.
func queueWaitingVideo(t *testing.T, form url.Values) int64 {
	t.Helper()
	slotHoldingYtdlp(t)
	id := addWebChannel(t, form)

	// Not due a crawl, so the crawl watchdog leaves it alone.
	if _, err := fuzzServer.db.Exec("UPDATE channel_urls SET last_scan = ? WHERE channel_id = ?", time.Now(), id); err != nil {
		t.Fatal(err)
	}

	c, found, err := fuzzServer.ss.cs.GetChannelModel(consts.QChanID, strconv.FormatInt(id, 10), true)
	if err != nil || !found || c == nil || len(c.URLModels) == 0 {
		t.Fatalf("loading channel %d = %t, %v", id, found, err)
	}
	cu := c.URLModels[0]
	reqs := []queue.Request{
		{Video: &models.Video{URL: cu.URL + "/hold-slot-1", ChannelURLID: cu.ID}, ByUser: true},
		{Video: &models.Video{URL: cu.URL + "/hold-slot-2", ChannelURLID: cu.ID}, ByUser: true},
	}
	if queued, err := fuzzServer.ss.dq.Add(c, reqs...); err != nil || len(queued) != 2 {
		t.Fatalf("Add = %d queued, %v; want both", len(queued), err)
	}
	t.Cleanup(func() {
		fuzzServer.ss.dq.Abort(id)
		waitForStages(t, id, 0, 0)
	})
	waitForStages(t, id, 2, 1)
	return id
}

// waitForStages waits until the router's queue has the given counts of the channel's videos, and of those not started.
func waitForStages(t *testing.T, channelID int64, queued, waiting int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		var stages []string
		n := 0
		for _, e := range fuzzServer.ss.dq.Entries() {
			if e.ChannelID == channelID && e.DownloadStatus != nil {
				stages = append(stages, e.DownloadStatus.Stage)
				if e.DownloadStatus.Stage == "Waiting to start" {
					n++
				}
			}
		}
		if len(stages) == queued && n == waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("channel's queued videos are at stages %q, want %d with %d waiting to start", stages, queued, waiting)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestChannelSaveStartsWaitingVideo tests raising a channel's download concurrency starts its waiting video at once.
func TestChannelSaveStartsWaitingVideo(t *testing.T) {
	setupFuzzServer(t)
	form := webChannelForm(t, "LiveLimitChannel", "https://example.com/@livelimitchannel")
	form.Set(jsonkeys.SettingsMaxConcurrency, "1")
	id := queueWaitingVideo(t, form)

	form.Set(jsonkeys.SettingsMaxConcurrency, "2")
	if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusOK {
		t.Fatalf("saving the channel = %d %s", w.Code, w.Body.String())
	}
	waitForStages(t, id, 2, 0)
}

// TestProfileSaveStartsWaitingVideo tests raising a profile's download concurrency starts its channel's waiting video.
func TestProfileSaveStartsWaitingVideo(t *testing.T) {
	setupFuzzServer(t)
	profile := url.Values{"name": {"Live Limit Profile"}, jsonkeys.SettingsMaxConcurrency: {"1"}}
	if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", profile); w.Code != http.StatusCreated {
		t.Fatalf("adding the profile = %d %s", w.Code, w.Body.String())
	}
	var profileID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", profile.Get("name")).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
	})

	form := webChannelForm(t, "LiveLimitProfile", "https://example.com/@livelimitprofile")
	form.Set("profile_id", strconv.FormatInt(profileID, 10))
	id := queueWaitingVideo(t, form)

	profile.Set(jsonkeys.SettingsMaxConcurrency, "2")
	if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/profiles/%d/update", profileID), profile); w.Code != http.StatusOK {
		t.Fatalf("saving the profile = %d %s", w.Code, w.Body.String())
	}
	waitForStages(t, id, 2, 0)
}

// TestWatchdogAppliesLimitSavedElsewhere tests the crawl watchdog applies a limit saved outside the edit page.
func TestWatchdogAppliesLimitSavedElsewhere(t *testing.T) {
	setupFuzzServer(t)
	form := webChannelForm(t, "LiveLimitWatchdog", "https://example.com/@livelimitwatchdog")
	form.Set(jsonkeys.SettingsMaxConcurrency, "1")
	id := queueWaitingVideo(t, form)

	// Saved straight to the database, without telling the queues.
	if _, err := fuzzServer.ss.cs.UpdateChannelSettingsJSON(consts.QChanID, strconv.FormatInt(id, 10), func(s *models.Settings) error {
		s.DownloadConcurrency = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fuzzServer.ss.crawlDueChannels(t.Context(), map[int64]string{})
	waitForStages(t, id, 2, 0)
}

// ******************************** Router ********************************************************************************************

// TestServePageIncludes tests that every web page has its shared partials filled in.
func TestServePageIncludes(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0) // TestMain moves the working directory, so locate web/ from this source file.
	webRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "web")
	pages, err := filepath.Glob(filepath.Join(webRoot, "*.html"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no web pages found in %q: %v", webRoot, err)
	}

	for _, page := range pages {
		w := httptest.NewRecorder()
		servePage(w, httptest.NewRequest(http.MethodGet, "/"+filepath.Base(page), nil), webRoot, page)
		body := w.Body.String()

		if w.Code != http.StatusOK {
			t.Errorf("%s: got status %d", filepath.Base(page), w.Code)
		}
		for _, leftover := range []string{"<!-- include:", "{{title}}", "{{selected}}"} {
			if strings.Contains(body, leftover) {
				t.Errorf("%s: rendered page still contains %q", filepath.Base(page), leftover)
			}
		}
		if filepath.Base(page) == "login.html" { // Shown before logging in, so it has no sidebar or header.
			continue
		}
		if !strings.Contains(body, "<aside") || !strings.Contains(body, "<header") {
			t.Errorf("%s: rendered page is missing the sidebar or header", filepath.Base(page))
		}
	}
}

// TestWithDefaultPort tests that listen addresses given without a port get the default one.
func TestWithDefaultPort(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0":        "0.0.0.0:8827",
		"localhost":      "localhost:8827",
		"localhost:":     "localhost:8827",
		"::":             "[::]:8827",
		"::1":            "[::1]:8827",
		"[::1]":          "[::1]:8827",
		"[::1]:9000":     "[::1]:9000",
		":9000":          ":9000",
		"127.0.0.1:9000": "127.0.0.1:9000",
	} {
		if got := withDefaultPort(in); got != want {
			t.Errorf("withDefaultPort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLocalURL tests that Metarr and the container scripts are pointed at a reachable address for each listen address.
func TestLocalURL(t *testing.T) {
	for _, tc := range []struct{ scheme, addr, want string }{
		{"http", "127.0.0.1:8827", "http://127.0.0.1:8827"},
		{"http", ":8827", "http://127.0.0.1:8827"},
		{"https", "0.0.0.0:9000", "https://127.0.0.1:9000"},
		{"https", "[::]:9000", "https://127.0.0.1:9000"},
		{"http", "192.168.1.5:8827", "http://192.168.1.5:8827"},
		{"http", "[::1]:8827", "http://[::1]:8827"},
		{"http", "localhost:8827", "http://localhost:8827"},
	} {
		if got := localURL(tc.scheme, tc.addr); got != tc.want {
			t.Errorf("localURL(%q, %q) = %q, want %q", tc.scheme, tc.addr, got, tc.want)
		}
	}
}

// TestServePageMissingPartial tests that a page including an unknown partial fails rather than serving a broken page.
func TestServePageMissingPartial(t *testing.T) {
	logger.Pl.Console = io.Discard // servePage logs the error.
	webRoot := t.TempDir()
	page := filepath.Join(webRoot, "page.html")
	if err := os.WriteFile(page, []byte(`<!-- include:nope title="x" -->`), 0o644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	servePage(w, httptest.NewRequest(http.MethodGet, "/page.html", nil), webRoot, page)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ******************************** Serving thumbnails ********************************************************************************

// noisyJPEG returns a JPEG well over the 512 bytes sniffed for its type.
func noisyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	r := rand.New(rand.NewSource(1))
	for y := range 200 {
		for x := range 200 {
			img.Set(x, y, color.RGBA{uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256)), 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestServeThumbnailContentType tests a thumbnail is sent as an image, whole, with or without an extension (cached ones
// have none).
func TestServeThumbnailContentType(t *testing.T) {
	data := noisyJPEG(t)
	dir := t.TempDir()
	for _, name := range []string{"thumb.jpg", "0123456789abcdef"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			serveThumbnailFile(w, httptest.NewRequest(http.MethodGet, "/", nil), path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if got := w.Header().Get("Content-Type"); got != "image/jpeg" {
				t.Errorf("Content-Type = %q, want image/jpeg", got)
			}
			if !bytes.Equal(w.Body.Bytes(), data) {
				t.Errorf("sent %d bytes, want the whole %d byte image", w.Body.Len(), len(data))
			}
		})
	}
}

// TestServeThumbnailRefusesOtherFiles tests a file that isn't an image isn't served, as the path may be anything typed in.
func TestServeThumbnailRefusesOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.txt")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	serveThumbnailFile(w, httptest.NewRequest(http.MethodGet, "/", nil), path)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ******************************** Thumbnails ****************************************************************************************

// TestThumbnailUpload checks thumbnail uploading works.
func TestThumbnailUpload(t *testing.T) {
	setupFuzzServer(t)
	oldDir := paths.UploadedThumbnailDir
	paths.UploadedThumbnailDir = filepath.Join(t.TempDir(), "thumbnails")
	t.Cleanup(func() { paths.UploadedThumbnailDir = oldDir })

	form := url.Values{
		"name":                           {"Thumbnail Channel"},
		"urls":                           {"https://example.com/@thumbs"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, req)
	if w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	s, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := s.ChannelStore().GetChannelModel(consts.QChanName, "Thumbnail Channel", true)
	if err != nil {
		t.Fatal(err)
	}
	videos := []*models.Video{{ChannelURLID: c.URLModels[0].ID, ChannelURL: c.URLModels[0].URL, URL: "https://example.com/v/thumb"}}
	if _, err := s.VideoStore().AddAvailableVideos(videos, c.ID); err != nil {
		t.Fatal(err)
	}
	videoID := videos[0].ID

	encodeImage := func(t *testing.T, format string, width, height int) []byte {
		t.Helper()

		img := image.NewRGBA(image.Rect(0, 0, width, height))
		var buf bytes.Buffer
		var err error

		switch format {
		case sharedconsts.ITypePNG:
			err = png.Encode(&buf, img)
		case sharedconsts.ITypeJPG, sharedconsts.ITypeJPEG:
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
		case sharedconsts.ITypeGIF:
			err = gif.Encode(&buf, img, nil)
		default:
			t.Fatalf("unsupported test image format %q", format)
		}
		if err != nil {
			t.Fatal(err)
		}

		return buf.Bytes()
	}

	upload := func(id int64, filename string, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("thumbnail", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/videos/%d/thumbnail", id), &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}
	savedPath := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		var got struct{ Path string }
		if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
		return got.Path
	}

	// Valid PNG, GIF and JPEG uploads must succeed, each in a new file named for the video.
	checkSaved := func(path, ext string) {
		t.Helper()
		if filepath.Dir(path) != paths.UploadedThumbnailDir || !strings.HasPrefix(filepath.Base(path), fmt.Sprintf("%d_", videoID)) || filepath.Ext(path) != ext {
			t.Errorf("%s image saved to %q", ext, path)
		}
	}
	pngData := encodeImage(t, sharedconsts.ITypePNG, 2, 2)
	pngPath := savedPath(upload(videoID, "cover.png", pngData))
	checkSaved(pngPath, ".png")

	gifData := encodeImage(t, sharedconsts.ITypeGIF, 2, 2)
	gifPath := savedPath(upload(videoID, "cover.gif", gifData))
	checkSaved(gifPath, ".gif")

	jpgData := encodeImage(t, sharedconsts.ITypeJPG, 2, 2)
	jpg := savedPath(upload(videoID, "cover.jpg", jpgData))
	checkSaved(jpg, ".jpg")

	// Earlier uploads stay, as a saved thumbnail must survive a dialog cancelled after an upload, of any type.
	for _, earlier := range []string{pngPath, gifPath} {
		if _, err := os.Stat(earlier); err != nil {
			t.Errorf("earlier upload removed by a later one: %v", err)
		}
	}
	if again := savedPath(upload(videoID, "cover.png", pngData)); again == pngPath {
		t.Errorf("second PNG overwrote the first at %q", pngPath)
	}

	// Invalid image data must fail.
	if w := upload(videoID, "cover.png", []byte("not an image")); w.Code != http.StatusBadRequest {
		t.Errorf("text upload: got %d, want 400", w.Code)
	}

	// The file extension must match the actual image format.
	if w := upload(videoID, "cover.jpg", pngData); w.Code != http.StatusBadRequest {
		t.Errorf("mismatched image extension: got %d, want 400", w.Code)
	}

	// Unsupported extensions must fail.
	for _, filename := range []string{"cover.webp", "cover.bmp", "cover"} {
		t.Run("unsupported extension "+filename, func(t *testing.T) {
			if w := upload(videoID, filename, pngData); w.Code != http.StatusBadRequest {
				t.Errorf("upload with filename %q: got %d, want 400", filename, w.Code)
			}
		})
	}

	// Images exceeding the maximum file size must fail.
	t.Run("maximum filesize", func(t *testing.T) {
		data := encodeImage(t, sharedconsts.ITypePNG, 2, 2)
		data = append(data, make([]byte, sharedconsts.ImageUploadMaxBytes-len(data)+1)...)

		if w := upload(videoID, "cover.png", data); w.Code != http.StatusBadRequest {
			t.Errorf("oversized upload: got %d, want 400", w.Code)
		}
	})

	// Images exceeding the pixel limit must fail.
	t.Run("maximum pixels", func(t *testing.T) {
		width := uint32(sharedconsts.ImageUploadMaxPixels + 1)

		// Build a PNG header with an oversized width and a valid IHDR CRC.
		data := []byte{
			0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
			0x00, 0x00, 0x00, 0x0d,
			'I', 'H', 'D', 'R',
			byte(width >> 24), byte(width >> 16), byte(width >> 8), byte(width),
			0x00, 0x00, 0x00, 0x01,
			0x08, 0x02, 0x00, 0x00, 0x00,
		}
		crc := crc32.ChecksumIEEE(data[12:])
		data = append(data,
			byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc),
		)

		if w := upload(videoID, "cover.png", data); w.Code != http.StatusBadRequest {
			t.Errorf("oversized image dimensions: got %d, want 400", w.Code)
		}
	})

	if w := upload(999999, "cover.jpg", jpgData); w.Code != http.StatusNotFound {
		t.Errorf("upload for an unknown video: got %d, want 404", w.Code)
	}

	// A thumbnail must exist, and can't be stripped at the same time.
	for name, lines := range map[string]models.OverrideLines{
		"missing file":     {Thumbnail: filepath.Join(paths.UploadedThumbnailDir, "nope.png")},
		"set and stripped": {Thumbnail: jpg, StripThumbnail: true},
	} {
		if _, err := parseOverrideLines(&lines, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	o, err := parseOverrideLines(&models.OverrideLines{Thumbnail: jpg}, nil)
	if err != nil || o.Thumbnail != jpg {
		t.Fatalf("uploaded thumbnail override = %+v, %v", o, err)
	}

	// Only thumbnails a video's overrides use are kept.
	if err := s.VideoStore().SetVideoOverrides([]int64{videoID}, o); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(paths.UploadedThumbnailDir, "424242.png")
	if err := os.WriteFile(orphan, []byte(sharedconsts.ITypePNG), 0o600); err != nil {
		t.Fatal(err)
	}
	overrides, err := s.VideoStore().ThumbnailOverrides()
	if err != nil {
		t.Fatal(err)
	}
	file.RemoveUnusedThumbnails(overrides)
	if _, err := os.Stat(jpg); err != nil {
		t.Errorf("thumbnail in use was removed: %v", err)
	}
	for _, unused := range []string{orphan, pngPath, gifPath} {
		if _, err := os.Stat(unused); !os.IsNotExist(err) {
			t.Errorf("unused thumbnail %q was kept", unused)
		}
	}

	// The thumbnail is served for the pages, which are told the video has one, and a stripped one isn't shown.
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if w := get(fmt.Sprintf("/api/v1/videos/%d/thumbnail", videoID)); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("getting the thumbnail: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	shownThumbnail := func() models.Video {
		t.Helper()
		var listed []models.AvailableVideo
		if err := json.Unmarshal(get("/api/v1/videos/available").Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}
		for _, v := range listed {
			if v.ID == videoID {
				return v.Video
			}
		}
		t.Fatalf("video %d isn't listed as available", videoID)
		return models.Video{}
	}
	if v := shownThumbnail(); !v.CustomThumbnail {
		t.Errorf("video with a custom thumbnail listed without custom_thumbnail: %+v", v)
	}
	if err := s.VideoStore().SetVideoOverrides([]int64{videoID}, &models.VideoOverrides{StripThumbnail: true}); err != nil {
		t.Fatal(err)
	}
	if v := shownThumbnail(); v.CustomThumbnail || v.Metadata.ThumbnailURL != "" {
		t.Errorf("video with a stripped thumbnail still shows one: %+v", v)
	}
	if w := get(fmt.Sprintf("/api/v1/videos/%d/thumbnail", videoID)); w.Code != http.StatusNotFound {
		t.Errorf("getting a stripped thumbnail: %d, want 404", w.Code)
	}
}

// ******************************** TLS ***********************************************************************************************

// TestSelfSignedReused checks the self-signed certificate is saved once and reused, so browser exceptions stick.
func TestSelfSignedReused(t *testing.T) {
	logger.Pl.Console = io.Discard
	path := filepath.Join(t.TempDir(), selfSignedFile)
	first, err := loadOrCreateSelfSigned(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("self-signed file mode = %v, want 0600", info.Mode().Perm())
	}
	second, err := loadOrCreateSelfSigned(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Certificate[0], second.Certificate[0]) {
		t.Error("self-signed certificate was regenerated instead of reused")
	}
	if err := first.Leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("self-signed certificate not valid for localhost: %v", err)
	}
}

// TestSelfSignedKeptAfterHostnameChange checks a certificate made under another hostname (e.g. a recreated container) is kept.
func TestSelfSignedKeptAfterHostnameChange(t *testing.T) {
	logger.Pl.Console = io.Discard
	path := filepath.Join(t.TempDir(), selfSignedFile)
	old, err := generateSelfSigned("old-container-hostname.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}

	cert, err := loadOrCreateSelfSigned(path)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, old) {
		t.Error("self-signed certificate was regenerated after a hostname change")
	}
	if cert.Leaf.VerifyHostname("old-container-hostname.invalid") != nil {
		t.Error("loaded certificate isn't the saved one")
	}
}

// TestCustomCertificateFallbackAndReload checks a combined PEM is served, missing files fall back, and changes reload.
func TestCustomCertificateFallbackAndReload(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	paths.HomeTubarrDir = dir
	certFile := filepath.Join(dir, "custom.pem")

	writeCert := func(host string, mod time.Time) []byte {
		t.Helper()
		data, err := generateSelfSigned(host)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(certFile, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(certFile, mod, mod); err != nil {
			t.Fatal(err)
		}
		return data
	}
	served := func(c *tlsCerts) []byte {
		t.Helper()
		cert, err := c.getCertificate(nil)
		if err != nil {
			t.Fatal(err)
		}
		return cert.Certificate[0]
	}

	writeCert("first.test", time.Now().Add(-time.Hour))
	if err := checkKeyPair(certFile, ""); err != nil {
		t.Fatalf("combined PEM rejected: %v", err)
	}
	c, err := newTLSCerts(certFile, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.active(); got != "custom" {
		t.Fatalf("active = %q, want custom", got)
	}
	first := served(c)

	// Changed on disk: reloaded without a restart.
	writeCert("second.test", time.Now())
	if bytes.Equal(served(c), first) {
		t.Error("changed certificate file was not reloaded")
	}

	// Missing file: falls back to self-signed.
	if err := os.Remove(certFile); err != nil {
		t.Fatal(err)
	}
	if got := c.active(); got != "self-signed" {
		t.Errorf("active after removal = %q, want self-signed", got)
	}

	// Cleared path: self-signed.
	c.setPaths("", "")
	if !bytes.Equal(served(c), c.selfSigned.Certificate[0]) {
		t.Error("cleared path did not serve the self-signed certificate")
	}
}

// ******************************** Web settings input ********************************************************************************

// The channel form and the per-URL settings the channel pages send, which share the CLI's and config files' settings
// input.

// webSettingsDir makes a folder under the test directory's wiring folder, returning its path.
func webSettingsDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(fuzzServer.dir, "wiring", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// webChannelForm returns a channel form holding only what a channel needs, as the add page sends it.
func webChannelForm(t *testing.T, name, channelURL string) url.Values {
	t.Helper()
	return url.Values{"name": {name}, "urls": {channelURL}, "video_directory": {webSettingsDir(t, "videos-"+name)}}
}

// setURLSettings sets the form's per-URL settings for channelURL, splitting fields into the settings and Metarr sections.
func setURLSettings(t *testing.T, form url.Values, channelURL string, fields map[string]any) {
	t.Helper()
	settings, metarr := map[string]any{}, map[string]any{}
	for k, v := range fields {
		if strings.HasPrefix(k, "metarr_") {
			metarr[k] = v
		} else {
			settings[k] = v
		}
	}
	data, err := json.Marshal(map[string]any{channelURL: map[string]any{"settings": settings, "metarr": metarr}})
	if err != nil {
		t.Fatal(err)
	}
	form.Set("url_settings", string(data))
}

// addWebChannel adds a channel through the API, returning its ID.
func addWebChannel(t *testing.T, form url.Values) int64 {
	t.Helper()
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code >= 300 {
		t.Fatalf("adding channel %q = %d %s", form.Get("name"), w.Code, w.Body.String())
	}
	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", form.Get("name")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", "") })
	return id
}

// TestWebInputsReachSettingsInput tests that every per-URL and Download As field the pages send reaches the settings
// input the CLI and config files use.
func TestWebInputsReachSettingsInput(t *testing.T) {
	var s settingsInput
	var m metarrArgsInput
	for _, in := range []any{&s, &m} {
		v := reflect.ValueOf(in).Elem()
		for i := range v.NumField() {
			switch f := v.Field(i); f.Kind() {
			case reflect.Pointer:
				f.Set(reflect.New(f.Type().Elem()))
			case reflect.Slice:
				f.Set(reflect.MakeSlice(f.Type(), 0, 0))
			}
		}
	}
	if got, want := len(parsing.GivenKeys(s.input())), reflect.TypeFor[settingsInput]().NumField(); got != want {
		t.Errorf("%d of the %d per-URL settings fields reach the settings input", got, want)
	}
	if got, want := len(parsing.GivenKeys(m.input())), reflect.TypeFor[metarrArgsInput]().NumField(); got != want {
		t.Errorf("%d of the %d Metarr fields reach the Metarr args input", got, want)
	}
}

// TestWebSettingsNormalized tests how the channel form and per-URL settings save values needing normalizing.
func TestWebSettingsNormalized(t *testing.T) {
	setupFuzzServer(t)
	const u = "https://example.com/@web-normalized"
	form := webChannelForm(t, "Web Normalized", u)
	for k, v := range map[string]string{
		"ytdlp_output_ext":     "MKV",
		"metarr_output_ext":    "MP4",
		"crawl_freq":           "-5",
		"download_retries":     "-1",
		"download_concurrency": "-2",
		"metarr_concurrency":   "-3",
		"metarr_max_cpu_usage": "150",
	} {
		form.Set(k, v)
	}
	setURLSettings(t, form, u, map[string]any{
		"ytdlp_output_ext":     "WEBM",
		"metarr_output_ext":    "MKV",
		"crawl_freq":           -5,
		"metarr_max_cpu_usage": 150,
	})
	id := addWebChannel(t, form)

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	for _, c := range []struct {
		what string
		read any
		want string
	}{
		{"ytdlp_output_ext", got.Settings["ytdlp_output_ext"], "mkv"},
		{"metarr_output_ext", got.Metarr["metarr_output_ext"], "mp4"},
		{"crawl_freq", got.Settings["crawl_freq"], ""},
		{"download_retries", got.Settings["download_retries"], ""},
		{"download_concurrency", got.Settings["download_concurrency"], ""},
		{"metarr_concurrency", got.Metarr["metarr_concurrency"], ""},
		{"metarr_max_cpu_usage", got.Metarr["metarr_max_cpu_usage"], "100"}, // Stored as 101, sent as the most the field holds.
		{"per-URL ytdlp_output_ext", got.URLSettings[u].Settings["ytdlp_output_ext"], "webm"},
		{"per-URL metarr_output_ext", got.URLSettings[u].Metarr["metarr_output_ext"], "mkv"},
		{"per-URL crawl_freq", got.URLSettings[u].Settings["crawl_freq"], ""},
		{"per-URL metarr_max_cpu_usage", got.URLSettings[u].Metarr["metarr_max_cpu_usage"], "100"},
	} {
		if g := formValue(c.read); g != c.want {
			t.Errorf("%s reads back as %q, want %q", c.what, g, c.want)
		}
	}
}

// TestWebSettingsRefused tests that the channel form and per-URL settings refuse each invalid value.
func TestWebSettingsRefused(t *testing.T) {
	setupFuzzServer(t)
	missing := filepath.Join(fuzzServer.dir, "wiring", "missing.txt")
	for i, c := range []struct {
		key string
		bad any
	}{
		{"video_directory", "/media/{{nope}}"},
		{"ytdlp_output_ext", "xyz"},
		{"max_filesize", "lots"},
		{"max_resolution", "huge"},
		{"from_date", "someday"},
		{"to_date", "someday"},
		{"filters", "nonsense"},
		{"move_ops", "nonsense"},
		{"filter_file", missing},
		{"move_ops_file", missing},
		{"metarr_output_ext", "xyz"},
		{"metarr_rename_style", "bogus"},
		{"metarr_gpu", "bogus"},
		{"metarr_purge_metafile", "bogus"},
		{"metarr_min_free_mem", "lots"},
		{"metarr_transcode_video_args", "av1:-crf high"},
		{"metarr_extra_ffmpeg_args", "-vf scale=1280:-2"},
		{"metarr_transcode_audio_codecs", "bogus"},
		{"metarr_meta_ops", "nonsense"},
		{"metarr_filename_ops", "nonsense"},
		{"metarr_meta_ops_file", missing},
		{"metarr_filename_ops_file", missing},
	} {
		t.Run(c.key, func(t *testing.T) {
			name := fmt.Sprintf("Web Refused %d", i)
			u := fmt.Sprintf("https://example.com/@web-refused-%d", i)

			form := webChannelForm(t, name, u)
			form.Set(c.key, fmt.Sprint(c.bad))
			if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code < 400 {
				deleteChannelNamed(t, name)
				t.Errorf("channel form saved %s %q (%d)", c.key, c.bad, w.Code)
			}

			form = webChannelForm(t, name, u)
			setURLSettings(t, form, u, map[string]any{c.key: c.bad})
			if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code < 400 {
				deleteChannelNamed(t, name)
				t.Errorf("per-URL settings saved %s %q (%d)", c.key, c.bad, w.Code)
			}
		})
	}
}

// TestWebSettingsMissingFile tests that a file going missing only stops the edit form saving if the save changes it.
func TestWebSettingsMissingFile(t *testing.T) {
	setupFuzzServer(t)
	for _, c := range []struct {
		what   string
		perURL bool
		key    string
	}{
		{"channel filter file", false, "filter_file"},
		{"channel Metarr meta ops file", false, "metarr_meta_ops_file"},
		{"per-URL filter file", true, "filter_file"},
		{"per-URL Metarr meta ops file", true, "metarr_meta_ops_file"},
	} {
		t.Run(c.what, func(t *testing.T) {
			name := "Web Missing " + c.what
			u := "https://example.com/@web-missing-" + strings.ReplaceAll(c.what, " ", "-")
			path := wiringFile(t, strings.ReplaceAll(c.what, " ", "-")+".txt", "title:set:A\n")
			if c.key == "filter_file" {
				path = wiringFile(t, strings.ReplaceAll(c.what, " ", "-")+".txt", "title:contains:a:must\n")
			}
			form := webChannelForm(t, name, u)
			if c.perURL {
				setURLSettings(t, form, u, map[string]any{c.key: path})
			} else {
				form.Set(c.key, path)
			}
			id := addWebChannel(t, form)

			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := fmt.Sprintf("/api/v1/channels/%d/update", id)
			if w := sendForm(t, http.MethodPut, target, form); w.Code >= 400 {
				t.Errorf("saving unchanged with %s missing: %d %s", c.what, w.Code, w.Body.String())
			}

			// Changed to another missing file.
			moved := path + ".moved"
			if c.perURL {
				setURLSettings(t, form, u, map[string]any{c.key: moved})
			} else {
				form.Set(c.key, moved)
			}
			if w := sendForm(t, http.MethodPut, target, form); w.Code < 400 {
				t.Errorf("saved %s changed to a missing file", c.what)
			}
		})
	}
}

// TestParseURLSettingsNumbers tests that numeric per-URL settings sent as JSON numbers are kept.
func TestParseURLSettingsNumbers(t *testing.T) {
	const raw = `{"https://example.com/c":{
		"settings":{"download_concurrency":3,"crawl_freq":45,"download_retries":2,"paused":true},
		"metarr":{"metarr_concurrency":4,"metarr_max_cpu_usage":50.5}}}`

	parsed, err := parseURLSettings(raw)
	if err != nil {
		t.Fatalf("parseURLSettings: %v", err)
	}
	got, ok := parsed["https://example.com/c"]
	if !ok || got.Settings == nil || got.Metarr == nil {
		t.Fatalf("parsed = %+v, want settings and metarr for the URL", parsed)
	}
	if s := got.Settings; s.DownloadConcurrency != 3 || s.CrawlFreq != 45 || s.Retries != 2 || !s.Paused {
		t.Errorf("settings = concurrency %d, crawl freq %d, retries %d, paused %v; want 3, 45, 2, true",
			s.DownloadConcurrency, s.CrawlFreq, s.Retries, s.Paused)
	}
	if m := got.Metarr; m.MetarrConcurrency != 4 || m.MaxCPU != 50.5 {
		t.Errorf("metarr = concurrency %d, max CPU %v; want 4, 50.5", m.MetarrConcurrency, m.MaxCPU)
	}
}

// TestSplitAuthDetails tests that a JSON array keeps values containing spaces, and the old space-separated form still works.
func TestSplitAuthDetails(t *testing.T) {
	got, err := splitAuthDetails(`[{"username":"a b","password":"correct horse"},{"username":"c"}]`)
	if err != nil || len(got) != 2 || got[0] != `{"username":"a b","password":"correct horse"}` {
		t.Fatalf("splitAuthDetails(array) = %q, %v", got, err)
	}
	if got, err := splitAuthDetails(`{"username":"a"} {"username":"b"}`); err != nil || len(got) != 2 {
		t.Fatalf("splitAuthDetails(legacy) = %q, %v", got, err)
	}
}

// TestParseURLSettingsRejectsBadType tests that a wrongly typed value is an error rather than silently dropped.
func TestParseURLSettingsRejectsBadType(t *testing.T) {
	if _, err := parseURLSettings(`{"https://example.com/c":{"settings":{"download_concurrency":"three"}}}`); err == nil {
		t.Fatal("parseURLSettings accepted a string concurrency, want an error")
	}
}

// ******************************** Write NFO *****************************************************************************************

// TestWriteNFOChannelChoices tests the channel form's three choices: blank saves unset so a profile's Yes applies, and an
// explicit No is kept (not dropped as if blank), so it beats the profile's Yes.
func TestWriteNFOChannelChoices(t *testing.T) {
	setupFuzzServer(t)
	const profile = "NFO Writer"
	if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", url.Values{"name": {profile}, "metarr_write_nfo": {"true"}}); w.Code != http.StatusCreated {
		t.Fatalf("adding profile = %d %s", w.Code, w.Body.String())
	}
	var profileID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", profile).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
	})

	for _, tc := range []struct {
		name     string
		choice   string
		saved    any // As the edit page reads it.
		inEffect any // As list channels reads it.
	}{
		{"default", "", nil, true},
		{"yes", "true", true, true},
		{"no", "false", false, false},
	} {
		channelURL := "https://example.com/@nfo-" + tc.name
		form := webChannelForm(t, "NFO "+tc.name, channelURL)
		form.Set("profile_id", fmt.Sprint(profileID))
		if tc.choice != "" {
			form.Set("metarr_write_nfo", tc.choice)
		}
		id := addWebChannel(t, form)

		var details channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &details)
		if got := details.Metarr["metarr_write_nfo"]; got != tc.saved {
			t.Errorf("%s: saved as %v, want %v", tc.name, got, tc.saved)
		}

		type listedChannel struct {
			ID     int64          `json:"id"`
			Metarr map[string]any `json:"metarr"`
		}
		var channels []listedChannel
		fetchJSON(t, "/api/v1/channels/all", &channels)
		i := slices.IndexFunc(channels, func(c listedChannel) bool { return c.ID == id })
		if i < 0 {
			t.Fatalf("%s: channel %d isn't listed", tc.name, id)
		}
		if got := channels[i].Metarr["metarr_write_nfo"]; got != tc.inEffect {
			t.Errorf("%s: listed as %v, want %v", tc.name, got, tc.inEffect)
		}
	}
}

// TestWriteNFOURLChoices tests a URL's explicit No is kept, as a boolean in its own settings.
func TestWriteNFOURLChoices(t *testing.T) {
	setupFuzzServer(t)
	const channelURL = "https://example.com/@nfo-url"
	form := webChannelForm(t, "NFO URL", channelURL)
	form.Set("metarr_write_nfo", "true")
	setURLSettings(t, form, channelURL, map[string]any{"metarr_write_nfo": false})
	id := addWebChannel(t, form)

	var details channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &details)
	if got := details.URLSettings[channelURL].Metarr["metarr_write_nfo"]; got != false {
		t.Errorf("URL's No saved as %v", got)
	}
}

// TestWriteNFODownloadAs tests Download As reads a saved choice back, No included, with the channel URL's as the default.
func TestWriteNFODownloadAs(t *testing.T) {
	setupFuzzServer(t)
	const channelURL = "https://example.com/@nfo-download-as"
	form := webChannelForm(t, "NFO Download As", channelURL)
	form.Set("metarr_write_nfo", "true")
	chanID := addWebChannel(t, form)
	var cuID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = ? AND url = ?", chanID, channelURL).Scan(&cuID); err != nil {
		t.Fatal(err)
	}

	vs := repo.GetVideoStore(fuzzServer.db)
	const videoURL = "https://example.com/v/nfo-download-as"
	if _, err := vs.AddAvailableVideos([]*models.Video{{ChannelURLID: cuID, ChannelURL: channelURL, URL: videoURL, Metadata: models.VideoMetadata{Title: "NFO"}}}, chanID); err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM videos WHERE url = ?", videoURL).Scan(&videoID); err != nil {
		t.Fatal(err)
	}

	// The dialog's body decodes to a parsed override, as the handlers read it.
	var input metarrArgsInput
	if err := json.Unmarshal([]byte(`{"metarr_write_nfo":false}`), &input); err != nil {
		t.Fatal(err)
	}
	overrides, err := parseOverrideLines(&models.OverrideLines{}, &input)
	if err != nil {
		t.Fatal(err)
	}
	if err := vs.SetVideoOverrides([]int64{videoID}, overrides); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Defaults map[string]any `json:"defaults_metarr"`
		Saved    map[string]any `json:"saved_metarr"`
	}
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videoID), &got)
	if got.Defaults["metarr_write_nfo"] != true {
		t.Errorf("default = %v, want the channel URL's Yes", got.Defaults["metarr_write_nfo"])
	}
	if got.Saved["metarr_write_nfo"] != false {
		t.Errorf("saved = %v, want the video's No", got.Saved["metarr_write_nfo"])
	}
}

// TestDownloadAsMetarrFields tests the Download As dialog's Metarr fields are all reloaded by metarrForm and read by the
// server, and that its Yes/No fields, which select strings, are sent as the booleans the server reads.
func TestDownloadAsMetarrFields(t *testing.T) {
	src := readWebFile(t, "partials/download-as.html")
	m := regexp.MustCompile(`metarrKeys: \[([^\]]*)\]`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("download-as.html has no metarrKeys")
	}
	var reloaded []string
	for _, s := range regexp.MustCompile(`'(\w+)'`).FindAllStringSubmatch(m[1], -1) {
		reloaded = append(reloaded, s[1])
	}

	bound := boundKeys(bindings(src), "form.metarr.")
	boundNames := make([]string, 0, len(bound))
	for k := range bound {
		boundNames = append(boundNames, k)
	}
	reportDiff(t, "download-as.html", "fields", boundNames, "metarrKeys", reloaded)

	// The boxes filled with the channel URL's must be fields the dialog reloads.
	boxes := regexp.MustCompile(`metarrBoxes: \[([^\]]*)\]`).FindStringSubmatch(src)
	if boxes == nil {
		t.Fatal("download-as.html has no metarrBoxes")
	}
	for _, s := range regexp.MustCompile(`'(\w+)'`).FindAllStringSubmatch(boxes[1], -1) {
		if !slices.Contains(reloaded, s[1]) {
			t.Errorf("download-as.html: metarrBoxes has %q, which isn't in metarrKeys", s[1])
		}
	}

	kinds := fieldKinds(metarrArgsInput{})
	for _, k := range boundNames {
		kind, ok := kinds[k]
		if !ok {
			t.Errorf("download-as.html: field %q isn't read by the server", k)
			continue
		}
		sent := regexp.MustCompile(`metarr\.` + k + ` = metarr\.` + k + ` === 'true'`)
		if kind.String() == "bool" && !sent.MatchString(src) {
			t.Errorf("download-as.html: Yes/No field %q isn't sent as a boolean", k)
		}
	}
}
