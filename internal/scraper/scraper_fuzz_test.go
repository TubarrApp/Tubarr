package scraper

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fhttp "github.com/bogdanfinn/fhttp"
)

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
		_, _ = getBaseDomain(s)

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
	f.Fuzz(func(t *testing.T, cookieName, cookieValue, headerName, headerValue string) {
		if len(cookieValue) < 6 || strings.ContainsAny(cookieValue, ";= \t") || strings.Contains(cookieName, cookieValue) ||
			strings.Contains(headerName, cookieValue) || strings.Contains(headerValue, cookieValue) {
			return // Only check values that can't appear in the output for other reasons.
		}
		h := fhttp.Header{}
		h.Set("Cookie", cookieName+"="+cookieValue)
		if headerName != "" {
			h.Set(headerName, headerValue)
		}
		h[fhttp.HeaderOrderKey] = []string{"host", "cookie", strings.ToLower(headerName)}

		if out := describeHeaders(h); strings.Contains(out, cookieValue) {
			t.Errorf("describeHeaders printed cookie value %q: %s", cookieValue, out)
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
			return !(r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
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
		for _, line := range strings.Split(string(data), "\n") {
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
