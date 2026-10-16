package parsing

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtags"
	"github.com/spf13/viper"
)

// scrapeSiteConfig mirrors a single site entry in a user-supplied scrape config file.
type scrapeSiteConfig struct {
	Domain              string                 `mapstructure:"domain"`
	Impersonate         *string                `mapstructure:"impersonate"`
	FlareSolverr        bool                   `mapstructure:"flaresolverr"`
	UserAgent           string                 `mapstructure:"user_agent"`
	FlareSolverrTimeout *int                   `mapstructure:"flaresolverr_timeout"`
	Selectors           []scrapeSelectorConfig `mapstructure:"selectors"`
	Crawl               *scrapeCrawlConfig     `mapstructure:"crawl"`
}

// scrapeCrawlConfig mirrors a site's channel page crawl rule in a scrape config file.
type scrapeCrawlConfig struct {
	Selector      string                 `mapstructure:"selector"`
	Attr          string                 `mapstructure:"attr"`
	JSONPath      string                 `mapstructure:"json_path"`
	Include       string                 `mapstructure:"include"`
	Exclude       string                 `mapstructure:"exclude"`
	StripQuery    bool                   `mapstructure:"strip_query"`
	Item          string                 `mapstructure:"item"`
	ItemPath      string                 `mapstructure:"item_path"`
	PreviewFields []scrapeSelectorConfig `mapstructure:"preview_fields"`
}

// crawlPreviewFields are the fields a crawl rule can read for previewing videos before download.
var crawlPreviewFields = []string{sharedtags.JTitle, sharedtags.JThumbnailURL, sharedtags.JUploadDate, consts.CrawlFieldDuration}

// scrapeSelectorConfig mirrors a single selector entry for a site in a scrape config file.
type scrapeSelectorConfig struct {
	Field    string              `mapstructure:"field"`
	Selector string              `mapstructure:"selector"`
	Attr     string              `mapstructure:"attr"`
	JSONPath string              `mapstructure:"json_path"`
	Regex    string              `mapstructure:"regex"`
	Fetch    []scrapeFetchConfig `mapstructure:"fetch"`
}

// scrapeFetchConfig mirrors a selector's follow-up request in a scrape config file.
type scrapeFetchConfig struct {
	URL      string `mapstructure:"url"`
	Selector string `mapstructure:"selector"`
	Attr     string `mapstructure:"attr"`
	JSONPath string `mapstructure:"json_path"`
	Regex    string `mapstructure:"regex"`
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
	if !v.IsSet("sites") {
		return nil, fmt.Errorf("%q is not a scrape config file: it has no 'sites' key (use 'sites: []' for none)", name)
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
		if len(s.Selectors) == 0 && s.Crawl == nil && s.Impersonate == nil && !s.FlareSolverr && s.FlareSolverrTimeout == nil && strings.TrimSpace(s.UserAgent) == "" {
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
			re, err := compileOptionalRegex(sel.Regex)
			if err != nil {
				return nil, fmt.Errorf("scrape config file %q: site %q field %q: %w", name, domain, field, err)
			}
			fetch, err := parseFetchConfigs(sel.Fetch)
			if err != nil {
				return nil, fmt.Errorf("scrape config file %q: site %q field %q: %w", name, domain, field, err)
			}
			selectors = append(selectors, models.ScrapeSelectors{
				Field:    field,
				Selector: selector,
				Attr:     strings.TrimSpace(sel.Attr),
				JSONPath: strings.TrimSpace(sel.JSONPath),
				Regex:    re,
				Fetch:    fetch,
			})
		}

		// FlareSolverr time limit is optional, in seconds, and clamped to the allowed range.
		var fsTimeout time.Duration
		if s.FlareSolverrTimeout != nil {
			fsTimeout = clampFlareSolverrTimeout(name, domain, *s.FlareSolverrTimeout)
		}

		// Add the validated site to the list of sites to return.
		sites = append(sites, models.SiteScraper{
			Domain:              domain,
			Selectors:           selectors,
			Crawl:               crawl,
			Impersonate:         impersonate,
			UserAgent:           strings.TrimSpace(s.UserAgent),
			FlareSolverr:        s.FlareSolverr,
			FlareSolverrTimeout: fsTimeout,
		})
	}

	logger.Pl.I("Loaded %d custom scrape site(s) from %q", len(sites), name)
	return sites, nil
}

// clampFlareSolverrTimeout converts a site's flaresolverr_timeout (seconds) to a time limit within the allowed range,
// warning if it had to be changed.
func clampFlareSolverrTimeout(name, domain string, seconds int) time.Duration {
	timeout := time.Duration(seconds) * time.Second
	clamped := min(max(timeout, consts.FlareSolverrMinTimeout), consts.FlareSolverrMaxTimeout)
	if clamped != timeout {
		logger.Pl.W("Scrape config file %q: site %q flaresolverr_timeout of %ds is outside %s to %s, using %s",
			name, domain, seconds, consts.FlareSolverrMinTimeout, consts.FlareSolverrMaxTimeout, clamped)
	}
	return clamped
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
	defer func() { _ = os.Remove(tmpPath) }() // No-op once renamed into place.

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

// compileOptionalRegex compiles a selector or fetch step regex, returning nil if none is set.
func compileOptionalRegex(pattern string) (*regexp.Regexp, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
	}
	return re, nil
}

// parseFetchConfigs validates a selector's follow-up requests and compiles their regexes.
func parseFetchConfigs(configs []scrapeFetchConfig) ([]models.FetchStep, error) {
	if len(configs) == 0 {
		return nil, nil
	}

	steps := make([]models.FetchStep, 0, len(configs))
	for i, c := range configs {
		u := strings.TrimSpace(c.URL)
		if u == "" {
			return nil, fmt.Errorf("fetch step %d is missing a url", i+1)
		}
		re, err := compileOptionalRegex(c.Regex)
		if err != nil {
			return nil, fmt.Errorf("fetch step %d: %w", i+1, err)
		}
		steps = append(steps, models.FetchStep{
			URL:      u,
			Selector: strings.TrimSpace(c.Selector),
			Attr:     strings.TrimSpace(c.Attr),
			JSONPath: strings.TrimSpace(c.JSONPath),
			Regex:    re,
		})
	}
	return steps, nil
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
		Item:       strings.TrimSpace(c.Item),
		ItemPath:   strings.TrimSpace(c.ItemPath),
	}
	if rule.Selector == "" {
		return nil, fmt.Errorf("crawl rule is missing a selector")
	}
	if rule.ItemPath != "" && rule.JSONPath == "" {
		return nil, fmt.Errorf("crawl rule with item_path needs a json_path for each item's URL")
	}

	var err error
	if rule.PreviewFields, err = parseCrawlPreviewFields(c.PreviewFields); err != nil {
		return nil, err
	}
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

// parseCrawlPreviewFields validates a crawl rule's preview fields, which are read from the channel page without fetches.
func parseCrawlPreviewFields(configs []scrapeSelectorConfig) ([]models.MetadataRule, error) {
	fields := make([]models.MetadataRule, 0, len(configs))
	for _, c := range configs {
		name := strings.TrimSpace(c.Field)
		if !slices.Contains(crawlPreviewFields, name) {
			return nil, fmt.Errorf("crawl preview field %q is not one of %s", name, strings.Join(crawlPreviewFields, ", "))
		}
		rule := models.MetadataRule{
			Name:     name,
			Selector: strings.TrimSpace(c.Selector),
			Attr:     strings.TrimSpace(c.Attr),
			JSONPath: strings.TrimSpace(c.JSONPath),
		}
		if rule.Selector == "" && rule.JSONPath == "" {
			return nil, fmt.Errorf("crawl preview field %q needs a selector or json_path", name)
		}
		if len(c.Fetch) > 0 {
			return nil, fmt.Errorf("crawl preview field %q can't use fetch (it would request every video's page on each crawl)", name)
		}
		var err error
		if rule.Regex, err = compileOptionalRegex(c.Regex); err != nil {
			return nil, fmt.Errorf("crawl preview field %q: %w", name, err)
		}
		fields = append(fields, rule)
	}
	return fields, nil
}
