package siterules

import (
	"fmt"
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// AddSiteRulesToYTDLP returns args with the yt-dlp arguments for the site rule matching pageURL added: its impersonate target and
// user agent, or Chrome and FlareSolverr's user agent if fs is set (its cookies are in the cookie file). Also returns
// fs's generation, for a later Refresh.
//
// The user's own --impersonate and --user-agent in args are kept, except for FlareSolverr sites, where they are removed
// and replaced, since FlareSolverr's cookies only work with its user agent and a Chrome fingerprint.
func AddSiteRulesToYTDLP(pageURL string, fs *models.FlareSolverrSolution, args []string) (_ []string, fsGen int) {
	var (
		impersonate consts.Impersonate
		userAgent   string
		fsHeaders   [][2]string
	)

	// Site rule impersonation and user agent.
	if rules, ok := MatchAny(pageURL); ok {
		impersonate, userAgent = consts.Impersonate(rules.Impersonate), rules.UserAgent
	}

	// FlareSolverr's user agent is used for the download, so yt-dlp's --impersonate is set to Chrome to match it.
	//
	// Overrides the above site rule impersonation and user agent to prioritize FlareSolverr's since its cookies
	// only work with its user agent and a Chrome fingerprint.
	if fs != nil {
		logger.Pl.D(1, "FlareSolverr solution found for %q, using its user agent and headers", pageURL)
		solve := fs.Current()
		userAgent, fsHeaders, fsGen = solve.UserAgent, solve.Headers, solve.Gen
		impersonate = consts.ImpersonateChrome
	}

	// Impersonate setting from site rules.
	var impersonateStr string
	switch impersonate {
	case consts.ImpersonateNone:
		impersonateStr = ""
	case consts.ImpersonateBrave, consts.ImpersonateOpera: // yt-dlp has no Brave or Opera targets.
		logger.Pl.W("yt-dlp has no %q impersonate target, using \"chrome\" instead", impersonate)
		impersonateStr = string(consts.ImpersonateChrome)
	default:
		impersonateStr = string(impersonate)
	}
	logger.Pl.D(2, "Got target %q and user agent %q for %q", impersonateStr, userAgent, pageURL)

	// Add the impersonate target and user agent, replacing the user's own for FlareSolverr sites.
	if impersonateStr != "" {
		if fs != nil && hasFlag(args, command.Impersonate) {
			logger.Pl.W("Overriding custom yt-dlp --impersonate for %q with %q, to match FlareSolverr's user agent", pageURL, impersonateStr)
			args = stripFlagArgs(args, command.Impersonate, pageURL)
		}
		if !hasFlag(args, command.Impersonate) {
			args = append(args, command.Impersonate, impersonateStr)
		}
	}
	if userAgent != "" {
		if fs != nil && hasFlag(args, command.UserAgent) {
			logger.Pl.W("Overriding custom yt-dlp --user-agent for %q with FlareSolverr's %q", pageURL, userAgent)
			args = stripFlagArgs(args, command.UserAgent, pageURL)
		}
		if !hasFlag(args, command.UserAgent) {
			args = append(args, command.UserAgent, userAgent)
		}
	}

	// Add FlareSolverr's browser headers, which replace the impersonated browser's own (keeping their order), and the
	// user's own for the same headers. The user agent is set above, and yt-dlp manages the encoding it accepts.
	if len(fsHeaders) > 0 {
		logger.Pl.D(2, "Adding FlareSolverr's browser headers for %q (except its user-agent and accept-encoding)", pageURL)
	}
	for _, kv := range fsHeaders {
		logger.Pl.D(4, "FlareSolverr header %q: %q", kv[0], kv[1]) // kv is a [2]string, so kv[1] is safe.
		if kv[0] == "user-agent" || kv[0] == "accept-encoding" {
			continue
		}

		if stripped, found := stripHeaderArg(args, kv[0]); found {
			logger.Pl.W("Overriding custom yt-dlp --add-headers %q for %q with FlareSolverr's", kv[0], pageURL)
			args = stripped
		}
		logger.Pl.D(3, "Adding FlareSolverr header %q for %q", kv[0], pageURL)
		args = append(args, command.AddHeaders, kv[0]+":"+kv[1]) // --add-headers is the documented form, though yt-dlp accepts --add-header too.
	}

	return args, fsGen
}

// stripFlagArgs removes flag from args, either alone or as flag=value. Returns the new slice.
func stripFlagArgs(args []string, flag, pageURL string) (newArgs []string) {
	var (
		deleteNext           bool
		lastFlag             string
		valuelessFlagWarning = fmt.Sprintf("Flag %q for %q had no value.", flag, pageURL)
	)

	// Iterate through args, keeping all except the flag and its value (if any).
	for _, a := range args {

		// If the previous arg was the flag, this one is its value, so delete it.
		if deleteNext {
			deleteNext = false
			if !strings.HasPrefix(a, "-") {
				// This arg is the value for the flag, so delete it.
				logger.Pl.D(3, "Removing flag %q's value %q for %q", lastFlag, a, pageURL)
				lastFlag = ""
				continue
			}
			logger.Pl.W(valuelessFlagWarning)
			lastFlag = ""
		}

		// Discard "flag=value" or "flag" arguments.
		if strings.HasPrefix(a, flag+"=") {
			continue
		}
		if a == flag {
			deleteNext = true
			lastFlag = a
			continue
		}

		// Keep all other arguments.
		newArgs = append(newArgs, a)
	}

	// If the last arg was the flag, it had no value, so warn.
	if deleteNext {
		logger.Pl.W(valuelessFlagWarning)
	}

	// Return the new args slice without the unwanted flag/its value.
	return newArgs
}

// stripHeaderArg removes --add-headers (or --add-header) arguments setting the header name from args, either as
// "flag name:value" or "flag=name:value". Other headers are kept. Returns the new slice, and whether any were removed.
func stripHeaderArg(args []string, name string) (newArgs []string, found bool) {
	for i := 0; i < len(args); i++ {
		flag, value, joined := strings.Cut(args[i], "=")
		if flag != command.AddHeaders && flag != command.AddHeader {
			newArgs = append(newArgs, args[i])
			continue
		}
		if !joined && i+1 < len(args) {
			value = args[i+1]
		}
		if headerArgName(value) != name {
			newArgs = append(newArgs, args[i])
			continue
		}
		found = true
		if !joined {
			i++ // Skip the value too.
		}
	}
	return newArgs, found
}

// headerArgName returns the lowercased header name in an --add-headers value (e.g. "Accept-Language:en" gives
// "accept-language").
func headerArgName(value string) string {
	name, _, _ := strings.Cut(value, ":")
	return strings.ToLower(strings.TrimSpace(name))
}

// hasFlag reports whether args contains flag, either alone or as flag=value.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}
