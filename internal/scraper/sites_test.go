package scraper

import (
	"regexp"
	"testing"
	"tubarr/internal/dev"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestBuiltinSitesToggles tests that each site's metadata and crawl toggles gate its built-in rules.
func TestBuiltinSitesToggles(t *testing.T) {
	sites := builtinSites()
	checks := []struct {
		query  consts.HTMLMetadataQuery
		toggle dev.BuiltinScraper
	}{
		{consts.HTMLCensored, dev.CensoredTV},
		{consts.HTMLBitchute, dev.BitchuteCom},
		{consts.HTMLOdysee, dev.OdyseeCom},
		{consts.HTMLRumble, dev.RumbleCom},
	}
	for _, c := range checks {
		got := sites[c.query.Site]
		if hasRules := len(got.Rules) > 0; hasRules != c.toggle.Metadata {
			t.Errorf("%s: metadata rules registered = %v, toggle = %v", c.query.Site, hasRules, c.toggle.Metadata)
		}
		if hasCrawl := got.Crawl != nil; hasCrawl != c.toggle.Crawl {
			t.Errorf("%s: crawl rule registered = %v, toggle = %v", c.query.Site, hasCrawl, c.toggle.Crawl)
		}
	}
}

// TestRegisterCustomSitesMerge tests that user metadata selectors and crawl rules each only replace their built-in counterpart.
func TestRegisterCustomSitesMerge(t *testing.T) {
	t.Cleanup(ResetCustomSites)
	ResetCustomSites()

	userCrawl := &consts.HTMLCrawlRule{Selector: "a.episode", Attr: "href", Include: regexp.MustCompile(`/e/`)}
	RegisterCustomSites([]models.SiteScraper{
		{Domain: "censored.tv", Selectors: []models.ScrapeSelectors{{Field: "title", Selector: "h1"}}},
		{Domain: "bitchute.com", Crawl: userCrawl},
	})

	customSitesMu.RLock()
	censored, bitchute := customSites["censored.tv"], customSites["bitchute.com"]
	customSitesMu.RUnlock()

	if len(censored.Rules) != 1 || censored.Rules[0].Selector != "h1" {
		t.Errorf("censored.tv: expected user metadata selectors, got %+v", censored.Rules)
	}
	if dev.CensoredTV.Crawl && censored.Crawl != consts.HTMLCensored.Crawl {
		t.Errorf("censored.tv: expected built-in crawl rule to be kept, got %+v", censored.Crawl)
	}
	if bitchute.Crawl != userCrawl {
		t.Errorf("bitchute.com: expected user crawl rule, got %+v", bitchute.Crawl)
	}
}

// TestRegisterCustomSitesImpersonate tests that only an explicitly set impersonate value overrides the built-in one.
func TestRegisterCustomSitesImpersonate(t *testing.T) {
	t.Cleanup(ResetCustomSites)
	none, firefox, chrome := consts.ImpersonateNone, consts.ImpersonateFirefox, consts.ImpersonateChrome
	tests := []struct {
		name        string
		impersonate *consts.Impersonate
		want        string
	}{
		{"unset keeps built-in", nil, consts.HTMLRumble.Impersonate},
		{"explicit none disables", &none, ""},
		{"explicit value replaces", &firefox, "firefox"},
		{"explicit value replaces", &chrome, "chrome"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetCustomSites()
			RegisterCustomSites([]models.SiteScraper{{Domain: "rumble.com", Impersonate: tt.impersonate}})

			customSitesMu.RLock()
			got := customSites["rumble.com"].Impersonate
			customSitesMu.RUnlock()
			if got != tt.want {
				t.Errorf("got impersonate %q, want %q", got, tt.want)
			}
		})
	}
}
