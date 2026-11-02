package validation_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"tubarr/internal/validation"
)

// TestValidateToFromDate_DayWithoutMonth tests a number marked as a day stays the day when no month is given.
func TestValidateToFromDate_DayWithoutMonth(t *testing.T) {
	thisYear := strconv.Itoa(time.Now().Year())
	for _, tc := range []struct{ input, want string }{
		{"2024y5d", "20240105"},
		{"2024y15d", "20240115"},
		{"5d", thisYear + "0105"},
		{"2024y1m5d", "20240105"},
		{"2024y5m", "20240501"},
		{"5m", thisYear + "0501"},
		{"20240105", "20240105"},
		{"2024-01-05", "20240105"},
	} {
		got, err := validation.ValidateToFromDate(tc.input)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestValidateYtdlpVideoCodecs_ErrorNamesVideo tests a refused video codec's error says video.
func TestValidateYtdlpVideoCodecs_ErrorNamesVideo(t *testing.T) {
	_, err := validation.ValidateYtdlpVideoCodecs([]string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "video codec") {
		t.Errorf("error = %v, want one naming a video codec", err)
	}
}
