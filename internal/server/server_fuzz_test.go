package server

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/repo"

	"github.com/go-chi/chi/v5/middleware"
)

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
	{http.MethodDelete, "/api/v1/channels/{p}/cancel-download/{p}"},
	{http.MethodPost, "/api/v1/channels/{p}/notification-seen"},
	{http.MethodPost, "/api/v1/channels/{p}/toggle-pause"},
	{http.MethodGet, "/api/v1/downloads"},
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

// testDir is the working directory for this package's tests, holding the test database and any directories requests
// create (directory settings are created when validated). Removed when the tests finish.
var testDir string

// TestMain runs the package's tests from a temporary directory, so nothing is created in the source tree.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tubarr-server-test")
	if err != nil {
		log.Fatal(err)
	}
	testDir = dir
	if err := os.Chdir(dir); err != nil {
		log.Fatal(err)
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
		ss := serverStore{
			s: store, cs: store.ChannelStore(), ds: store.DownloadStore(), vs: store.VideoStore(),
			tss: store.TubarrSettingsStore(), db: dbase, ctx: ctx, cancel: cancel,
		}
		if fuzzServer.handler, err = NewRouter(ss); err != nil {
			fuzzServer.initErr = err
			return
		}
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

// TestRejectedAddCreatesNoDirectories tests that a channel's directories are only created once it's accepted.
func TestRejectedAddCreatesNoDirectories(t *testing.T) {
	setupFuzzServer(t)
	videoDir := filepath.Join(fuzzServer.dir, "new-videos")
	form := url.Values{
		"name":                           {"Dir Test"},
		"urls":                           {"https://example.com/@dirtest"},
		jsonkeys.SettingsVideoDirectory:  {videoDir},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.MetarrRenameStyle:       {"not-a-style"}, // Rejected after the directories are checked.
	}
	add := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w.Code
	}

	if code := add(); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid rename style, got %d", code)
	}
	if _, err := os.Stat(videoDir); !os.IsNotExist(err) {
		t.Errorf("rejected add created video directory %q (stat error: %v)", videoDir, err)
	}

	form.Del(jsonkeys.MetarrRenameStyle)
	if code := add(); code >= 300 {
		t.Fatalf("expected the corrected add to succeed, got %d", code)
	}
	if info, err := os.Stat(videoDir); err != nil || !info.IsDir() {
		t.Errorf("accepted add didn't create video directory %q: %v", videoDir, err)
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
		f.Add(uint8(i), "1", "name=x&urls=https%3A%2F%2Fa.example&video_directory=videos&crawl_freq=abc&max_concurrency=-5")
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

		// Directory settings are created when validated, so only allow paths inside the test's working directory.
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
