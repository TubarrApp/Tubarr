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

// TestCheckOverridesFit tests that Download As options are checked together with the channel URL's they leave blank.
func TestCheckOverridesFit(t *testing.T) {
	c := &models.Channel{URLModels: []*models.ChannelURL{
		{ID: 1, ChanURLMetarrArgs: &models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}},
		{ID: 2, ChanURLMetarrArgs: &models.MetarrArgs{OutputExt: "webm"}},
	}}
	tests := []struct {
		name  string
		urlID int64
		o     models.MetarrArgs
		fits  bool
	}{
		{"webm over the URL's h264", 1, models.MetarrArgs{OutputExt: "webm"}, false},
		{"webm replacing the URL's h264 with vp9", 1, models.MetarrArgs{OutputExt: "webm", TranscodeVideoCodecs: []string{"vp9"}}, true},
		{"h264 under the URL's webm", 2, models.MetarrArgs{TranscodeVideoCodecs: []string{"h264"}}, false},
		{"mp4 replacing the URL's webm", 2, models.MetarrArgs{OutputExt: "mp4", TranscodeVideoCodecs: []string{"h264"}}, true},
		{"unknown URL", 3, models.MetarrArgs{OutputExt: "webm"}, true},
	}
	for _, tt := range tests {
		err := checkOverridesFit(nil, c, tt.urlID, &models.VideoOverrides{MetarrArgs: tt.o})
		if (err == nil) != tt.fits {
			t.Errorf("%s: got error %v, want fits %t", tt.name, err, tt.fits)
		}
	}
}
