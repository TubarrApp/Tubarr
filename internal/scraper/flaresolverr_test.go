package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/siterules"

	"github.com/spf13/viper"
)

const testFlareSolverrUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// lastSolvedURL and lastMaxTimeout are the URL and time limit (ms) of the most recent fakeFlareSolverr solve.
var (
	lastSolvedURL  atomic.Value
	lastMaxTimeout atomic.Int64
)

// echoRequests counts fakeFlareSolverr's header echo page loads.
var echoRequests atomic.Int32

// testEchoHeaders are the headers fakeFlareSolverr's "browser" sends, as the header echo page shows them.
var testEchoHeaders = []string{
	":method: GET", ":authority: tls.peet.ws", ":scheme: https", ":path: /api/all",
	`sec-ch-ua: "Not?A_Brand";v="24", "Chromium";v="131"`, "sec-ch-ua-mobile: ?0", `sec-ch-ua-platform: "Linux"`,
	"upgrade-insecure-requests: 1", "user-agent: " + testFlareSolverrUA, "accept: text/html,*/*;q=0.8",
	"sec-fetch-site: none", "sec-fetch-mode: navigate", "accept-language: C.UTF-8,C.UTF;q=0.9", "priority: u=0, i",
}

// testEchoPage returns the header echo page as a browser renders it: its JSON, HTML-escaped inside a <pre>.
func testEchoPage() string {
	echo, _ := json.Marshal(map[string]any{"http2": map[string]any{"sent_frames": []map[string]any{
		{"frame_type": "SETTINGS"},
		{"frame_type": "HEADERS", "headers": testEchoHeaders},
	}}})
	return "<html><head></head><body><pre>" + html.EscapeString(string(echo)) + "</pre></body></html>"
}

// fakeFlareSolverr starts a server answering FlareSolverr API requests. Each solve returns a cf_clearance cookie
// numbered by solve count ("1", "2", ...), and the number of solves so far is kept in solves.
func fakeFlareSolverr(t *testing.T, solves *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req flareSolverrRequest
		if r.Method != http.MethodPost || r.URL.Path != "/v1" || json.NewDecoder(r.Body).Decode(&req) != nil || req.Cmd != "request.get" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if req.URL == flareSolverrHeaderEchoURL {
			echoRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "solution": map[string]any{"status": 200, "userAgent": testFlareSolverrUA, "response": testEchoPage()}})
			return
		}
		lastSolvedURL.Store(req.URL)
		lastMaxTimeout.Store(req.MaxTimeout)
		n := solves.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"solution": map[string]any{
				"status":    200,
				"userAgent": testFlareSolverrUA,
				"cookies":   []map[string]any{{"name": "cf_clearance", "value": fmt.Sprint(n), "path": "/", "expires": -1}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeCloudflareSite starts a site that returns a Cloudflare challenge unless the request has FlareSolverr's
// user agent and a cf_clearance cookie of at least minClearance.
func fakeCloudflareSite(t *testing.T, minClearance int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("cf_clearance")
		var clearance int
		if err == nil {
			_, _ = fmt.Sscan(c.Value, &clearance)
		}
		if r.UserAgent() != testFlareSolverrUA || clearance < minClearance {
			w.Header().Set("Cf-Mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
			return
		}
		_, _ = w.Write([]byte(`<html><body><a href="/video/1">One</a><a href="/about">About</a></body></html>`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// flareSolverrQuery returns a FlareSolverr site query with a link crawl rule.
func flareSolverrQuery() models.SiteRules {
	return models.SiteRules{
		Site:         "cf.example",
		FlareSolverr: true,
		Crawl:        &models.CrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile("/video/")},
	}
}

// useFlareSolverr points the FlareSolverr URL setting at srv for the duration of a test.
func useFlareSolverr(t *testing.T, srv *httptest.Server) {
	t.Helper()
	viper.Set(keys.FlareSolverrURL, srv.URL)
	t.Cleanup(func() { viper.Set(keys.FlareSolverrURL, "") })
}

// TestCrawlWithRuleFlareSolverr tests that a FlareSolverr site is crawled with the solved cookies and user agent.
func TestCrawlWithRuleFlareSolverr(t *testing.T) {
	var solves atomic.Int32
	useFlareSolverr(t, fakeFlareSolverr(t, &solves))
	site := fakeCloudflareSite(t, 1)

	got, err := New().crawlWithRule(site.URL+"/channel", nil, flareSolverrQuery())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[site.URL+"/video/1"]; !ok || len(got) != 1 {
		t.Errorf("got %v, want only %s/video/1", got, site.URL)
	}
	if solves.Load() != 1 {
		t.Errorf("expected 1 FlareSolverr solve, got %d", solves.Load())
	}
}

// TestCrawlWithRuleFlareSolverrResolve tests that a Cloudflare challenge triggers one fresh solve and retry.
func TestCrawlWithRuleFlareSolverrResolve(t *testing.T) {
	var solves atomic.Int32
	useFlareSolverr(t, fakeFlareSolverr(t, &solves))

	// The site only accepts the second solve's cookie, so the first attempt is challenged.
	site := fakeCloudflareSite(t, 2)
	got, err := New().crawlWithRule(site.URL+"/channel", nil, flareSolverrQuery())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || solves.Load() != 2 {
		t.Errorf("got %d URLs after %d solves, want 1 URL after 2 solves", len(got), solves.Load())
	}

	// A site that keeps challenging fails after one retry, rather than solving forever.
	solves.Store(0)
	site = fakeCloudflareSite(t, 100)
	if _, err := New().crawlWithRule(site.URL+"/channel", nil, flareSolverrQuery()); err == nil || !strings.Contains(err.Error(), "again after a fresh FlareSolverr solve") {
		t.Errorf("expected a still-challenged error, got %v", err)
	}
	if solves.Load() != 2 {
		t.Errorf("expected 2 FlareSolverr solves, got %d", solves.Load())
	}
}

// fakePlainSite starts a site with no Cloudflare protection.
func fakePlainSite(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="/video/1">One</a></body></html>`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeFailingFlareSolverr starts a FlareSolverr that succeeds for the first okSolves solves, then fails,
// counting all requests in calls.
func fakeFailingFlareSolverr(t *testing.T, okSolves int32, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		resp := map[string]any{"status": "error", "message": "Error solving the challenge."}
		if n <= okSolves {
			resp = map[string]any{"status": "ok", "solution": map[string]any{
				"status":    200,
				"userAgent": testFlareSolverrUA,
				"cookies":   []map[string]any{{"name": "cf_clearance", "value": fmt.Sprint(n), "path": "/"}},
			}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCrawlWithRuleFlareSolverrFallback tests falling back to normal requests when FlareSolverr can't be used.
func TestCrawlWithRuleFlareSolverrFallback(t *testing.T) {
	plain := fakePlainSite(t)

	// No FlareSolverr URL: crawls the site with normal requests.
	viper.Set(keys.FlareSolverrURL, "")
	got, err := New().crawlWithRule(plain.URL+"/channel", nil, flareSolverrQuery())
	if err != nil || len(got) != 1 {
		t.Fatalf("expected a fallback crawl to find 1 URL, got %v, %v", got, err)
	}

	// If Cloudflare then challenges, the error says why FlareSolverr wasn't used.
	site := fakeCloudflareSite(t, 1)
	_, err = New().crawlWithRule(site.URL+"/channel", nil, flareSolverrQuery())
	if err == nil || !strings.Contains(err.Error(), "FlareSolverr could not be used") || !strings.Contains(err.Error(), "no FlareSolverr URL") {
		t.Errorf("expected an error explaining FlareSolverr wasn't used, got %v", err)
	}

	// A failing FlareSolverr is only asked once per crawl session.
	var calls atomic.Int32
	useFlareSolverr(t, fakeFailingFlareSolverr(t, 0, &calls))
	s := New()
	for range 3 {
		if _, err := s.crawlWithRule(plain.URL+"/channel", nil, flareSolverrQuery()); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("expected FlareSolverr to be asked once, got %d", calls.Load())
	}
}

// TestCrawlWithRuleFlareSolverrResolveFails tests the error when a fresh solve after a challenge fails.
func TestCrawlWithRuleFlareSolverrResolveFails(t *testing.T) {
	var calls atomic.Int32
	useFlareSolverr(t, fakeFailingFlareSolverr(t, 1, &calls))

	// The site only accepts a second solve, which fails.
	site := fakeCloudflareSite(t, 2)
	_, err := New().crawlWithRule(site.URL+"/channel", nil, flareSolverrQuery())
	if err == nil || !strings.Contains(err.Error(), "fresh FlareSolverr solve failed") || !strings.Contains(err.Error(), "Error solving the challenge") {
		t.Errorf("expected a failed fresh solve error, got %v", err)
	}
}

// TestFlareSolverrSolutionCache tests that solutions are cached per hostname, and only re-solved when stale.
func TestFlareSolverrSolutionCache(t *testing.T) {
	var solves atomic.Int32
	useFlareSolverr(t, fakeFlareSolverr(t, &solves))
	cm := NewCookieManager()
	ctx := context.Background()

	first, err := cm.flareSolverrSolution(ctx, "https://cf.example/a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := lastSolvedURL.Load(); got != "https://cf.example/" {
		t.Errorf("expected the site's homepage to be solved, got %v", got)
	}
	if again, _ := cm.flareSolverrSolution(ctx, "https://cf.example/b", 0); again != first {
		t.Error("expected the cached solution for the same hostname")
	}
	fresh, err := cm.flareSolverrSolution(ctx, "https://cf.example/a", first.gen)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.gen <= first.gen || fresh.cookies[0].Value != "2" {
		t.Errorf("expected a newer solution, got gen %d (was %d) with cookie %q", fresh.gen, first.gen, fresh.cookies[0].Value)
	}
	if again, _ := cm.flareSolverrSolution(ctx, "https://cf.example/a", first.gen); again != fresh {
		t.Error("expected a stale request after a refresh to reuse the refreshed solution")
	}
	if solves.Load() != 2 {
		t.Errorf("expected 2 FlareSolverr solves, got %d", solves.Load())
	}
}

// TestFlareSolverrEndpoint tests FlareSolverr base URL validation and endpoint building.
func TestFlareSolverrEndpoint(t *testing.T) {
	valid := map[string]string{
		"http://localhost:8191":      "http://localhost:8191/v1",
		" http://localhost:8191/ ":   "http://localhost:8191/v1",
		"https://fs.example.com/v1":  "https://fs.example.com/v1",
		"http://10.0.0.5:8191/proxy": "http://10.0.0.5:8191/proxy/v1",
	}
	for in, want := range valid {
		if got, err := flareSolverrEndpoint(in); err != nil || got != want {
			t.Errorf("flareSolverrEndpoint(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"localhost:8191", "ftp://localhost", "http://", "not a url"} {
		if _, err := flareSolverrEndpoint(in); err == nil {
			t.Errorf("flareSolverrEndpoint(%q): expected an error", in)
		}
	}
}

// TestClientProfileForUserAgent tests that impersonation picks the closest profile at or below a user agent's version.
func TestClientProfileForUserAgent(t *testing.T) {
	if got := userAgentMajorVersion(testFlareSolverrUA); got != 131 {
		t.Fatalf("userAgentMajorVersion = %d, want 131", got)
	}
	_, newest := clientProfile(consts.ImpersonateChrome, 0)
	_, matched := clientProfile(consts.ImpersonateChrome, 131)
	if len(newest) == 0 || len(matched) == 0 {
		t.Fatal("expected Chrome TLS profiles")
	}
	if matched[0] > 131 && matched[0] != newest[0] {
		t.Errorf("matched profile version %v is above 131", matched)
	}
	if newest[0] > 131 && matched[0] > 131 {
		t.Logf("no Chrome profile at or below 131; using the oldest (%v)", matched)
	}
}

// TestFlareSolverrSiteTimeout tests that a site's flaresolverr_timeout is passed to FlareSolverr, with the default otherwise.
func TestFlareSolverrSiteTimeout(t *testing.T) {
	var solves atomic.Int32
	useFlareSolverr(t, fakeFlareSolverr(t, &solves))
	t.Cleanup(siterules.Reset)
	siterules.Reset()
	siterules.Register([]models.SiteScraper{{Domain: "slow.example", FlareSolverr: true, FlareSolverrTimeout: 120 * time.Second}})

	cm := NewCookieManager()
	if _, err := cm.flareSolverrSolution(context.Background(), "https://slow.example/channel", 0); err != nil {
		t.Fatal(err)
	}
	if got := lastMaxTimeout.Load(); got != 120000 {
		t.Errorf("site with a 120s timeout: FlareSolverr got maxTimeout %d, want 120000", got)
	}

	if _, err := cm.flareSolverrSolution(context.Background(), "https://other.example/channel", 0); err != nil {
		t.Fatal(err)
	}
	if got, want := lastMaxTimeout.Load(), consts.FlareSolverrDefaultTimeout.Milliseconds(); got != want {
		t.Errorf("site with no timeout: FlareSolverr got maxTimeout %d, want %d", got, want)
	}
}

// TestParseHeaderEcho tests reading the headers a browser sent from the header echo page, in order.
func TestParseHeaderEcho(t *testing.T) {
	got, err := parseHeaderEcho(testEchoPage())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(testEchoHeaders)-4 || got[0] != [2]string{"sec-ch-ua", `"Not?A_Brand";v="24", "Chromium";v="131"`} {
		t.Errorf("expected the headers without pseudo-headers, in order, got %v", got)
	}

	// HTTP/1.1 echoes, and the host and cookies (which differ per request), are handled too.
	h1, err := parseHeaderEcho(`{"http1": {"headers": ["Host: tls.peet.ws", "User-Agent: x", "Cookie: a=b", "Accept-Language: en"]}}`)
	if err != nil || len(h1) != 2 || h1[0] != [2]string{"user-agent", "x"} || h1[1] != [2]string{"accept-language", "en"} {
		t.Errorf("HTTP/1.1 echo: got %v, %v", h1, err)
	}

	if _, err := parseHeaderEcho("<html><body>Blocked</body></html>"); err == nil {
		t.Error("expected an error for a page with no echoed headers")
	}
}

// TestFlareSolverrCopiesBrowserHeaders tests that requests for FlareSolverr sites send the headers FlareSolverr's
// browser sends, copied once per crawl session.
func TestFlareSolverrCopiesBrowserHeaders(t *testing.T) {
	var solves atomic.Int32
	echoRequests.Store(0)
	useFlareSolverr(t, fakeFlareSolverr(t, &solves))

	var got http.Header
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`<a href="/video/1">One</a>`))
	}))
	defer site.Close()

	s := New()
	for range 2 {
		if _, err := s.crawlWithRule(site.URL+"/channel", nil, flareSolverrQuery()); err != nil {
			t.Fatal(err)
		}
	}
	if got.Get("Accept-Language") != "C.UTF-8,C.UTF;q=0.9" || got.Get("Sec-Ch-Ua") != `"Not?A_Brand";v="24", "Chromium";v="131"` {
		t.Errorf("expected FlareSolverr's browser headers, got Accept-Language %q, Sec-Ch-Ua %q", got.Get("Accept-Language"), got.Get("Sec-Ch-Ua"))
	}
	if !strings.Contains(got.Get("Cookie"), "cf_clearance=") {
		t.Errorf("expected FlareSolverr's cookies to be kept, got %q", got.Get("Cookie"))
	}
	if echoRequests.Load() != 1 {
		t.Errorf("expected the headers to be copied once, got %d echo requests", echoRequests.Load())
	}
}

// TestFlareSolverrNewerSolveForDownloads tests that a channel URL's solution switches to a newer solve made by Tubarr's
// own requests, rewriting the cookie file for yt-dlp, without asking FlareSolverr again.
func TestFlareSolverrNewerSolveForDownloads(t *testing.T) {
	var solves atomic.Int32
	useFlareSolverr(t, fakeFlareSolverr(t, &solves))
	cm := NewCookieManager()
	ctx := context.Background()
	cu := &models.ChannelURL{URL: "https://cf.example/channel", CookiePath: filepath.Join(t.TempDir(), "cookies.txt")}

	first, err := cm.flareSolverrSolution(ctx, cu.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	fs := models.NewFlareSolverrSolution(first.ytDLPSolve(), cm.flareSolverrResolver(cu, nil), cm.flareSolverrNewer(cu, nil))
	if got := fs.Current(); got.Gen != first.gen {
		t.Errorf("expected the first solve with nothing newer, got gen %d", got.Gen)
	}

	// Tubarr's own request is challenged and solves again.
	if _, err := cm.flareSolverrSolution(ctx, cu.URL, first.gen); err != nil {
		t.Fatal(err)
	}
	if got := fs.Current(); got.Gen <= first.gen {
		t.Errorf("expected the solution to switch to the newer solve, got gen %d (first was %d)", got.Gen, first.gen)
	}
	if cookies, _ := os.ReadFile(cu.CookiePath); !strings.Contains(string(cookies), "cf_clearance\t2") {
		t.Errorf("expected the cookie file to have the newer solve's cookie, got:\n%s", cookies)
	}
	if solves.Load() != 2 {
		t.Errorf("expected 2 FlareSolverr solves (none from switching), got %d", solves.Load())
	}
}

// TestCrawlWithRuleFlareSolverrResolveWithChannelCookies tests that a refreshed solve's cookies aren't replaced by the
// channel URL's older ones (which hold the first solve's cookies) when retrying a channel crawl.
func TestCrawlWithRuleFlareSolverrResolveWithChannelCookies(t *testing.T) {
	var solves atomic.Int32
	useFlareSolverr(t, fakeFlareSolverr(t, &solves))
	site := fakeCloudflareSite(t, 2) // Only accepts the second solve's cookie.

	s := New()
	first, err := s.cookieManager.flareSolverrSolution(context.Background(), site.URL+"/channel", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.crawlWithRule(site.URL+"/channel", first.cookies, flareSolverrQuery()); err != nil {
		t.Errorf("expected the retry to use the refreshed solve's cookies, got %v", err)
	}
	if solves.Load() != 2 {
		t.Errorf("expected 2 FlareSolverr solves, got %d", solves.Load())
	}
}
