package scraper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"tubarr/internal/models"

	fhttp "github.com/bogdanfinn/fhttp"
)

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
		if _, err := New().crawlWithRule(srv.URL, []*http.Cookie{{Name: "session", Value: "abc"}}, query); err != nil {
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
	if _, err := New().crawlWithRule(srv.URL, []*http.Cookie{{Name: "session", Value: "abc"}}, query); err != nil {
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
