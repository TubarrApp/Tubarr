package metarr

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// TestSkipReason tests each reason Metarr is held back for a channel URL, and that it isn't otherwise.
func TestSkipReason(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	c := &models.Channel{ID: 1, Name: "Cats"}

	for name, tc := range map[string]struct {
		installErr error
		args       models.MetarrArgs
		want       models.SkipMetarrReasons
	}{
		"runs":                       {},
		"no metarr":                  {installErr: errors.New("not found"), want: models.SkipMetarrNoPath},
		"meta ops file":              {args: models.MetarrArgs{MetaOpsFile: missing}, want: models.SkipMetarrMissingMetaOpsFile},
		"filename ops file":          {args: models.MetarrArgs{FilenameOpsFile: missing}, want: models.SkipMetarrMissingFilenameOpsFile},
		"filtered meta ops file":     {args: models.MetarrArgs{FilteredMetaOpsFile: missing}, want: models.SkipMetarrMissingFilteredMetaOpsFile},
		"filtered filename ops file": {args: models.MetarrArgs{FilteredFilenameOpsFile: missing}, want: models.SkipMetarrMissingFilteredFilenameOpsFile},
	} {
		cu := &models.ChannelURL{URL: "https://example.com/@cats", ChanURLMetarrArgs: &tc.args}
		skip, err := SkipReason(tc.installErr, cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
		if skip.Skip != (tc.want != "") || skip.Reason != tc.want || (err != nil) != skip.Skip {
			t.Errorf("%s: SkipReason = %+v, %v; want reason %q", name, skip, err, tc.want)
		}
	}
}
