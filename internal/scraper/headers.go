package scraper

import (
	"maps"
	"slices"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"

	fhttp "github.com/bogdanfinn/fhttp"
)

// setRequestHeaders adds browser headers to an impersonated request: headers copied from a real browser if there are
// any (e.g. FlareSolverr's), or otherwise ones generated for the impersonated browser. Other browsers are left as is.
func setRequestHeaders(h fhttp.Header, impersonate consts.Impersonate, fsHeaders [][2]string) {
	switch {
	case len(fsHeaders) > 0:
		setFlareSolverrHeaders(h, fsHeaders)
		logger.Pl.D(1, "Sending FlareSolverr's browser headers for impersonate %q:\n\n%s", impersonate, describeHeaders(h))
	case impersonate == consts.ImpersonateChrome:
		setChromeHeaders(h)
		logger.Pl.D(1, "Sending built in Chrome browser headers for impersonate %q:\n\n%s", impersonate, describeHeaders(h))
	case impersonate == consts.ImpersonateFirefox:
		setFirefoxHeaders(h)
		logger.Pl.D(1, "Sending built in Firefox browser headers for impersonate %q:\n\n%s", impersonate, describeHeaders(h))
	default:
		logger.Pl.D(1, "No browser headers for impersonate %q, sending only the user agent and Colly's defaults:\n\n%s", impersonate, describeHeaders(h))
	}
}

// describeHeaders returns the headers in the order they'll be sent, as "name: value", with cookie values left out.
func describeHeaders(h fhttp.Header) string {
	var lines []string
	seen := map[string]bool{fhttp.HeaderOrderKey: true, fhttp.PHeaderOrderKey: true}
	add := func(name string) {
		key := fhttp.CanonicalHeaderKey(name)
		if seen[key] {
			return
		}
		seen[key] = true
		for _, v := range h[key] {
			if key == "Cookie" {
				v = cookieNamesOnly(v)
			}
			lines = append(lines, strings.ToLower(key)+": "+v)
		}
	}
	for _, name := range h[fhttp.HeaderOrderKey] {
		add(name)
	}
	for _, name := range slices.Sorted(maps.Keys(h)) { // Any not in the order list.
		add(name)
	}
	return strings.Join(lines, " | ")
}

// cookieNamesOnly turns a Cookie header's "a=1; b=2" into "a; b".
func cookieNamesOnly(cookie string) string {
	parts := strings.Split(cookie, ";")
	for i, p := range parts {
		name, _, _ := strings.Cut(strings.TrimSpace(p), "=")
		parts[i] = name
	}
	return strings.Join(parts, "; ")
}
