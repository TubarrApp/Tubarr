package parsing

import (
	"slices"
	"testing"
)

// TestSplitArgs tests shell-style splitting of extra command line arguments.
func TestSplitArgs(t *testing.T) {
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
		{`--match-filter "title~='\d+'"`, []string{"--match-filter", `title~='\d+'`}},
		{`"say \"hi\"" 'it''s'`, []string{`say "hi"`, "its"}},
		{`--referer ""`, []string{"--referer", ""}},
		{`a"b c"d`, []string{"ab cd"}},
		{`"unclosed quote`, []string{"unclosed quote"}},
		{`trailing\`, []string{`trailing\`}},
	}
	for _, tt := range tests {
		if got := SplitArgs(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("SplitArgs(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
