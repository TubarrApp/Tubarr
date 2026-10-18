package cfg

import (
	"slices"
	"testing"
)

func TestParsePicks(t *testing.T) {
	tests := []struct {
		input   string
		want    []int
		wantErr bool
	}{
		{input: "", want: nil},
		{input: "2", want: []int{2}},
		{input: "1,3,5-7", want: []int{1, 3, 5, 6, 7}},
		{input: "1, 3 4", want: []int{1, 3, 4}},
		{input: "3-4,4,1", want: []int{3, 4, 1}},
		{input: "ALL", want: []int{1, 2, 3, 4, 5, 6, 7, 8}},
		{input: "0", wantErr: true},
		{input: "9", wantErr: true},
		{input: "5-3", wantErr: true},
		{input: "2-9", wantErr: true},
		{input: "x", wantErr: true},
		{input: "1-", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parsePicks(tt.input, 8)
		if (err != nil) != tt.wantErr {
			t.Errorf("parsePicks(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && !slices.Equal(got, tt.want) {
			t.Errorf("parsePicks(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
