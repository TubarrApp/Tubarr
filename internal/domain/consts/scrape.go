package consts

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

// ValidImpersonateValues is a set of valid impersonation values for scraping.
var ValidImpersonateValues = map[Impersonate]struct{}{
	ImpersonateNone:    {},
	ImpersonateChrome:  {},
	ImpersonateFirefox: {},
	ImpersonateSafari:  {},
	ImpersonateOpera:   {},
	ImpersonateBrave:   {},
}
