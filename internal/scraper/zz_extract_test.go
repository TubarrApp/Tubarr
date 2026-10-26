package scraper

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtags"
	"github.com/gocolly/colly"
)

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
	return v.DirectVideoURL
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
