package limits

import "testing"

// TestRegisteredDomain tests hosts reduce to their registered domain whatever their case.
func TestRegisteredDomain(t *testing.T) {
	for in, want := range map[string]string{
		"www.youtube.com":   "youtube.com",
		"WWW.YouTube.COM":   "youtube.com",
		"www.bbc.co.uk":     "bbc.co.uk",
		"WWW.EXAMPLE.CO.UK": "example.co.uk",
	} {
		if got := RegisteredDomain(in); got != want {
			t.Errorf("RegisteredDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
