package server

import (
	"testing"
	"tubarr/internal/models"
)

// TestParseOverrideLinesChecksMetarrArgs tests that Download As options are checked and normalized as saved settings are.
func TestParseOverrideLinesChecksMetarrArgs(t *testing.T) {
	bad := "xyz"
	if _, err := parseOverrideLines(&models.OverrideLines{}, &metarrArgsInput{OutputExt: &bad}); err == nil {
		t.Error("accepted Metarr output extension xyz")
	}

	upper := "MKV"
	o, err := parseOverrideLines(&models.OverrideLines{}, &metarrArgsInput{OutputExt: &upper})
	if err != nil {
		t.Fatal(err)
	}
	if o.OutputExt != "mkv" {
		t.Errorf("Metarr output extension %q not lowercased, got %q", upper, o.OutputExt)
	}
}
