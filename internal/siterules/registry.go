// Package siterules holds the registry of per-site rules (built-in and from a rules file): metadata selectors, channel
// crawl rules, and request settings like impersonation and FlareSolverr, used for scraping and downloading.
package siterules

import (
	"sort"
	"strings"
	"sync"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

var (
	sitesMu sync.RWMutex
	sites   = builtinSites()
)

// Reset clears any previously registered user-defined sites, reverting to built-in defaults.
func Reset() {
	sitesMu.Lock()
	defer sitesMu.Unlock()
	sites = builtinSites()
}

// Register loads user-defined site rules into the registry.
//
// User-defined metadata selectors and crawl rules each replace the built-in ones for the same domain.
func Register(scrapers []models.SiteScraper) {
	sitesMu.Lock()
	defer sitesMu.Unlock()

	for _, s := range scrapers {
		domain := strings.ToLower(s.Domain)
		rules := sites[domain]
		rules.Site = s.Domain

		if len(s.Selectors) > 0 {
			rules.Metadata = make([]models.MetadataRule, 0, len(s.Selectors))
			for _, sel := range s.Selectors {
				rules.Metadata = append(rules.Metadata, models.MetadataRule{
					Name:     sel.Field,
					Selector: sel.Selector,
					Attr:     sel.Attr,
				})
			}
		}
		if s.Crawl != nil {
			rules.Crawl = s.Crawl
		}
		if s.Impersonate != nil {
			rules.Impersonate = string(*s.Impersonate)
		}
		if s.FlareSolverr {
			rules.FlareSolverr = true
		}
		if s.FlareSolverrTimeout > 0 {
			rules.FlareSolverrTimeout = s.FlareSolverrTimeout
		}
		sites[domain] = rules
	}

	logger.Pl.I("Registered %d custom scrape site(s)", len(scrapers))
}

// List returns all currently registered sites (built-in and custom), sorted by domain, for display in the web UI.
func List() []models.SiteRules {
	sitesMu.RLock()
	defer sitesMu.RUnlock()

	list := make([]models.SiteRules, 0, len(sites))
	for _, rules := range sites {
		list = append(list, rules)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Site < list[j].Site })
	return list
}

// Match finds the registered site for a URL based on domain match, whose rules satisfy the filter.
func Match(url string, filter func(models.SiteRules) bool) (models.SiteRules, bool) {
	sitesMu.RLock()
	defer sitesMu.RUnlock()

	for domain, rules := range sites {
		if strings.Contains(url, domain) && filter(rules) {
			return rules, true
		}
	}
	return models.SiteRules{}, false
}

// MatchAny finds the registered site for a URL based on domain match.
func MatchAny(url string) (models.SiteRules, bool) {
	return Match(url, func(models.SiteRules) bool { return true })
}

// MatchMetadata finds the registered site for a URL with metadata scraping rules.
func MatchMetadata(url string) (models.SiteRules, bool) {
	return Match(url, func(r models.SiteRules) bool { return len(r.Metadata) > 0 })
}

// MatchCrawl finds the registered site for a URL with a channel page crawl rule.
func MatchCrawl(url string) (models.SiteRules, bool) {
	return Match(url, func(r models.SiteRules) bool { return r.Crawl != nil })
}

// UsesFlareSolverr reports whether the registered site for a URL is set to use FlareSolverr.
func UsesFlareSolverr(url string) bool {
	_, ok := Match(url, func(r models.SiteRules) bool { return r.FlareSolverr })
	return ok
}
