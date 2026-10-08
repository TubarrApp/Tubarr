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
