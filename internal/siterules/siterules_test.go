package siterules

import (
	"io"
	"os"
	"regexp"
	"slices"
	"testing"
	"tubarr/internal/dev"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// TestMain sets up the test environment for the siterules package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// TestBuiltinSitesToggles tests that each site's metadata and crawl toggles gate its built-in rules.
func TestBuiltinSitesToggles(t *testing.T) {
	builtins := builtinSites()
	checks := []struct {
		query  models.SiteRules
		toggle dev.BuiltinScraper
	}{
		{Censored, dev.CensoredTV},
		{Bitchute, dev.BitchuteCom},
		{Odysee, dev.OdyseeCom},
		{Rumble, dev.RumbleCom},
	}
	for _, c := range checks {
		got := builtins[c.query.Site]
		if hasRules := len(got.Metadata) > 0; hasRules != c.toggle.Metadata {
			t.Errorf("%s: metadata rules registered = %v, toggle = %v", c.query.Site, hasRules, c.toggle.Metadata)
		}
		if hasCrawl := got.Crawl != nil; hasCrawl != c.toggle.Crawl {
			t.Errorf("%s: crawl rule registered = %v, toggle = %v", c.query.Site, hasCrawl, c.toggle.Crawl)
		}
	}
}

// TestRegisterMerge tests that user metadata selectors and crawl rules each only replace their built-in counterpart.
func TestRegisterMerge(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	userCrawl := &models.CrawlRule{Selector: "a.episode", Attr: "href", Include: regexp.MustCompile(`/e/`)}
	Register([]models.SiteScraper{
		{Domain: "censored.tv", Selectors: []models.ScrapeSelectors{{Field: "title", Selector: "h1"}}},
		{Domain: "bitchute.com", Crawl: userCrawl},
	})

	sitesMu.RLock()
	censored, bitchute := sites["censored.tv"], sites["bitchute.com"]
	sitesMu.RUnlock()

	if len(censored.Metadata) != 1 || censored.Metadata[0].Selector != "h1" {
		t.Errorf("censored.tv: expected user metadata selectors, got %+v", censored.Metadata)
	}
	if dev.CensoredTV.Crawl && censored.Crawl != Censored.Crawl {
		t.Errorf("censored.tv: expected built-in crawl rule to be kept, got %+v", censored.Crawl)
	}
	if bitchute.Crawl != userCrawl {
		t.Errorf("bitchute.com: expected user crawl rule, got %+v", bitchute.Crawl)
	}
}

// TestRegisterImpersonate tests that only an explicitly set impersonate value overrides the built-in one.
func TestRegisterImpersonate(t *testing.T) {
	t.Cleanup(Reset)
	none, firefox, chrome := consts.ImpersonateNone, consts.ImpersonateFirefox, consts.ImpersonateChrome
	tests := []struct {
		name        string
		impersonate *consts.Impersonate
		want        string
	}{
		{"unset keeps built-in", nil, Rumble.Impersonate},
		{"explicit none disables", &none, ""},
		{"explicit value replaces", &firefox, "firefox"},
		{"explicit value replaces", &chrome, "chrome"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset()
			Register([]models.SiteScraper{{Domain: "rumble.com", Impersonate: tt.impersonate}})

			sitesMu.RLock()
			got := sites["rumble.com"].Impersonate
			sitesMu.RUnlock()
			if got != tt.want {
				t.Errorf("got impersonate %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRegisterSettingsOnly tests that a site entry setting only flaresolverr keeps the built-in rules,
// and works for a site with no rules (which uses yt-dlp, with FlareSolverr's cookies).
func TestRegisterSettingsOnly(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Register([]models.SiteScraper{
		{Domain: "rumble.com", FlareSolverr: true},
		{Domain: "norules.example", FlareSolverr: true},
	})

	sitesMu.RLock()
	rumble := sites["rumble.com"]
	sitesMu.RUnlock()
	if !rumble.FlareSolverr || rumble.Impersonate != Rumble.Impersonate {
		t.Errorf("rumble.com: expected FlareSolverr on and built-in impersonation kept, got %+v", rumble)
	}
	if dev.RumbleCom.Crawl && rumble.Crawl != Rumble.Crawl {
		t.Errorf("rumble.com: expected the built-in crawl rule to be kept, got %+v", rumble.Crawl)
	}

	u := "https://norules.example/channel"
	if !UsesFlareSolverr(u) {
		t.Error("norules.example: expected FlareSolverr to be used")
	}
	if _, ok := MatchCrawl(u); ok {
		t.Error("norules.example: expected no crawl rule, so yt-dlp crawls it")
	}
	if _, ok := MatchMetadata(u); ok {
		t.Error("norules.example: expected no metadata rules, so yt-dlp gets its metadata")
	}
}

// TestYtDLPArgs tests yt-dlp getting the impersonate target from the URL's site rule (Chrome with FlareSolverr,
// plus its user agent). The user's own arguments are kept, except for FlareSolverr sites.
func TestYtDLPArgs(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	brave, none := consts.ImpersonateBrave, consts.ImpersonateNone
	Register([]models.SiteScraper{
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
		{"built-in rule", "https://rumble.com/v1-a.html", nil, nil, []string{"--impersonate", Rumble.Impersonate}, 0},
		{"no yt-dlp target for brave", "https://brave.example/v", nil, nil, []string{"--impersonate", "chrome"}, 0},
		{"no rule", "https://norules.example/v", nil, nil, nil, 0},
		{"FlareSolverr", "https://plain.example/v", fs, nil, []string{"--impersonate", "chrome", "--user-agent", "UA/1"}, 3},
		{"FlareSolverr unavailable", "https://plain.example/v", nil, nil, nil, 0},
		{"user's own arguments", "https://brave.example/v", nil, []string{"--impersonate=safari", "--user-agent", "Mine"}, nil, 0},
		{"FlareSolverr overrides user's own", "https://plain.example/v", fs, []string{"--impersonate=safari", "--user-agent", "Mine"}, []string{"--impersonate", "chrome", "--user-agent", "UA/1"}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gen := YtDLPArgs(tt.url, tt.fs, tt.existing)
			if !slices.Equal(got, tt.want) || gen != tt.wantGen {
				t.Errorf("got %v (gen %d), want %v (gen %d)", got, gen, tt.want, tt.wantGen)
			}
		})
	}
}
