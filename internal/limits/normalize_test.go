package limits

import "testing"

// TestNormalizeDomain tests that hostnames and URLs as users enter them reduce to the domain limits are looked up by.
func TestNormalizeDomain(t *testing.T) {
	tests := map[string]string{
		"youtube.com":                      "youtube.com",
		"www.YouTube.com":                  "youtube.com",
		" m.youtube.com ":                  "youtube.com",
		"https://www.youtube.com/@channel": "youtube.com",
		"www.bbc.co.uk/iplayer":            "bbc.co.uk",
		"WWW.EXAMPLE.CO.UK":                "example.co.uk", // Uppercase suffixes must still match the list.
		"https://Example.Co.Uk/video":      "example.co.uk",
	}
	for in, want := range tests {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
