package consts

import "time"

// Impersonate defines the type of browser to impersonate during scraping.
type Impersonate string

const (
	ImpersonateNone    Impersonate = ""
	ImpersonateChrome  Impersonate = "chrome"
	ImpersonateFirefox Impersonate = "firefox"
	ImpersonateSafari  Impersonate = "safari"
	ImpersonateOpera   Impersonate = "opera"
	ImpersonateBrave   Impersonate = "brave"
)

// Time limits for FlareSolverr to solve a site's challenge (set per site with flaresolverr_timeout).
const (
	FlareSolverrDefaultTimeout = 60 * time.Second
	FlareSolverrMinTimeout     = 10 * time.Second
	FlareSolverrMaxTimeout     = 300 * time.Second
)

// ValidImpersonateValues is a set of valid impersonation values for scraping.
var ValidImpersonateValues = map[Impersonate]struct{}{
	ImpersonateNone:    {},
	ImpersonateChrome:  {},
	ImpersonateFirefox: {},
	ImpersonateSafari:  {},
	ImpersonateOpera:   {},
	ImpersonateBrave:   {},
}
