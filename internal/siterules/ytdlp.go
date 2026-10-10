package siterules

import (
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// YtDLPArgs returns the yt-dlp arguments for the site rule matching pageURL: its impersonate target (Chrome when
// using FlareSolverr, to match its browser), and FlareSolverr's user agent if fs is set (its cookies are in the cookie
// file). Also returns fs's generation, for a later Refresh.
//
// Skips any already existent argument (e.g. a user's own --impersonate).
func YtDLPArgs(pageURL string, fs *models.FlareSolverrSolution, existing []string) (args []string, fsGen int) {
	var impersonate consts.Impersonate
	if rules, ok := MatchAny(pageURL); ok {
		impersonate = consts.Impersonate(rules.Impersonate)
	}

	// FlareSolverr's user agent is used for the download, so yt-dlp's --impersonate is set to Chrome to match it.
	var userAgent string
	if fs != nil {
		userAgent, fsGen = fs.Current()
		impersonate = consts.ImpersonateChrome
	}

	// Impersonate setting from site rules.
	var target string
	switch impersonate {
	case consts.ImpersonateNone:
		target = ""
	case consts.ImpersonateBrave, consts.ImpersonateOpera: // yt-dlp has no Brave or Opera targets.
		logger.Pl.W("yt-dlp has no %q impersonate target, using \"chrome\" instead", impersonate)
		target = string(consts.ImpersonateChrome)
	default:
		target = string(impersonate)
	}

	// Add the target and user agent, unless the user set their own. For FlareSolverr sites, these replace the user's
	// own, since its cookies only work with its user agent and a Chrome fingerprint (yt-dlp uses the last value given).
	if target != "" && (fs != nil || !hasArg(existing, command.Impersonate)) {
		if fs != nil && hasArg(existing, command.Impersonate) {
			logger.Pl.W("Overriding custom yt-dlp --impersonate for %q with %q, to match FlareSolverr's user agent", pageURL, target)
		}
		args = append(args, command.Impersonate, target)
	}
	if userAgent != "" && (fs != nil || !hasArg(existing, command.UserAgent)) {
		if fs != nil && hasArg(existing, command.UserAgent) {
			logger.Pl.W("Overriding custom yt-dlp --user-agent for %q with FlareSolverr's %q", pageURL, userAgent)
		}
		args = append(args, command.UserAgent, userAgent)
	}
	return args, fsGen
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
