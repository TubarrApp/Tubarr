package scraper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtags"
)

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
			got, err := New().crawlWithRule(srv.URL+"/channel", nil, query)
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

// TestCrawlWithRuleNoMatch tests that crawlWithRule returns an empty result when no URLs match the crawl rule.
func TestCrawlWithRuleRequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	query := models.SiteRules{Site: "test", Crawl: &models.CrawlRule{Selector: "a[href]", Attr: "href"}}
	if _, err := New().crawlWithRule(srv.URL, nil, query); err == nil {
		t.Error("expected an error for a 403 channel page")
	}
}

// TestCrawlWithRuleNilRule tests that crawlWithRule returns an error rather than panicking when a site has no crawl rule.
func TestCrawlWithRuleNilRule(t *testing.T) {
	if _, err := New().crawlWithRule("https://example.com", nil, models.SiteRules{Site: "example.com"}); err == nil {
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
		got, err := New().crawlWithRule(srv.URL, nil, query)
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
			{Name: sharedtags.JThumbnailURL, Selector: "img[data-src]", Attr: "data-src"},
			{Name: sharedtags.JThumbnailURL, Selector: "img[src]", Attr: "src"},
			{Name: consts.CrawlFieldDuration, Selector: ".duration"},
			{Name: sharedtags.JUploadDate, Selector: "time"},
		},
	}
	got, err := New().crawlWithRule(srv.URL, nil, models.SiteRules{Site: "test", Crawl: &html})
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
			{Name: sharedtags.JThumbnailURL, JSONPath: "thumb"},
			{Name: consts.CrawlFieldDuration, JSONPath: "duration"},
			{Name: sharedtags.JUploadDate, JSONPath: "upload_date"},
		},
	}
	got, err = New().crawlWithRule(srv.URL, nil, models.SiteRules{Site: "test", Crawl: &json})
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

// TestParseDuration tests the duration formats crawl preview fields accept.
func TestParseDuration(t *testing.T) {
	for in, want := range map[string]int{"00:47:35": 2855, "1:02:03": 3723, "62:03": 3723, "3723": 3723, "312.5": 312, "PT1H2M3S": 3723, "pt47m": 2820, "abc": 0, "1:xx": 0, "": 0} {
		if got := parseDuration(in); got != want {
			t.Errorf("parseDuration(%q) = %d, want %d", in, got, want)
		}
	}
}
