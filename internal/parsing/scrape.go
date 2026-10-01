package parsing

import (
	"fmt"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"

	"github.com/spf13/viper"
)

// scrapeSiteConfig mirrors a single site entry in a user-supplied scrape config file.
type scrapeSiteConfig struct {
	Domain      string                 `mapstructure:"domain"`
	Impersonate string                 `mapstructure:"impersonate"`
	Selectors   []scrapeSelectorConfig `mapstructure:"selectors"`
}

// scrapeSelectorConfig mirrors a single selector entry for a site in a scrape config file.
type scrapeSelectorConfig struct {
	Field    string `mapstructure:"field"`
	Selector string `mapstructure:"selector"`
	Attr     string `mapstructure:"attr"`
}

// ParseScrapeSelectorsFile loads custom site scraping rules from a user-supplied config file.
//
// Supports any format Viper supports (YAML, TOML, JSON, etc.). Expected shape:
//
//	sites:
//	  - domain: example.com
//	    impersonate: chrome
//	    selectors:
//	      - field: title
//	        selector: "h1.title"
//	      - field: description
//	        selector: "meta[name=description]"
//	        attr: content
func ParseScrapeSelectorsFile(f string) ([]models.SiteScraper, error) {
	v := viper.New()
	if err := file.LoadConfigFile(v, f); err != nil {
		return nil, fmt.Errorf("failed to load scrape config file %q: %w", f, err)
	}

	var raw struct {
		Sites []scrapeSiteConfig `mapstructure:"sites"`
	}
	if err := v.Unmarshal(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse scrape config file %q: %w", f, err)
	}

	sites := make([]models.SiteScraper, 0, len(raw.Sites))
	for _, s := range raw.Sites {
		// Domain is the site's hostname (e.g., "example.com"). It is required and must be unique.
		domain := strings.ToLower(strings.TrimSpace(s.Domain))
		if domain == "" {
			return nil, fmt.Errorf("scrape config file %q: site entry missing domain", f)
		}

		// Impersonate is optional and intended to simulate a real browser if provided. It must be one of the supported values.
		impersonate := consts.Impersonate(strings.ToLower(strings.TrimSpace(s.Impersonate)))
		if impersonate == "none" {
			impersonate = consts.ImpersonateNone
		}
		if impersonate == consts.ImpersonateOpera {
			logger.Pl.W("Scrape config file %q: Site %q uses impersonate value %q which is unstable. Using %q instead.", f, domain, impersonate, consts.ImpersonateChrome)
			impersonate = consts.ImpersonateChrome
		}
		if _, valid := consts.ValidImpersonateValues[impersonate]; !valid {
			return nil, fmt.Errorf("scrape config file %q: site %q has invalid impersonate value %q", f, domain, impersonate)
		}

		// Selectors are required and must have at least one entry. Each selector must have a non-empty field and selector value.
		if len(s.Selectors) == 0 {
			return nil, fmt.Errorf("scrape config file %q: site %q has no selectors", f, domain)
		}

		// Build the list of selectors for this site, validating each one.
		selectors := make([]models.ScrapeSelectors, 0, len(s.Selectors))
		for _, sel := range s.Selectors {
			field := strings.TrimSpace(sel.Field)
			selector := strings.TrimSpace(sel.Selector)
			if field == "" || selector == "" {
				return nil, fmt.Errorf("scrape config file %q: site %q has a selector missing field or selector value", f, domain)
			}
			selectors = append(selectors, models.ScrapeSelectors{
				Field:    field,
				Selector: selector,
				Attr:     strings.TrimSpace(sel.Attr),
			})
		}

		// Add the validated site to the list of sites to return.
		sites = append(sites, models.SiteScraper{
			Domain:      domain,
			Selectors:   selectors,
			Impersonate: consts.Impersonate(impersonate),
		})
	}

	logger.Pl.I("Loaded %d custom scrape site(s) from %q", len(sites), f)
	return sites, nil
}
