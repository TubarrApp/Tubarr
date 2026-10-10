package siterules

import (
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// YtDLPArgs returns args with the yt-dlp arguments for the site rule matching pageURL added: its impersonate target and
// user agent, or Chrome and FlareSolverr's user agent if fs is set (its cookies are in the cookie file). Also returns
// fs's generation, for a later Refresh.
//
// The user's own --impersonate and --user-agent in args are kept, except for FlareSolverr sites, where they are removed
// and replaced, since FlareSolverr's cookies only work with its user agent and a Chrome fingerprint.
func YtDLPArgs(pageURL string, fs *models.FlareSolverrSolution, args []string) (_ []string, fsGen int) {
	var (
		impersonate consts.Impersonate
		userAgent   string
	)
	if rules, ok := MatchAny(pageURL); ok {
		impersonate, userAgent = consts.Impersonate(rules.Impersonate), rules.UserAgent
	}

	// FlareSolverr's user agent is used for the download, so yt-dlp's --impersonate is set to Chrome to match it.
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

	// Add the target and user agent, replacing the user's own for FlareSolverr sites.
	if target != "" {
		if fs != nil && hasArg(args, command.Impersonate) {
			logger.Pl.W("Overriding custom yt-dlp --impersonate for %q with %q, to match FlareSolverr's user agent", pageURL, target)
			args = stripArg(args, command.Impersonate)
		}
		if !hasArg(args, command.Impersonate) {
			args = append(args, command.Impersonate, target)
		}
	}
	if userAgent != "" {
		if fs != nil && hasArg(args, command.UserAgent) {
			logger.Pl.W("Overriding custom yt-dlp --user-agent for %q with FlareSolverr's %q", pageURL, userAgent)
			args = stripArg(args, command.UserAgent)
		}
		if !hasArg(args, command.UserAgent) {
			args = append(args, command.UserAgent, userAgent)
		}
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

// stripArg removes flag from args, either alone or as flag=value. Returns the new slice.
func stripArg(args []string, flag string) []string {
	var newArgs []string

	var deleteNext bool
	for _, a := range args {
		if deleteNext {
			deleteNext = false
			if !strings.HasPrefix(a, "-") {
				// This arg is the value for the flag, so delete it.
				continue
			}
		}
		if strings.HasPrefix(a, flag+"=") {
			continue
		}
		if a == flag {
			deleteNext = true
			continue
		}
		newArgs = append(newArgs, a)
	}
	return newArgs
}
