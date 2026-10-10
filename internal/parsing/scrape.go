package parsing

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"

	"github.com/spf13/viper"
)

// scrapeSiteConfig mirrors a single site entry in a user-supplied scrape config file.
type scrapeSiteConfig struct {
	Domain       string                 `mapstructure:"domain"`
	Impersonate  *string                `mapstructure:"impersonate"`
	FlareSolverr bool                   `mapstructure:"flaresolverr"`
	Selectors    []scrapeSelectorConfig `mapstructure:"selectors"`
	Crawl        *scrapeCrawlConfig     `mapstructure:"crawl"`
}

// scrapeCrawlConfig mirrors a site's channel page crawl rule in a scrape config file.
type scrapeCrawlConfig struct {
	Selector   string `mapstructure:"selector"`
	Attr       string `mapstructure:"attr"`
	JSONPath   string `mapstructure:"json_path"`
	Include    string `mapstructure:"include"`
	Exclude    string `mapstructure:"exclude"`
	StripQuery bool   `mapstructure:"strip_query"`
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
//	    crawl:
//	      selector: "a[href]"
//	      attr: href
//	      include: "/video/"
func ParseScrapeSelectorsFile(f string) ([]models.SiteScraper, error) {
	return parseScrapeSelectorsFile(f, f)
}

// parseScrapeSelectorsFile loads scrape rules from file f, naming it as name in messages (e.g. the file a temporary
// copy is being checked for).
func parseScrapeSelectorsFile(f, name string) ([]models.SiteScraper, error) {
	v := viper.New()
	if err := file.LoadConfigFile(v, f); err != nil {
		return nil, fmt.Errorf("failed to load scrape config file %q: %w", name, err)
	}

	var raw struct {
		Sites []scrapeSiteConfig `mapstructure:"sites"`
	}
	if err := v.Unmarshal(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse scrape config file %q: %w", name, err)
	}

	sites := make([]models.SiteScraper, 0, len(raw.Sites))
	for _, s := range raw.Sites {
		// Domain is the site's hostname (e.g., "example.com"). It is required and must be unique.
		domain := strings.ToLower(strings.TrimSpace(s.Domain))
		if domain == "" {
			return nil, fmt.Errorf("scrape config file %q: site entry missing domain", name)
		}

		// Impersonate is optional and intended to simulate a real browser if provided.
		// Unset keeps any built-in value, while "" or "none" explicitly disables impersonation.
		var impersonate *consts.Impersonate
		if s.Impersonate != nil {
			imp := consts.Impersonate(strings.ToLower(strings.TrimSpace(*s.Impersonate)))
			if imp == "none" {
				imp = consts.ImpersonateNone
			}
			if imp == consts.ImpersonateOpera {
				logger.Pl.W("Scrape config file %q: Site %q uses impersonate value %q which is unstable. Using %q instead.", name, domain, imp, consts.ImpersonateChrome)
				imp = consts.ImpersonateChrome
			}
			if _, valid := consts.ValidImpersonateValues[imp]; !valid {
				return nil, fmt.Errorf("scrape config file %q: site %q has invalid impersonate value %q", name, domain, imp)
			}
			impersonate = &imp
		}

		// A site needs at least one of metadata selectors, a crawl rule, impersonate, or flaresolverr.
		// Each selector must have a non-empty field and selector value.
		if len(s.Selectors) == 0 && s.Crawl == nil && s.Impersonate == nil && !s.FlareSolverr {
			return nil, fmt.Errorf("scrape config file %q: site %q sets nothing (needs selectors, a crawl rule, impersonate, or flaresolverr)", name, domain)
		}

		crawl, err := parseCrawlConfig(s.Crawl)
		if err != nil {
			return nil, fmt.Errorf("scrape config file %q: site %q: %w", name, domain, err)
		}

		// Build the list of selectors for this site, validating each one.
		selectors := make([]models.ScrapeSelectors, 0, len(s.Selectors))
		for _, sel := range s.Selectors {
			field := strings.TrimSpace(sel.Field)
			selector := strings.TrimSpace(sel.Selector)
			if field == "" || selector == "" {
				return nil, fmt.Errorf("scrape config file %q: site %q has a selector missing field or selector value", name, domain)
			}
			selectors = append(selectors, models.ScrapeSelectors{
				Field:    field,
				Selector: selector,
				Attr:     strings.TrimSpace(sel.Attr),
			})
		}

		// Add the validated site to the list of sites to return.
		sites = append(sites, models.SiteScraper{
			Domain:       domain,
			Selectors:    selectors,
			Crawl:        crawl,
			Impersonate:  impersonate,
			FlareSolverr: s.FlareSolverr,
		})
	}

	logger.Pl.I("Loaded %d custom scrape site(s) from %q", len(sites), name)
	return sites, nil
}

// SaveScrapeSelectorsFile validates content as a scrape config file, and if valid, replaces the file at path with it.
//
// Validated as a temporary file beside path (with the same extension, so it is read as the same format), then renamed
// into place, so an invalid edit never touches the existing file.
func SaveScrapeSelectorsFile(path, content string) ([]models.SiteScraper, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*"+filepath.Ext(path))
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file for %q: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // No-op once renamed into place.

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("failed to write temporary file for %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("failed to write temporary file for %q: %w", path, err)
	}
	if info, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmpPath, info.Mode().Perm())
	}

	sites, err := parseScrapeSelectorsFile(tmpPath, path)
	if err != nil {
		return nil, errors.New(strings.ReplaceAll(err.Error(), tmpPath, path))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, fmt.Errorf("failed to save %q: %w", path, err)
	}
	return sites, nil
}

// parseCrawlConfig validates a crawl rule and compiles its URL filters.
func parseCrawlConfig(c *scrapeCrawlConfig) (*models.CrawlRule, error) {
	if c == nil {
		return nil, nil
	}

	rule := &models.CrawlRule{
		Selector:   strings.TrimSpace(c.Selector),
		Attr:       strings.TrimSpace(c.Attr),
		JSONPath:   strings.TrimSpace(c.JSONPath),
		StripQuery: c.StripQuery,
	}
	if rule.Selector == "" {
		return nil, fmt.Errorf("crawl rule is missing a selector")
	}

	var err error
	if include := strings.TrimSpace(c.Include); include != "" {
		if rule.Include, err = regexp.Compile(include); err != nil {
			return nil, fmt.Errorf("invalid crawl include pattern %q: %w", include, err)
		}
	}
	if exclude := strings.TrimSpace(c.Exclude); exclude != "" {
		if rule.Exclude, err = regexp.Compile(exclude); err != nil {
			return nil, fmt.Errorf("invalid crawl exclude pattern %q: %w", exclude, err)
		}
	}
	return rule, nil
}
