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
		headers     [][2]string
	)
	if rules, ok := MatchAny(pageURL); ok {
		impersonate, userAgent = consts.Impersonate(rules.Impersonate), rules.UserAgent
	}

	// FlareSolverr's user agent is used for the download, so yt-dlp's --impersonate is set to Chrome to match it.
	if fs != nil {
		solve := fs.Current()
		userAgent, headers, fsGen = solve.UserAgent, solve.Headers, solve.Gen
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

	// Add FlareSolverr's browser headers, which replace the impersonated browser's own (keeping their order), and the
	// user's own for the same headers. The user agent is set above, and yt-dlp manages the encoding it accepts.
	for _, kv := range headers {
		if kv[0] == "user-agent" || kv[0] == "accept-encoding" {
			continue
		}

		if stripped, found := stripHeaderArg(args, kv[0]); found {
			logger.Pl.W("Overriding custom yt-dlp --add-headers %q for %q with FlareSolverr's", kv[0], pageURL)
			args = stripped
		}
		args = append(args, command.AddHeaders, kv[0]+":"+kv[1])
	}
	return args, fsGen
}

// headerArgName returns the lowercased header name in an --add-headers value (e.g. "Accept-Language:en" gives
// "accept-language").
func headerArgName(value string) string {
	name, _, _ := strings.Cut(value, ":")
	return strings.ToLower(strings.TrimSpace(name))
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
