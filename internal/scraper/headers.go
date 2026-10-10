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
func setRequestHeaders(h fhttp.Header, impersonate consts.Impersonate, copied [][2]string) {
	switch {
	case len(copied) > 0:
		setBrowserHeaders(h, copied)
	case impersonate == consts.ImpersonateChrome:
		setChromeHeaders(h)
	case impersonate == consts.ImpersonateFirefox:
		setFirefoxHeaders(h)
	default:
		logger.Pl.I("No browser headers for impersonate %q, sending only the user agent and Colly's defaults", impersonate)
	}
	logger.Pl.I("Sending headers: %s", describeHeaders(h))
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

// setBrowserHeaders adds headers copied from a real browser's request (e.g. FlareSolverr's), in the browser's order.
// Headers already set (e.g. the user agent and cookies) are kept, except Colly's default "Accept: */*".
func setBrowserHeaders(h fhttp.Header, browser [][2]string) {
	if h.Get("Accept") == "*/*" {
		h.Del("Accept")
	}

	order := []string{"host"}
	for _, kv := range browser {
		if h.Get(kv[0]) == "" {
			h.Set(kv[0], kv[1])
		}
		if kv[0] == "priority" {
			order = append(order, "cookie") // Chrome sends cookies just before priority.
		}
		order = append(order, kv[0])
	}
	if !slices.Contains(order, "cookie") {
		order = append(order, "cookie")
	}
	h[fhttp.HeaderOrderKey] = order
}
