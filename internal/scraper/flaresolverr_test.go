package scraper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"

	"github.com/spf13/viper"
)

// fakeFlareSolverr starts a server that answers FlareSolverr API requests with the given HTML, recording each request.
func fakeFlareSolverr(t *testing.T, html string, received *[]flareSolverrRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var req flareSolverrRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*received = append(*received, req)

		resp := map[string]any{"status": "ok", "message": "", "solution": map[string]any{"url": req.URL, "status": 200, "response": html}}
		if strings.Contains(req.URL, "fail") {
			resp = map[string]any{"status": "error", "message": "Error solving the challenge."}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCrawlWithRuleFlareSolverr tests that a site set to use FlareSolverr crawls the page FlareSolverr returns.
func TestCrawlWithRuleFlareSolverr(t *testing.T) {
	var received []flareSolverrRequest
	srv := fakeFlareSolverr(t, `<html><body><a href="https://cf.example/video/1">One</a><a href="/about">About</a></body></html>`, &received)
	viper.Set(keys.FlareSolverrURL, srv.URL)
	t.Cleanup(func() { viper.Set(keys.FlareSolverrURL, "") })

	query := consts.HTMLMetadataQuery{
		Site:         "cf.example",
		Impersonate:  "chrome",
		FlareSolverr: true,
		Crawl:        &consts.HTMLCrawlRule{Selector: "a[href]", Attr: "href", Include: regexp.MustCompile("/video/")},
	}
	cookies := []*http.Cookie{{Name: "session", Value: "abc"}}
	got, err := New().crawlWithRule("https://cf.example/channel", cookies, query)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["https://cf.example/video/1"]; !ok || len(got) != 1 {
		t.Errorf("got %v, want only https://cf.example/video/1", got)
	}

	if len(received) != 1 {
		t.Fatalf("expected 1 FlareSolverr request, got %d", len(received))
	}
	req := received[0]
	if req.Cmd != "request.get" || req.URL != "https://cf.example/channel" || req.MaxTimeout <= 0 {
		t.Errorf("unexpected FlareSolverr request: %+v", req)
	}
	if !slices.Contains(req.Cookies, flareSolverrCookie{Name: "session", Value: "abc"}) {
		t.Errorf("expected channel cookie to be forwarded, got %+v", req.Cookies)
	}
}

// TestCrawlWithRuleFlareSolverrErrors tests that FlareSolverr failures and a missing URL are reported as errors.
func TestCrawlWithRuleFlareSolverrErrors(t *testing.T) {
	var received []flareSolverrRequest
	srv := fakeFlareSolverr(t, "", &received)
	t.Cleanup(func() { viper.Set(keys.FlareSolverrURL, "") })

	query := consts.HTMLMetadataQuery{Site: "cf.example", FlareSolverr: true, Crawl: &consts.HTMLCrawlRule{Selector: "a[href]", Attr: "href"}}

	viper.Set(keys.FlareSolverrURL, srv.URL)
	if _, err := New().crawlWithRule("https://cf.example/fail", nil, query); err == nil || !strings.Contains(err.Error(), "Error solving the challenge") {
		t.Errorf("expected FlareSolverr's error to be reported, got %v", err)
	}

	viper.Set(keys.FlareSolverrURL, "")
	if _, err := New().crawlWithRule("https://cf.example/channel", nil, query); err == nil || !strings.Contains(err.Error(), "no FlareSolverr URL") {
		t.Errorf("expected a missing FlareSolverr URL error, got %v", err)
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
