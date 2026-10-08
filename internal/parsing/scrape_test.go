package parsing

import (
	"os"
	"path/filepath"
	"testing"
)

// writeScrapeConfig writes the given content to a temporary file with the given name, returning the full path.
func writeScrapeConfig(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestParseScrapeSelectorsFile tests parsing a scrape config file with multiple sites and selectors.
func TestParseScrapeSelectorsFileCrawl(t *testing.T) {
	p := writeScrapeConfig(t, "rules.yaml", `
sites:
  - domain: example.com
    impersonate: chrome
    crawl:
      selector: 'script[type="application/json"]'
      json_path: 'items.#.url'
      include: '/v\d+'
      exclude: '/shorts/'
      strip_query: true
`)
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].Crawl == nil {
		t.Fatalf("expected one site with a crawl rule, got %+v", sites)
	}
	c := sites[0].Crawl
	if c.JSONPath != "items.#.url" || !c.StripQuery || !c.Include.MatchString("/v123") || !c.Exclude.MatchString("/shorts/x") {
		t.Errorf("crawl rule parsed incorrectly: %+v", c)
	}
	if len(sites[0].Selectors) != 0 {
		t.Errorf("expected no metadata selectors, got %d", len(sites[0].Selectors))
	}
}

// TestParseScrapeSelectorsFileCrawlErrors tests that invalid crawl rules in a scrape config file produce errors.
func TestParseScrapeSelectorsFileCrawlErrors(t *testing.T) {
	tests := map[string]string{
		"no selectors or crawl":  "sites:\n  - domain: example.com\n",
		"missing crawl selector": "sites:\n  - domain: example.com\n    crawl:\n      include: '/v'\n",
		"bad include regex":      "sites:\n  - domain: example.com\n    crawl:\n      selector: a\n      include: '('\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseScrapeSelectorsFile(writeScrapeConfig(t, "rules.yaml", content)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// TestParseExampleScrapeRules tests that the example scrape rules file parses, including its Rumble crawl rule.
func TestParseExampleScrapeRules(t *testing.T) {
	sites, err := ParseScrapeSelectorsFile("../../web/src/files/example-scrape-rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		if s.Domain == "rumble.com" && s.Crawl != nil {
			return
		}
	}
	t.Error("expected a crawl rule for rumble.com")
}

// TestParseScrapeSelectorsFileImpersonate tests that an unset impersonate value is distinguished from an explicit "" or "none".
func TestParseScrapeSelectorsFileImpersonate(t *testing.T) {
	files := map[string]string{
		"rules.toml": `
[[sites]]
domain = "unset.com"
  [sites.crawl]
  selector = "a"
[[sites]]
domain = "empty.com"
impersonate = ""
  [sites.crawl]
  selector = "a"
[[sites]]
domain = "none.com"
impersonate = "none"
  [sites.crawl]
  selector = "a"
[[sites]]
domain = "firefox.com"
impersonate = "Firefox"
  [sites.crawl]
  selector = "a"
`,
		"rules.yaml": `
sites:
  - domain: unset.com
    crawl: {selector: a}
  - domain: empty.com
    impersonate: ""
    crawl: {selector: a}
  - domain: none.com
    impersonate: none
    crawl: {selector: a}
  - domain: firefox.com
    impersonate: Firefox
    crawl: {selector: a}
`,
		"rules.json": `{"sites": [
  {"domain": "unset.com", "crawl": {"selector": "a"}},
  {"domain": "empty.com", "impersonate": "", "crawl": {"selector": "a"}},
  {"domain": "none.com", "impersonate": "none", "crawl": {"selector": "a"}},
  {"domain": "firefox.com", "impersonate": "Firefox", "crawl": {"selector": "a"}}
]}`,
	}
	want := map[string]string{"empty.com": "", "none.com": "", "firefox.com": "firefox"}

	for name, content := range files {
		t.Run(name, func(t *testing.T) {
			sites, err := ParseScrapeSelectorsFile(writeScrapeConfig(t, name, content))
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range sites {
				w, explicit := want[s.Domain]
				switch {
				case !explicit && s.Impersonate != nil:
					t.Errorf("%s: expected unset impersonate, got %q", s.Domain, *s.Impersonate)
				case explicit && s.Impersonate == nil:
					t.Errorf("%s: expected impersonate %q, got unset", s.Domain, w)
				case explicit && string(*s.Impersonate) != w:
					t.Errorf("%s: expected impersonate %q, got %q", s.Domain, w, *s.Impersonate)
				}
			}
		})
	}
}

// TestParseScrapeSelectorsFileFlareSolverr tests that the flaresolverr key is read from a scrape config file.
func TestParseScrapeSelectorsFileFlareSolverr(t *testing.T) {
	p := writeScrapeConfig(t, "rules.toml", "[[sites]]\ndomain = \"on.com\"\nflaresolverr = true\n  [sites.crawl]\n  selector = \"a\"\n[[sites]]\ndomain = \"off.com\"\n  [sites.crawl]\n  selector = \"a\"\n")
	sites, err := ParseScrapeSelectorsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		if s.FlareSolverr != (s.Domain == "on.com") {
			t.Errorf("%s: FlareSolverr = %v", s.Domain, s.FlareSolverr)
		}
	}
}
