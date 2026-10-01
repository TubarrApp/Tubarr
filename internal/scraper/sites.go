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
	sites := make(map[string]consts.HTMLMetadataQuery, 4)
	if dev.CensoredTVUseCustom {
		sites[consts.HTMLCensored.Site] = consts.HTMLCensored
	}
	if dev.BitchuteComUseCustom {
		sites[consts.HTMLBitchute.Site] = consts.HTMLBitchute
	}
	if dev.OdyseeComUseCustom {
		sites[consts.HTMLOdysee.Site] = consts.HTMLOdysee
	}
	if dev.RumbleComUseCustom {
		sites[consts.HTMLRumble.Site] = consts.HTMLRumble
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
// User-defined sites take priority over built-in rules for the same domain.
func RegisterCustomSites(sites []models.SiteScraper) {
	customSitesMu.Lock()
	defer customSitesMu.Unlock()

	for _, s := range sites {
		rules := make([]consts.HTMLMetadataRule, 0, len(s.Selectors))
		for _, sel := range s.Selectors {
			rules = append(rules, consts.HTMLMetadataRule{
				Name:     sel.Field,
				Selector: sel.Selector,
				Attr:     sel.Attr,
			})
		}
		customSites[strings.ToLower(s.Domain)] = consts.HTMLMetadataQuery{
			Site:        s.Domain,
			Rules:       rules,
			Impersonate: string(s.Impersonate),
		}
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

// matchCustomSite finds registered scraping rules for a URL based on domain match.
func matchCustomSite(url string) (consts.HTMLMetadataQuery, bool) {
	customSitesMu.RLock()
	defer customSitesMu.RUnlock()

	for domain, query := range customSites {
		if strings.Contains(url, domain) {
			return query, true
		}
	}
	return consts.HTMLMetadataQuery{}, false
}
