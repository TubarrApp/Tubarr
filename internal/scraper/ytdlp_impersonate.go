package scraper

import (
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// YtDLPSiteArgs returns the yt-dlp arguments for the site rule matching pageURL: its impersonate target (Chrome when
// using FlareSolverr, to match its browser), and FlareSolverr's user agent if fs is set (its cookies are in the cookie
// file). Also returns fs's generation, for a later Refresh.
//
// Skips any argument already in existing (e.g. a user's own --impersonate).
func YtDLPSiteArgs(pageURL string, fs *models.FlareSolverrSolution, existing []string) (args []string, fsGen int) {
	var impersonate consts.Impersonate
	if query, ok := matchSite(pageURL, func(consts.HTMLMetadataQuery) bool { return true }); ok {
		impersonate = consts.Impersonate(query.Impersonate)
	}

	var userAgent string
	if fs != nil {
		userAgent, fsGen = fs.Current()
		impersonate = consts.ImpersonateChrome
	}

	if target := ytDLPImpersonateTarget(impersonate); target != "" && !hasArg(existing, command.Impersonate) {
		args = append(args, command.Impersonate, target)
	}
	if userAgent != "" && !hasArg(existing, command.UserAgent) {
		args = append(args, command.UserAgent, userAgent)
	}
	return args, fsGen
}

// ytDLPImpersonateTarget returns the yt-dlp --impersonate target for a site's impersonate setting ("" for none).
func ytDLPImpersonateTarget(impersonate consts.Impersonate) string {
	switch impersonate {
	case consts.ImpersonateNone:
		return ""
	case consts.ImpersonateBrave, consts.ImpersonateOpera: // yt-dlp has no Brave or Opera targets.
		return string(consts.ImpersonateChrome)
	default:
		return string(impersonate)
	}
}

// hasArg reports whether args contains flag, either alone or as flag=value.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}
