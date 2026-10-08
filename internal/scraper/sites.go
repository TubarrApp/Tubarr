package scraper

import (
	"sort"
	"strings"
	"sync"
	"tubarr/internal/dev"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

var (
	customSitesMu sync.RWMutex
	customSites   = builtinSites()
)

// builtinSites returns the compile-time default scraping rules, gated by dev toggles.
func builtinSites() map[string]consts.HTMLMetadataQuery {
	builtins := []struct {
		query  consts.HTMLMetadataQuery
		toggle dev.BuiltinScraper
	}{
		{consts.HTMLCensored, dev.CensoredTV},
		{consts.HTMLBitchute, dev.BitchuteCom},
		{consts.HTMLOdysee, dev.OdyseeCom},
		{consts.HTMLRumble, dev.RumbleCom},
	}

	sites := make(map[string]consts.HTMLMetadataQuery, len(builtins))
	for _, b := range builtins {
		q := b.query
		if !b.toggle.Metadata {
			q.Rules = nil
		}
		if !b.toggle.Crawl {
			q.Crawl = nil
		}
		if len(q.Rules) > 0 || q.Crawl != nil {
			sites[q.Site] = q
		}
	}
	return sites
}

// ResetCustomSites clears any previously registered user-defined sites, reverting to built-in defaults.
func ResetCustomSites() {
	customSitesMu.Lock()
	defer customSitesMu.Unlock()
	customSites = builtinSites()
}

// RegisterCustomSites loads user-defined scrape site selectors into the global registry.
//
// User-defined metadata selectors and crawl rules each replace the built-in ones for the same domain.
func RegisterCustomSites(sites []models.SiteScraper) {
	customSitesMu.Lock()
	defer customSitesMu.Unlock()

	for _, s := range sites {
		domain := strings.ToLower(s.Domain)
		query := customSites[domain]
		query.Site = s.Domain

		if len(s.Selectors) > 0 {
			query.Rules = make([]consts.HTMLMetadataRule, 0, len(s.Selectors))
			for _, sel := range s.Selectors {
				query.Rules = append(query.Rules, consts.HTMLMetadataRule{
					Name:     sel.Field,
					Selector: sel.Selector,
					Attr:     sel.Attr,
				})
			}
		}
		if s.Crawl != nil {
			query.Crawl = s.Crawl
		}
		if s.Impersonate != nil {
			query.Impersonate = string(*s.Impersonate)
		}
		if s.FlareSolverr {
			query.FlareSolverr = true
		}
		customSites[domain] = query
	}

	logger.Pl.I("Registered %d custom scrape site(s)", len(sites))
}

// ListRegisteredSites returns all currently registered scrape sites (built-in and custom),
// sorted by domain, for display in the web UI.
func ListRegisteredSites() []consts.HTMLMetadataQuery {
	customSitesMu.RLock()
	defer customSitesMu.RUnlock()

	sites := make([]consts.HTMLMetadataQuery, 0, len(customSites))
	for _, query := range customSites {
		sites = append(sites, query)
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Site < sites[j].Site })
	return sites
}

// matchCustomSite finds registered metadata scraping rules for a URL based on domain match.
func matchCustomSite(url string) (consts.HTMLMetadataQuery, bool) {
	return matchSite(url, func(q consts.HTMLMetadataQuery) bool { return len(q.Rules) > 0 })
}

// matchCrawlSite finds a registered channel page crawl rule for a URL based on domain match.
func matchCrawlSite(url string) (consts.HTMLMetadataQuery, bool) {
	return matchSite(url, func(q consts.HTMLMetadataQuery) bool { return q.Crawl != nil })
}

// matchSite finds a registered site for a URL whose rules satisfy the filter.
func matchSite(url string, filter func(consts.HTMLMetadataQuery) bool) (consts.HTMLMetadataQuery, bool) {
	customSitesMu.RLock()
	defer customSitesMu.RUnlock()

	for domain, query := range customSites {
		if strings.Contains(url, domain) && filter(query) {
			return query, true
		}
	}
	return consts.HTMLMetadataQuery{}, false
}
