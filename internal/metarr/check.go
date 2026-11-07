package metarr

import (
	"context"
	"fmt"
	"os/exec"
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// CheckInstalled returns an error if Metarr is not in $PATH or cannot be executed.
func CheckInstalled(ctx context.Context) error {
	if _, err := exec.LookPath(sharedconsts.ProgramMetarr); err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, sharedconsts.ProgramMetarr, "--help").Run(); err != nil {
		return fmt.Errorf("found '%s' in $PATH but failed to execute: %w", sharedconsts.ProgramMetarr, err)
	}
	return nil
}

// SkipReason returns why Metarr can't run for a channel URL's videos, with the error behind it: installErr from
// CheckInstalled, or an ops file at any level that can't be used, which Metarr would otherwise run without.
func SkipReason(installErr error, cu *models.ChannelURL, dp *parsing.DirectoryParser) (models.SkipMetarr, error) {
	if installErr != nil {
		return models.SkipMetarr{Skip: true, Reason: models.SkipMetarrNoPath}, installErr
	}
	if _, err := channelURLMetaOps(cu, dp); err != nil {
		return models.SkipMetarr{Skip: true, Reason: models.SkipMetarrMissingMetaOpsFile}, err
	}
	if _, err := channelURLFilenameOps(cu, dp); err != nil {
		return models.SkipMetarr{Skip: true, Reason: models.SkipMetarrMissingFilenameOpsFile}, err
	}
	if _, err := metadata.ChannelURLFilteredMetaOps(cu, dp); err != nil {
		return models.SkipMetarr{Skip: true, Reason: models.SkipMetarrMissingFilteredMetaOpsFile}, err
	}
	if _, err := metadata.ChannelURLFilteredFilenameOps(cu, dp); err != nil {
		return models.SkipMetarr{Skip: true, Reason: models.SkipMetarrMissingFilteredFilenameOpsFile}, err
	}
	return models.SkipMetarr{}, nil
}
