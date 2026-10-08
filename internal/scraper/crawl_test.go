package scraper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
)

// TestMain sets up the test environment for the scraper package.
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
		rule consts.HTMLCrawlRule
		want []string
	}{
		{
			name: "links",
			rule: consts.HTMLCrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile("/video/"), StripQuery: true},
			want: []string{srv.URL + "/video/abc", srv.URL + "/video/def"},
		},
		{
			name: "links exclude",
			rule: consts.HTMLCrawlRule{Selector: "a[href]", Attr: "href", Exclude: regexp.MustCompile("/about|abc")},
			want: []string{srv.URL + "/video/def"},
		},
		{
			name: "json",
			rule: consts.HTMLCrawlRule{
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
			query := consts.HTMLMetadataQuery{Site: "test", Crawl: &tt.rule}
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

	query := consts.HTMLMetadataQuery{Site: "test", Crawl: &consts.HTMLCrawlRule{Selector: "a[href]", Attr: "href"}}
	if _, err := New().crawlWithRule(srv.URL, nil, query); err == nil {
		t.Error("expected an error for a 403 channel page")
	}
}

// TestCrawlWithRuleNilRule tests that crawlWithRule returns an error rather than panicking when a site has no crawl rule.
func TestCrawlWithRuleNilRule(t *testing.T) {
	if _, err := New().crawlWithRule("https://example.com", nil, consts.HTMLMetadataQuery{Site: "example.com"}); err == nil {
		t.Error("expected an error for a site with no crawl rule")
	}
}
