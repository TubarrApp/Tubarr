package blocking

import "testing"

// TestGetTimeoutForDomain tests that keys match as suffixes, and anything else gets the default.
func TestGetTimeoutForDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   float64
	}{
		{"youtube.com", 2880},
		{"youtu.be", 2880},
		{"WWW.YouTube.com", 2880},
		{"reddit.com", 360},
		{"dropbox.com", 720},
		{"unknown.org", 720},
	}
	for _, tt := range tests {
		if got := GetTimeoutForDomain(tt.domain); got != tt.want {
			t.Errorf("GetTimeoutForDomain(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}
