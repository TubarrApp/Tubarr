package parsing

import (
	"slices"
	"testing"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// TestSplitArgs tests shell-style splitting of extra command line arguments.
func TestShellSplit(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  -t   sleep \n --retries 3 ", []string{"-t", "sleep", "--retries", "3"}},
		{`--add-headers "Referer: https://a.example"`, []string{"--add-headers", "Referer: https://a.example"}},
		{`--add-headers 'Referer: https://a.example'`, []string{"--add-headers", "Referer: https://a.example"}},
		{`--add-headers=Referer:\ x`, []string{"--add-headers=Referer: x"}},
		{`-o "%(title)s [%(id)s].%(ext)s"`, []string{"-o", "%(title)s [%(id)s].%(ext)s"}},
		{`--match-filter "title~='\\d+'"`, []string{"--match-filter", `title~='\d+'`}},
		{`"say \"hi\"" 'it''s'`, []string{`say "hi"`, "its"}},
		{`--referer ""`, []string{"--referer", ""}},
		{`a"b c"d`, []string{"ab cd"}},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := sharedparsing.ShellSplit(tt.in)
			if err != nil {
				t.Fatalf("ShellSplit(%q) error = %v", tt.in, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ShellSplit(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
