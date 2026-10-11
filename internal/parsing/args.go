package parsing

import (
	"strings"
	"unicode"
)

// SplitArgs splits a command line argument string like a shell, so quoted values can contain spaces
// (e.g. `--add-headers "Referer: https://a.example"`).
//
// Single quotes keep everything inside as-is, double quotes allow \" and \\, and a backslash outside quotes keeps the next
// character as-is. An unclosed quote runs to the end of the string, and a trailing backslash is kept.
func SplitArgs(s string) []string {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool // Tracks quoted empty values ("") as arguments.
		quote   rune
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			// Like a shell, a backslash in double quotes only escapes " and \, and is kept before anything else.
			if quote == '"' && r != '"' && r != '\\' {
				cur.WriteRune('\\')
			}
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inArg = true
		case r == '\\':
			escaped = true
			inArg = true
		case unicode.IsSpace(r):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if escaped { // Trailing backslash.
		cur.WriteRune('\\')
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}
