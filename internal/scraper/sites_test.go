package scraper

import (
	"regexp"
	"slices"
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

// TestRegisterCustomSitesSettingsOnly tests that a site entry setting only flaresolverr keeps the built-in rules,
// and works for a site with no rules (which uses yt-dlp, with FlareSolverr's cookies).
func TestRegisterCustomSitesSettingsOnly(t *testing.T) {
	t.Cleanup(ResetCustomSites)
	ResetCustomSites()
	RegisterCustomSites([]models.SiteScraper{
		{Domain: "rumble.com", FlareSolverr: true},
		{Domain: "norules.example", FlareSolverr: true},
	})

	customSitesMu.RLock()
	rumble := customSites["rumble.com"]
	customSitesMu.RUnlock()
	if !rumble.FlareSolverr || rumble.Impersonate != consts.HTMLRumble.Impersonate {
		t.Errorf("rumble.com: expected FlareSolverr on and built-in impersonation kept, got %+v", rumble)
	}
	if dev.RumbleCom.Crawl && rumble.Crawl != consts.HTMLRumble.Crawl {
		t.Errorf("rumble.com: expected the built-in crawl rule to be kept, got %+v", rumble.Crawl)
	}

	u := "https://norules.example/channel"
	if !siteUsesFlareSolverr(u) {
		t.Error("norules.example: expected FlareSolverr to be used")
	}
	if _, ok := matchCrawlSite(u); ok {
		t.Error("norules.example: expected no crawl rule, so yt-dlp crawls it")
	}
	if _, ok := matchCustomSite(u); ok {
		t.Error("norules.example: expected no metadata rules, so yt-dlp gets its metadata")
	}
}

// TestYtDLPSiteArgs tests yt-dlp getting the impersonate target from the URL's site rule (Chrome with FlareSolverr,
// plus its user agent), without overriding the user's own arguments.
func TestYtDLPSiteArgs(t *testing.T) {
	t.Cleanup(ResetCustomSites)
	ResetCustomSites()
	brave, none := consts.ImpersonateBrave, consts.ImpersonateNone
	RegisterCustomSites([]models.SiteScraper{
		{Domain: "brave.example", Impersonate: &brave},
		{Domain: "plain.example", Impersonate: &none, FlareSolverr: true},
	})
	fs := models.NewFlareSolverrSolution("UA/1", 3, nil)

	tests := []struct {
		name     string
		url      string
		fs       *models.FlareSolverrSolution
		existing []string
		want     []string
		wantGen  int
	}{
		{"built-in rule", "https://rumble.com/v1-a.html", nil, nil, []string{"--impersonate", consts.HTMLRumble.Impersonate}, 0},
		{"no yt-dlp target for brave", "https://brave.example/v", nil, nil, []string{"--impersonate", "chrome"}, 0},
		{"no rule", "https://norules.example/v", nil, nil, nil, 0},
		{"FlareSolverr", "https://plain.example/v", fs, nil, []string{"--impersonate", "chrome", "--user-agent", "UA/1"}, 3},
		{"FlareSolverr unavailable", "https://plain.example/v", nil, nil, nil, 0},
		{"user's own arguments", "https://plain.example/v", fs, []string{"--impersonate=safari", "--user-agent", "Mine"}, nil, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gen := YtDLPSiteArgs(tt.url, tt.fs, tt.existing)
			if !slices.Equal(got, tt.want) || gen != tt.wantGen {
				t.Errorf("got %v (gen %d), want %v (gen %d)", got, gen, tt.want, tt.wantGen)
			}
		})
	}
}
