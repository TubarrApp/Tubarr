package siterules

import (
	"slices"
	"strings"
	"testing"
	"tubarr/internal/domain/command"
	"tubarr/internal/models"
)

// fuzzArgs splits a fuzz input into command line arguments, one per space.
func fuzzArgs(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, " ")
}

// isSubsequence reports whether sub's elements all appear in full, in the same order.
func isSubsequence(sub, full []string) bool {
	i := 0
	for _, a := range full {
		if i < len(sub) && sub[i] == a {
			i++
		}
	}
	return i == len(sub)
}

// argSeeds are realistic yt-dlp argument lists, including edge cases for flags without values.
var argSeeds = []string{
	"-t sleep --impersonate firefox --retries 3",
	"--impersonate=chrome --user-agent Mine",
	"--impersonate --retries 3",
	"--impersonate --impersonate chrome -x",
	"--retries 3 --impersonate",
	"--add-headers Accept-Language:en --add-headers=Referer:x --add-header=SEC-CH-UA:old --add-headers",
	"--user-agent=--impersonate",
	"",
}

// FuzzStripFlagArgs tests that stripping a flag always removes it entirely, and keeps every other argument in order.
func FuzzStripFlagArgs(f *testing.F) {
	for _, s := range argSeeds {
		f.Add(s, command.Impersonate)
		f.Add(s, command.UserAgent)
	}
	f.Fuzz(func(t *testing.T, s, flag string) {
		if !strings.HasPrefix(flag, "--") || strings.ContainsAny(flag, " =") {
			return
		}
		args := fuzzArgs(s)
		got := stripFlagArgs(args, flag, "https://test.example")

		if hasFlag(got, flag) {
			t.Errorf("flag %q still present after stripping %q: %q", flag, args, got)
		}
		if !isSubsequence(got, args) {
			t.Errorf("stripping %q reordered or invented arguments: %q -> %q", flag, args, got)
		}
		for _, a := range args {
			if a != flag && !strings.HasPrefix(a, flag+"=") && strings.HasPrefix(a, "-") && !slices.Contains(got, a) {
				t.Errorf("stripping %q removed unrelated flag %q: %q -> %q", flag, a, args, got)
			}
		}
	})
}

// FuzzStripHeaderArg tests that stripping a header removes every --add-headers setting it, and nothing else.
func FuzzStripHeaderArg(f *testing.F) {
	for _, s := range argSeeds {
		f.Add(s, "accept-language")
		f.Add(s, "sec-ch-ua")
	}
	f.Fuzz(func(t *testing.T, s, name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || strings.Contains(name, ":") {
			return
		}
		args := fuzzArgs(s)
		got, found := stripHeaderArg(args, name)

		if !isSubsequence(got, args) {
			t.Errorf("stripping header %q reordered or invented arguments: %q -> %q", name, args, got)
		}
		if len(got) < len(args) && !found {
			t.Errorf("stripping header %q removed arguments but reported none found: %q -> %q", name, args, got)
		}
		for i, a := range got {
			flag, value, joined := strings.Cut(a, "=")
			if flag != command.AddHeaders && flag != command.AddHeader {
				continue
			}
			if !joined && i+1 < len(got) {
				value = got[i+1]
			}
			if headerArgName(value) == name {
				t.Errorf("header %q still set after stripping: %q -> %q", name, args, got)
			}
		}
	})
}

// FuzzAddSiteRulesToYTDLP tests that a FlareSolverr site always ends up with exactly one --impersonate chrome and one
// --user-agent (FlareSolverr's), whatever the user's own arguments.
func FuzzAddSiteRulesToYTDLP(f *testing.F) {
	for _, s := range argSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		t.Cleanup(Reset)
		Reset()
		Register([]models.SiteScraper{{Domain: "cf.example", FlareSolverr: true}})
		fs := models.NewFlareSolverrSolution(models.FlareSolverrSolve{
			UserAgent: "FS/1",
			Headers:   [][2]string{{"accept-language", "C.UTF-8"}, {"user-agent", "FS/1"}},
			Gen:       1,
		}, nil, nil)

		got, _ := AddSiteRulesToYTDLP("https://cf.example/v", fs, fuzzArgs(s))

		count := func(flag string) (n int) {
			for _, a := range got {
				if a == flag || strings.HasPrefix(a, flag+"=") {
					n++
				}
			}
			return n
		}
		if n := count(command.Impersonate); n != 1 {
			t.Errorf("expected exactly one --impersonate, got %d: %q", n, got)
		}
		if n := count(command.UserAgent); n != 1 {
			t.Errorf("expected exactly one --user-agent, got %d: %q", n, got)
		}
		if i := slices.Index(got, command.UserAgent); i < 0 || i+1 >= len(got) || got[i+1] != "FS/1" {
			t.Errorf("expected FlareSolverr's user agent, got %q", got)
		}
	})
}
