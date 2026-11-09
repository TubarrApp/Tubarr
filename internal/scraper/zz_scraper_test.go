package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/siterules"

	"github.com/TubarrApp/gocommon/sharedtags"
	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/gocolly/colly"
	"github.com/spf13/viper"
)

// ******************************** Crawl *********************************************************************************************

// TestMain sets up the test environment for the scraper package.
//
// Will not throw page errors if deleted but will throw errors on the tests if not present as "logger" is used in the scraper package and needs to be initialized.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// crawlTestPage is a sample HTML page used for testing the crawlWithRule function.
const crawlTestPage = `<html><body>
<a href="/video/abc?ref=home">One</a>
<a href="/video/def">Two</a>
<a href="/about">About</a>
<rum-videos-grid><script type="application/json">
{"items":[
  {"object_type":"video","url":"https://rumble.com/v111-one.html?e9s=src"},
  {"object_type":"channel","url":"https://rumble.com/c/Other"},
  {"object_type":"video","url":"https://rumble.com/v222-two.html"}
]}
</script></rum-videos-grid>
</body></html>`

// TestCrawlWithRule tests the crawlWithRule function with various crawl rules and verifies the extracted URLs.
func TestCrawlWithRule(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(crawlTestPage))
	}))
	defer srv.Close()

	tests := []struct {
		name string
		rule models.CrawlRule
		want []string
	}{
		{
			name: "links",
			rule: models.CrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile("/video/"), StripQuery: true},
			want: []string{srv.URL + "/video/abc", srv.URL + "/video/def"},
		},
		{
			name: "links exclude",
			rule: models.CrawlRule{Selector: "a[href]", Attr: "href", Exclude: regexp.MustCompile("/about|abc")},
			want: []string{srv.URL + "/video/def"},
		},
		{
			name: "json",
			rule: models.CrawlRule{
				Selector:   `rum-videos-grid script[type="application/json"]`,
				JSONPath:   `items.#(object_type=="video")#.url`,
				Include:    regexp.MustCompile(`^https://rumble\.com/v[^/]+\.html$`),
				StripQuery: true,
			},
			want: []string{"https://rumble.com/v111-one.html", "https://rumble.com/v222-two.html"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := models.SiteRules{Site: "test", Crawl: &tt.rule}
			got, err := New().crawlWithRule(t.Context(), srv.URL+"/channel", nil, query)
			if err != nil {
				t.Fatal(err)
			}
			keys := make([]string, 0, len(got))
			for k := range got {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, tt.want) {
				t.Errorf("got %v, want %v", keys, tt.want)
			}
		})
	}
}

// TestCrawlWithRuleRequestError tests that crawlWithRule returns an error when the channel page request fails.
func TestCrawlWithRuleRequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	query := models.SiteRules{Site: "test", Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href"}}
	if _, err := New().crawlWithRule(t.Context(), srv.URL, nil, query); err == nil {
		t.Error("expected an error for a 403 channel page")
	}
}

// TestCrawlWithRuleCanceled tests that cancelling the context ends a channel page request still waiting on the server.
func TestCrawlWithRuleCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	query := models.SiteRules{Site: "test", Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href"}}
	if _, err := New().crawlWithRule(ctx, srv.URL, nil, query); err == nil {
		t.Error("expected an error for a canceled request")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("request ran for %v after its context was canceled", elapsed)
	}
}

// TestCrawlWithRuleNilRule tests that crawlWithRule returns an error rather than panicking when a site has no crawl rule.
func TestCrawlWithRuleNilRule(t *testing.T) {
	if _, err := New().crawlWithRule(t.Context(), "https://example.com", nil, models.SiteRules{Site: "example.com"}); err == nil {
		t.Error("expected an error for a site with no crawl rule")
	}
}

// TestCrawlWithRuleUserAgent tests that a site's user agent is sent, with and without impersonation.
func TestCrawlWithRuleUserAgent(t *testing.T) {
	const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != userAgent {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`<a href="/video/1">One</a>`))
	}))
	defer srv.Close()

	for _, impersonate := range []string{"", "chrome"} {
		query := models.SiteRules{Site: "test", Impersonate: impersonate, UserAgent: userAgent, Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href"}}
		got, err := New().crawlWithRule(t.Context(), srv.URL, nil, query)
		if err != nil || len(got) != 1 {
			t.Errorf("impersonate %q: expected the site's user agent to be sent, got %v, %v", impersonate, got, err)
		}
	}
}

// previewTestPage has censored.tv style episode cards, and a Rumble style JSON grid.
const previewTestPage = `<html><body>
<div class="episode">
  <a class="episode-link" href="/episodes/one"><img data-src="/thumbs/one.jpg"><span class="duration">00:47:35</span><h5>Episode &amp; One</h5></a>
  <time>Oct 15th, 2026</time>
</div>
<div class="episode">
  <a href="/episodes/two"><img src="https://cdn.example.com/two.jpg"></a>
  <a href="/episodes/two"><h5>Episode Two</h5></a>
</div>
<a href="/episodes/three">Outside a card</a>
<rum-videos-grid><script type="application/json">
{"items":[
  {"object_type":"video","url":"https://rumble.com/v111-one.html","title":"Rumble One","thumb":"https://cdn.example.com/r1.jpg","duration":5187,"upload_date":"2026-10-15T12:08:05+00:00"},
  {"object_type":"channel","url":"https://rumble.com/c/Other","title":"Channel"},
  {"object_type":"video","url":"https://rumble.com/v222-two.html"}
]}
</script></rum-videos-grid>
</body></html>`

// TestCrawlWithRulePreviewFields tests preview fields read from HTML cards (item) and JSON objects (item_path).
func TestCrawlWithRulePreviewFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(previewTestPage))
	}))
	defer srv.Close()

	html := models.CrawlRule{
		Selector: "a[href]", Attr: "href", Include: regexp.MustCompile("/episodes/"), Item: ".episode",
		PreviewFields: []models.MetadataRule{
			{Name: sharedtags.JTitle, Selector: "h5"},
			{Name: sharedtags.JThumbnail, Selector: "img[data-src]", Attr: "data-src"},
			{Name: sharedtags.JThumbnail, Selector: "img[src]", Attr: "src"},
			{Name: sharedtags.JDurationString, Selector: ".duration"},
			{Name: sharedtags.JUploadDate, Selector: "time"},
		},
	}
	got, err := New().crawlWithRule(t.Context(), srv.URL, nil, models.SiteRules{Site: "test", Crawl: &html})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]listedVideo{
		srv.URL + "/episodes/one":   {Title: "Episode & One", ThumbnailURL: srv.URL + "/thumbs/one.jpg", Duration: 47*60 + 35, UploadDate: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)},
		srv.URL + "/episodes/two":   {Title: "Episode Two", ThumbnailURL: "https://cdn.example.com/two.jpg"}, // Merged from both links.
		srv.URL + "/episodes/three": {},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d videos, want %d: %v", len(got), len(want), got)
	}
	for u, w := range want {
		if g := got[u]; g == nil || g.Title != w.Title || g.ThumbnailURL != w.ThumbnailURL || g.Duration != w.Duration || !g.UploadDate.Equal(w.UploadDate) {
			t.Errorf("%s: got %+v, want %+v", u, got[u], w)
		}
	}

	json := models.CrawlRule{
		Selector: `rum-videos-grid script[type="application/json"]`, ItemPath: `items.#(object_type=="video")#`, JSONPath: "url",
		PreviewFields: []models.MetadataRule{
			{Name: sharedtags.JTitle, JSONPath: "title"},
			{Name: sharedtags.JThumbnail, JSONPath: "thumb"},
			{Name: sharedtags.JDuration, JSONPath: "duration"},
			{Name: sharedtags.JUploadDate, JSONPath: "upload_date"},
		},
	}
	got, err = New().crawlWithRule(t.Context(), srv.URL, nil, models.SiteRules{Site: "test", Crawl: &json})
	if err != nil {
		t.Fatal(err)
	}
	one := got["https://rumble.com/v111-one.html"]
	if len(got) != 2 || one == nil || one.Title != "Rumble One" || one.ThumbnailURL != "https://cdn.example.com/r1.jpg" || one.Duration != 5187 || one.UploadDate.Format("2006-01-02") != "2026-10-15" {
		t.Errorf("json: got %d videos, video one %+v", len(got), one)
	}
	if two := got["https://rumble.com/v222-two.html"]; two == nil || two.Title != "" {
		t.Errorf("json: expected video two with no details, got %+v", two)
	}
}

// TestSetListedFieldDuration checks duration wins over duration_string in either order, and the first duration is kept.
func TestSetListedFieldDuration(t *testing.T) {
	type field struct{ name, value string }
	tests := []struct {
		fields []field
		want   int
	}{
		{[]field{{sharedtags.JDurationString, "1:00"}}, 60},
		{[]field{{sharedtags.JDuration, "90"}}, 90},
		{[]field{{sharedtags.JDurationString, "1:00"}, {sharedtags.JDuration, "90"}}, 90},
		{[]field{{sharedtags.JDuration, "90"}, {sharedtags.JDurationString, "1:00"}}, 90},
		{[]field{{sharedtags.JDuration, "90"}, {sharedtags.JDuration, "120"}}, 90},
		{[]field{{sharedtags.JDuration, "abc"}, {sharedtags.JDurationString, "1:00"}}, 60},
	}
	for _, tt := range tests {
		lv := &listedVideo{}
		for _, f := range tt.fields {
			setListedField(lv, f.name, f.value, nil)
		}
		if lv.Duration != tt.want {
			t.Errorf("%v: got duration %d, want %d", tt.fields, lv.Duration, tt.want)
		}
	}
}

// ******************************** Auth **********************************************************************************************

// TestLoginCookieDomain tests that a login cookie sent without a domain gets the login host's, so it can be saved for yt-dlp.
func TestLoginCookieDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"}) // Host-only: no Domain attribute.
		}
	}))
	defer srv.Close()

	cookies, err := login(context.Background(), &models.ChannelURLAccessDetails{LoginURL: srv.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	if len(cookies) != 1 || cookies[0].Domain != u.Hostname() {
		t.Fatalf("expected one cookie with domain %q, got %+v", u.Hostname(), cookies)
	}
}

// TestLoginRedirectCookie tests that a session cookie set only on the login's redirect is returned.
func TestLoginRedirectCookie(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
			http.Redirect(w, r, "/home", http.StatusFound)
		}
	})
	mux.HandleFunc("/home", func(http.ResponseWriter, *http.Request) {}) // Sets no cookies.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cookies, err := login(context.Background(), &models.ChannelURLAccessDetails{LoginURL: srv.URL + "/login", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "abc" || cookies[0].Domain != u.Hostname() {
		t.Fatalf("expected the redirect's session cookie with domain %q, got %+v", u.Hostname(), cookies)
	}
}

// ******************************** Cookies *******************************************************************************************

// TestCookieFileWritable tests that the cookie file is saved writable by its owner, replacing a read-only one. yt-dlp
// writes its cookies back to the file after each run, and exits with an error if it can't.
func TestCookieFileWritable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte("# Netscape HTTP Cookie File\n"), 0o400); err != nil {
		t.Fatal(err)
	}

	if err := saveCookiesToFile([]*http.Cookie{{Name: "a", Value: "1", Domain: "example.com", Path: "/"}}, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o600 != 0o600 {
		t.Errorf("cookie file saved with mode %o, want it readable and writable by its owner", mode)
	}
}

// ******************************** Extract *******************************************************************************************

// extractTestPage is a video page holding its embed ID in JSON-LD, a player iframe, and an ID with a broken API.
const extractTestPage = `<html><head>
<script type="application/ld+json">{"@type":"Organization","name":"Example"}</script>
<script type="application/ld+json">{"@type":"VideoObject","embedUrl":"https://example.com/embed/v42/"}</script>
</head><body>
<iframe src="/player/v42"></iframe>
<div data-id="bad"></div>
</body></html>`

// newExtractTestServer serves the test page, a JSON video API, and an HTML player page, recording API referers.
func newExtractTestServer(t *testing.T) (srv *httptest.Server, referers func() []string) {
	t.Helper()
	var (
		mu   sync.Mutex
		refs []string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/video/1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(extractTestPage))
	})
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		refs = append(refs, r.Referer())
		mu.Unlock()
		if r.URL.Query().Get("v") != "v42" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"u":{"hls":{"url":"https://cdn.example.com/v42/playlist.m3u8"}}}`))
	})
	mux.HandleFunc("/player/v42", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><video><source src="https://cdn.example.com/v42/player.m3u8"></video></body></html>`))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), refs...)
	}
}

// scrapeTestRules scrapes the test page with the given rules, returning the scraped direct video URL.
func scrapeTestRules(t *testing.T, pageURL string, rules []models.MetadataRule) string {
	t.Helper()
	s := New()
	v := &models.Video{URL: pageURL}
	query := models.SiteRules{Site: "test", Metadata: rules}
	if err := s.visitPage(t.Context(), pageURL, nil, query, func(c *colly.Collector) {
		s.ScrapeWithRules(pageURL, c, v, query)
	}); err != nil {
		t.Fatal(err)
	}
	return v.Metadata.DirectVideoURL
}

// TestScrapeFetchChain tests selectors that extract a value and follow it with fetch steps.
func TestScrapeFetchChain(t *testing.T) {
	srv, referers := newExtractTestServer(t)
	pageURL := srv.URL + "/video/1"

	tests := []struct {
		name  string
		rules []models.MetadataRule
		want  string
	}{
		{
			name: "json api",
			rules: []models.MetadataRule{{
				Name:     sharedtags.JDirectVideoURL,
				Selector: `script[type="application/ld+json"]`,
				JSONPath: "embedUrl",
				Regex:    regexp.MustCompile(`embed/([a-z0-9]+)`),
				Fetch:    []models.FetchStep{{URL: "/api?v={value}", JSONPath: "u.hls.url"}},
			}},
			want: "https://cdn.example.com/v42/playlist.m3u8",
		},
		{
			name: "html player",
			rules: []models.MetadataRule{{
				Name:     sharedtags.JDirectVideoURL,
				Selector: "iframe[src]",
				Attr:     "src",
				Fetch:    []models.FetchStep{{URL: "{value}", Selector: "source[src]", Attr: "src"}},
			}},
			want: "https://cdn.example.com/v42/player.m3u8",
		},
		{
			name: "failed fetch falls through to next rule",
			rules: []models.MetadataRule{
				{
					Name:     sharedtags.JDirectVideoURL,
					Selector: "div[data-id]",
					Attr:     "data-id",
					Fetch:    []models.FetchStep{{URL: "/api?v={value}", JSONPath: "u.hls.url"}},
				},
				{
					Name:     sharedtags.JDirectVideoURL,
					Selector: `script[type="application/ld+json"]`,
					JSONPath: "embedUrl",
					Regex:    regexp.MustCompile(`embed/([a-z0-9]+)`),
					Fetch:    []models.FetchStep{{URL: "/api?v={value}", JSONPath: "u.hls.url"}},
				},
			},
			want: "https://cdn.example.com/v42/playlist.m3u8",
		},
		{
			name: "regex no match",
			rules: []models.MetadataRule{{
				Name:     sharedtags.JDirectVideoURL,
				Selector: `script[type="application/ld+json"]`,
				Regex:    regexp.MustCompile(`nomatch/(\d+)`),
				Fetch:    []models.FetchStep{{URL: "/api?v={value}", JSONPath: "u.hls.url"}},
			}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scrapeTestRules(t, pageURL, tt.rules); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	for _, ref := range referers() {
		if ref != pageURL {
			t.Errorf("API request sent referer %q, want %q", ref, pageURL)
		}
	}
}

// TestScrapeDuration checks scraped metadata gets duration in seconds, from duration or else duration_string.
func TestScrapeDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><head><script type=application/ld+json>[{"@context":"http://schema.org","@type":"VideoObject","duration":"PT07H24M44S","interactionStatistic":{"@type":"InteractionCounter"}},{"@context":"http://schema.org","@type":"WebSite"},{"@context":"http://schema.org","@type":"Organization"}]</script></head>` +
			`<body><span class="duration">1:02:03</span><span class="secs">90</span><span class="bad">abc</span></body></html>`))
	}))
	defer srv.Close()

	tests := []struct {
		name       string
		rules      []models.MetadataRule
		wantSecs   any
		wantString any
	}{
		{"string only", []models.MetadataRule{{Name: sharedtags.JDurationString, Selector: ".duration"}}, 3723, "1:02:03"},
		{"duration preferred", []models.MetadataRule{{Name: sharedtags.JDurationString, Selector: ".duration"}, {Name: sharedtags.JDuration, Selector: ".secs"}}, 90, "1:02:03"},
		{"bad duration falls back", []models.MetadataRule{{Name: sharedtags.JDuration, Selector: ".bad"}, {Name: sharedtags.JDurationString, Selector: ".duration"}}, 3723, "1:02:03"},
		{"nothing parses", []models.MetadataRule{{Name: sharedtags.JDuration, Selector: ".bad"}}, nil, nil},
		{"rumble json-ld", []models.MetadataRule{{Name: sharedtags.JDuration, Selector: `script[type="application/ld+json"]`, JSONPath: `#(@type=="VideoObject").duration`}}, 26684, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			v := &models.Video{URL: srv.URL}
			query := models.SiteRules{Site: "test", Metadata: tt.rules}
			var metadata map[string]any
			if err := s.visitPage(t.Context(), srv.URL, nil, query, func(c *colly.Collector) {
				metadata = s.ScrapeWithRules(srv.URL, c, v, query)
			}); err != nil {
				t.Fatal(err)
			}
			if got := metadata[sharedtags.JDuration]; got != tt.wantSecs {
				t.Errorf("duration = %v, want %v", got, tt.wantSecs)
			}
			if got := metadata[sharedtags.JDurationString]; got != tt.wantString {
				t.Errorf("duration_string = %v, want %v", got, tt.wantString)
			}
		})
	}
}

// TestExtractValue tests applying a JSON path and regex to a value.
func TestExtractValue(t *testing.T) {
	tests := []struct {
		name, value, jsonPath string
		re                    *regexp.Regexp
		want                  string
	}{
		{"none", " abc ", "", nil, "abc"},
		{"json path", `{"a":{"b":"x"}}`, "a.b", nil, "x"},
		{"json path missing", `{"a":1}`, "b", nil, ""},
		{"regex group", "id=v42;", "", regexp.MustCompile(`id=(\w+)`), "v42"},
		{"regex whole match", "id=v42;", "", regexp.MustCompile(`v\d+`), "v42"},
		{"regex no match", "abc", "", regexp.MustCompile(`\d`), ""},
		{"json path then regex", `{"u":"https://x/embed/v7/"}`, "u", regexp.MustCompile(`embed/(\w+)`), "v7"},
		{"json-ld query", `[{"@type":"WebSite"},{"@type":"VideoObject","duration":"PT07H24M44S"}]`, `#(@type=="VideoObject").duration`, nil, "PT07H24M44S"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractValue(tt.value, tt.jsonPath, tt.re); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFetchStepURL tests filling and resolving fetch step URLs, rejecting non-http(s) results.
func TestFetchStepURL(t *testing.T) {
	tests := []struct {
		name, template, value, want string
		wantErr                     bool
	}{
		{"absolute", "https://api.example.com/v?id={value}", "v42", "https://api.example.com/v?id=v42", false},
		{"relative", "/api?id={value}", "v42", "https://example.com/api?id=v42", false},
		{"scraped link", "{value}", "https://other.example.com/embed/v42", "https://other.example.com/embed/v42", false},
		{"non-http scheme", "{value}", "file:///etc/passwd", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fetchStepURL("https://example.com/video/1", tt.template, tt.value)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("got %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// ******************************** FlareSolverr **************************************************************************************

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

	got, err := New().crawlWithRule(t.Context(), site.URL+"/channel", nil, flareSolverrQuery())
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
	got, err := New().crawlWithRule(t.Context(), site.URL+"/channel", nil, flareSolverrQuery())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || solves.Load() != 2 {
		t.Errorf("got %d URLs after %d solves, want 1 URL after 2 solves", len(got), solves.Load())
	}

	// A site that keeps challenging fails after one retry, rather than solving forever.
	solves.Store(0)
	site = fakeCloudflareSite(t, 100)
	if _, err := New().crawlWithRule(t.Context(), site.URL+"/channel", nil, flareSolverrQuery()); err == nil || !strings.Contains(err.Error(), "again after a fresh FlareSolverr solve") {
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
	got, err := New().crawlWithRule(t.Context(), plain.URL+"/channel", nil, flareSolverrQuery())
	if err != nil || len(got) != 1 {
		t.Fatalf("expected a fallback crawl to find 1 URL, got %v, %v", got, err)
	}

	// If Cloudflare then challenges, the error says why FlareSolverr wasn't used.
	site := fakeCloudflareSite(t, 1)
	_, err = New().crawlWithRule(t.Context(), site.URL+"/channel", nil, flareSolverrQuery())
	if err == nil || !strings.Contains(err.Error(), "FlareSolverr could not be used") || !strings.Contains(err.Error(), "no FlareSolverr URL") {
		t.Errorf("expected an error explaining FlareSolverr wasn't used, got %v", err)
	}

	// A failing FlareSolverr is only asked once per crawl session.
	var calls atomic.Int32
	useFlareSolverr(t, fakeFailingFlareSolverr(t, 0, &calls))
	s := New()
	for range 3 {
		if _, err := s.crawlWithRule(t.Context(), plain.URL+"/channel", nil, flareSolverrQuery()); err != nil {
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
	_, err := New().crawlWithRule(t.Context(), site.URL+"/channel", nil, flareSolverrQuery())
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
		if _, err := s.crawlWithRule(t.Context(), site.URL+"/channel", nil, flareSolverrQuery()); err != nil {
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
	cu := &models.ChannelURL{URL: "https://cf.example/channel", Session: models.ChannelURLSession{CookiePath: filepath.Join(t.TempDir(), "cookies.txt")}}

	first, err := cm.flareSolverrSolution(ctx, cu.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	fs := models.NewFlareSolverrSolution(first.ytDLPSolve(), cm.flareSolverrResolver(t.Context(), cu, nil), cm.flareSolverrNewer(cu, nil))
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
	if cookies, _ := os.ReadFile(cu.Session.CookiePath); !strings.Contains(string(cookies), "cf_clearance\t2") {
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
	if _, err := s.crawlWithRule(t.Context(), site.URL+"/channel", first.cookies, flareSolverrQuery()); err != nil {
		t.Errorf("expected the retry to use the refreshed solve's cookies, got %v", err)
	}
	if solves.Load() != 2 {
		t.Errorf("expected 2 FlareSolverr solves, got %d", solves.Load())
	}
}

// ******************************** Headers *******************************************************************************************

// TestChromeBrands tests that sec-ch-ua matches what Google Chrome sends for a version.
func TestChromeBrands(t *testing.T) {
	tests := map[int]string{
		120: `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
		124: `"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`,
	}
	for major, want := range tests {
		if got := chromeBrands(major); got != want {
			t.Errorf("chromeBrands(%d) = %s, want %s", major, got, want)
		}
	}
}

// TestChromeHeadersSent tests that Chrome impersonation sends Chrome's page load headers matching the user agent,
// keeping the user agent and cookies, and that requests without impersonation are left as they were.
func TestChromeHeadersSent(t *testing.T) {
	const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`<a href="/video/1">One</a>`))
	}))
	defer srv.Close()

	crawl := func(impersonate string) {
		t.Helper()
		query := models.SiteRules{Site: "test", Impersonate: impersonate, UserAgent: userAgent, Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href"}}
		if _, err := New().crawlWithRule(t.Context(), srv.URL, []*http.Cookie{{Name: "session", Value: "abc"}}, query); err != nil {
			t.Fatal(err)
		}
	}

	crawl("chrome")
	checks := map[string]string{
		"User-Agent":         userAgent,
		"Sec-Ch-Ua":          chromeBrands(152),
		"Sec-Ch-Ua-Platform": `"Linux"`,
		"Sec-Fetch-Mode":     "navigate",
		"Accept":             chromeAccept,
	}
	for name, want := range checks {
		if got.Get(name) != want {
			t.Errorf("Chrome impersonation: %s = %q, want %q", name, got.Get(name), want)
		}
	}
	if !strings.Contains(got.Get("Cookie"), "session=abc") {
		t.Errorf("Chrome impersonation: expected the cookie to be kept, got %q", got.Get("Cookie"))
	}
	for name := range got {
		if strings.Contains(strings.ToLower(name), "order") {
			t.Errorf("Chrome impersonation: header ordering key %q was sent as a header", name)
		}
	}

	crawl("")
	if got.Get("Accept") != "*/*" || got.Get("Sec-Ch-Ua") != "" {
		t.Errorf("no impersonation: expected Colly's defaults only, got Accept %q, Sec-Ch-Ua %q", got.Get("Accept"), got.Get("Sec-Ch-Ua"))
	}
}

// TestFirefoxHeadersSent tests that Firefox impersonation sends Firefox's page load headers, without Chrome's client
// hints, keeping the user agent and cookies.
func TestFirefoxHeadersSent(t *testing.T) {
	const userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0"
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`<a href="/video/1">One</a>`))
	}))
	defer srv.Close()

	query := models.SiteRules{Site: "test", Impersonate: "firefox", UserAgent: userAgent, Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href"}}
	if _, err := New().crawlWithRule(t.Context(), srv.URL, []*http.Cookie{{Name: "session", Value: "abc"}}, query); err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"User-Agent":     userAgent,
		"Accept":         "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Sec-Fetch-Mode": "navigate",
		"Te":             "trailers",
		"Sec-Ch-Ua":      "",
	}
	for name, want := range checks {
		if got.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, got.Get(name), want)
		}
	}
	if !strings.Contains(got.Get("Cookie"), "session=abc") {
		t.Errorf("expected the cookie to be kept, got %q", got.Get("Cookie"))
	}
}

// TestDescribeHeaders tests that headers are described in send order, with cookie values left out.
func TestDescribeHeaders(t *testing.T) {
	h := fhttp.Header{}
	h.Set("User-Agent", "UA/1")
	h.Set("Accept", "text/html")
	h.Set("Cookie", "cf_clearance=secret; session=also-secret")
	h.Set("X-Extra", "1")
	h[fhttp.HeaderOrderKey] = []string{"host", "accept", "user-agent", "cookie"}

	want := "accept: text/html | user-agent: UA/1 | cookie: cf_clearance; session | x-extra: 1"
	if got := describeHeaders(h); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ******************************** Helpers *******************************************************************************************

// TestVideoEntriesNestedTabs checks videos are found inside a channel's tab playlists, as yt-dlp returns for a channel root URL.
func TestVideoEntriesNestedTabs(t *testing.T) {
	const out = `{"entries":[
		{"_type":"playlist","url":null,"entries":[{"url":"https://www.youtube.com/watch?v=a"},{"url":"https://www.youtube.com/watch?v=b"}]},
		{"_type":"playlist","url":null,"entries":[{"url":"https://www.youtube.com/shorts/c"}]},
		{"_type":"url","url":"https://www.youtube.com/watch?v=d"}
	]}`
	var result ytDlpEntry
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}

	entries := result.videoEntries()
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.URL)
	}
	want := []string{
		"https://www.youtube.com/watch?v=a",
		"https://www.youtube.com/watch?v=b",
		"https://www.youtube.com/shorts/c",
		"https://www.youtube.com/watch?v=d",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestListedInOrder checks crawled videos come back in the order the channel lists them, with a repeated URL kept at
// its first position and its details merged.
func TestListedInOrder(t *testing.T) {
	iterationCount := 30
	want := make([]string, 0, iterationCount)
	urls := make(map[string]*listedVideo)
	for i := range iterationCount {
		u := fmt.Sprintf("https://example.com/v/%d", i)
		want = append(want, u)
		mergeListed(urls, u, &listedVideo{})
	}
	mergeListed(urls, want[3], &listedVideo{Title: "Repeat"})

	if got := listedInOrder(urls); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if lv := urls[want[3]]; lv.Order != 3 || lv.Title != "Repeat" {
		t.Errorf("repeated URL: got order %d, title %q, want 3, %q", lv.Order, lv.Title, "Repeat")
	}
}

// TestListedFlatEntry checks a flat playlist entry's title, best thumbnail, duration, and date are kept.
func TestListedFlatEntry(t *testing.T) {
	const out = `{"entries":[
		{"url":"a","title":"A","duration":312.5,"upload_date":"20260102","thumbnails":[{"url":"small.jpg"},{"url":"best.jpg"}]},
		{"url":"b","thumbnail":"main.jpg","timestamp":1767312000},
		{"url":"c"}
	]}`
	var result ytDlpEntry
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	entries := result.videoEntries()

	a := entries[0].listed()
	if a.Title != "A" || a.ThumbnailURL != "best.jpg" || a.Duration != 312 || !a.UploadDate.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("entry a: got %+v", a)
	}
	b := entries[1].listed()
	if b.ThumbnailURL != "main.jpg" || !b.UploadDate.Equal(time.Unix(1767312000, 0)) {
		t.Errorf("entry b: got %+v", b)
	}
	if c := entries[2].listed(); *c != (listedVideo{}) {
		t.Errorf("entry c: expected no details, got %+v", c)
	}
}

// TestIgnoreDownloadedURLs tests that known URLs are dropped whatever their scheme or trailing slash, keeping order.
func TestIgnoreDownloadedURLs(t *testing.T) {
	existing := []string{"http://a.example/v/1/", "https://a.example/v/3"}
	got := ignoreDownloadedURLs([]string{
		"https://a.example/v/1",
		"https://a.example/v/2",
		"http://a.example/v/3/",
		"https://a.example/v/4",
	}, existing)
	if want := []string{"https://a.example/v/2", "https://a.example/v/4"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ******************************** Long titles ***************************************************************************************

// TestScrapeCustomSiteLongTitle tests a scraped title too long for a filename, in bytes, still gets its metadata file.
func TestScrapeCustomSiteLongTitle(t *testing.T) {
	var title string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h1>%s</h1></body></html>", title)
	}))
	defer srv.Close()

	siterules.Reset()
	t.Cleanup(siterules.Reset)
	siterules.Register([]models.SiteScraper{{Domain: "127.0.0.1", Selectors: []models.ScrapeSelectors{{Field: "title", Selector: "h1"}}}})

	for _, tc := range []struct{ name, title string }{
		{"short", "A short title"},
		{"100 Japanese characters", strings.Repeat("猫", 100)},
		{"300 English characters", strings.Repeat("a", 300)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title = tc.title
			dir := t.TempDir()
			v := &models.Video{URL: srv.URL + "/watch/1"}
			if err := New().ScrapeCustomSite(t.Context(), v.URL, dir, v, nil); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("wrote %d files, want 1", len(entries))
			}
			if v.Metadata.Title != tc.title {
				t.Errorf("title = %q, want it whole", v.Metadata.Title)
			}
		})
	}
}

// ******************************** Fuzz **********************************************************************************************

// FuzzParseHeaderEcho tests that reading headers from any echo page never panics, and only returns usable headers.
func FuzzParseHeaderEcho(f *testing.F) {
	f.Add(testEchoPage())
	f.Add(`{"http1": {"headers": ["Host: x", "User-Agent: y", "Cookie: a=b"]}}`)
	f.Add(`<pre>{"http2":{"sent_frames":[{"frame_type":"HEADERS","headers":[": x", "a:b", "c: "]}]}}</pre>`)
	f.Add("<html><body>Blocked</body></html>")
	f.Fuzz(func(t *testing.T, page string) {
		headers, err := parseHeaderEcho(page)
		if err != nil {
			return
		}
		for _, kv := range headers {
			switch {
			case kv[0] == "", strings.HasPrefix(kv[0], ":"):
				t.Errorf("returned a pseudo or empty header name %q", kv[0])
			case kv[0] == "cookie", kv[0] == "host", kv[0] == "content-length":
				t.Errorf("returned per-request header %q", kv[0])
			case kv[0] != strings.ToLower(kv[0]):
				t.Errorf("returned a non-lowercase header name %q", kv[0])
			}
		}
	})
}

// FuzzCrawlRuleValues tests that extracting values with any JSON path never panics.
func FuzzCrawlRuleValues(f *testing.F) {
	f.Add(`{"items":[{"object_type":"video","url":"https://rumble.com/v1.html"}]}`, `items.#(object_type=="video")#.url`)
	f.Add(`[1,2,3]`, `#`)
	f.Add(`not json`, `a.b`)
	f.Add(`{"a":{"b":[{"c":1}]}}`, `a.b.#.c`)
	f.Add(`{}`, ``)
	f.Fuzz(func(_ *testing.T, value, jsonPath string) {
		_ = jsonPathValues(value, jsonPath)
	})
}

// FuzzURLHelpers tests that the URL helpers never panic, and that removing query parameters is idempotent and leaves no
// query.
func FuzzURLHelpers(f *testing.F) {
	for _, s := range []string{
		"https://rumble.com/v79e41g-title.html?e9s=src", "https://rumble.com/videos?x", "https://rumble.com/user/x",
		"http://[::1]:80/a?b#c", "%zz", "", "://", "https://a.example/v?x=1&y=2#frag", "rumble.com/v1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = isValidRumbleVideoURL(s)
		_ = normalizeURL(s)

		stripped := removeQueryParams(s)
		if again := removeQueryParams(stripped); again != stripped {
			t.Errorf("removeQueryParams isn't idempotent: %q -> %q -> %q", s, stripped, again)
		}
	})
}

// FuzzUserAgentHeaders tests that parsing user agents and building Chrome headers from them never panics.
func FuzzUserAgentHeaders(f *testing.F) {
	for _, s := range []string{
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0",
		"Chrome/99999999999999999999999", "Chrome/-1", "Chrome/", "", "Android CrOS Windows Macintosh",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, ua string) {
		major := userAgentMajorVersion(ua)
		if major < 0 {
			t.Errorf("negative major version %d from %q", major, ua)
		}
		_ = chromeBrands(major)
		_ = chromePlatform(ua)

		h := fhttp.Header{}
		h.Set("User-Agent", ua)
		setChromeHeaders(h)
		if h.Get("User-Agent") != ua {
			t.Errorf("setChromeHeaders changed the user agent")
		}
	})
}

// FuzzDescribeHeaders tests that describing headers never prints a cookie value.
func FuzzDescribeHeaders(f *testing.F) {
	f.Add("cf_clearance", "secret-value", "accept", "text/html")
	f.Add("a", "b=c=d", "x", "")
	f.Add("", "", "", "")
	f.Add("0", "00000a", "00000A", "0") // Another header's name matches the value once lowercased.
	f.Add("0", "00000:", "00000", "0")  // Another header's name and separator match the value.
	f.Fuzz(func(t *testing.T, cookieName, cookieValue, headerName, headerValue string) {
		// Only check values that can't appear on the cookie line for other reasons.
		if len(cookieValue) < 6 || strings.ContainsAny(cookieValue, ";= \t") || strings.Contains(cookieName, cookieValue) ||
			strings.Contains(headerValue, cookieValue) {
			return
		}
		h := fhttp.Header{}
		h.Set("Cookie", cookieName+"="+cookieValue)
		if headerName != "" {
			h.Set(headerName, headerValue)
		}
		h[fhttp.HeaderOrderKey] = []string{"host", "cookie", strings.ToLower(headerName)}

		out := describeHeaders(h)
		for _, line := range strings.Split(out, " | ") {
			if cookies, ok := strings.CutPrefix(line, "cookie: "); ok && strings.Contains(cookies, cookieValue) {
				t.Errorf("describeHeaders printed cookie value %q: %s", cookieValue, out)
			}
		}
	})
}

// FuzzScrapedDates tests that parsing scraped dates and years never panics.
func FuzzScrapedDates(f *testing.F) {
	for _, s := range []string{"2026-10-11T17:06:59+01:00", "October 11, 2026", "11/10/26", "2026", "Streamed 3 hours ago", "", "0000-00-00"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, raw string) {
		_, _ = parseScrapedDate(raw)
		_, _ = parseScrapedYear(raw)
	})
}

// FuzzSaveCookiesToFile tests that every cookie with a domain can be read back from the Netscape cookie file, and that
// cookies without one are left out.
func FuzzSaveCookiesToFile(f *testing.F) {
	f.Add("cf_clearance", "abc.def-123", ".rumble.com", "/")
	f.Add("a", "b", "", "")
	f.Add("session", "x=y", "www.a.example", "/p")
	f.Fuzz(func(t *testing.T, name, value, domain, path string) {
		// Only cookies a browser would accept: names and values without whitespace or control characters.
		for _, s := range []string{name, value, domain, path} {
			if strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
				return
			}
		}
		if name == "" {
			return
		}
		// Only hostname characters, as in any real cookie domain.
		if strings.ContainsFunc(domain, func(r rune) bool {
			return r != '.' && r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
		}) {
			return
		}
		wantWritten := domain != ""

		p := filepath.Join(t.TempDir(), "cookies.txt")
		if err := saveCookiesToFile([]*http.Cookie{{Name: name, Value: value, Domain: domain, Path: path}}, p); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}

		var found bool
		for line := range strings.SplitSeq(string(data), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) != 7 {
				t.Fatalf("cookie line has %d fields, want 7: %q", len(fields), line)
			}
			if fields[5] == name && fields[6] == value {
				found = true
			}
		}
		if found != wantWritten {
			t.Errorf("cookie %q=%q (domain %q): written = %v, want %v:\n%s", name, value, domain, found, wantWritten, data)
		}
	})
}

// FuzzGenerateCookieFilePath tests that a cookie file path always stays in Tubarr's directory, whatever the channel name.
func FuzzGenerateCookieFilePath(f *testing.F) {
	for _, s := range []string{"Kino Casino", "AC/DC", "../../etc/x", "a\\b", "", ".", ".."} {
		f.Add(s, "https://rumble.com/c/x")
		f.Add(s, "")
	}
	f.Fuzz(func(t *testing.T, channelName, videoURL string) {
		p := generateCookieFilePath(channelName, videoURL)
		dir := filepath.Dir(p)
		if base := filepath.Clean(filepath.Dir(generateCookieFilePath("x", ""))); dir != base {
			t.Errorf("cookie file for channel %q is in %q, not Tubarr's directory %q", channelName, dir, base)
		}
	})
}
