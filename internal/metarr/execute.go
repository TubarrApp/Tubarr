package metarr

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// InitMetarr begins processing with Metarr.
func InitMetarr(metarrCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) error {
	args := makeMetarrCommand(v, cu, c, dirParser)
	if len(args) == 0 {
		logger.Pl.I("No Metarr arguments built, returning...")
		return nil
	}

	cmd := exec.CommandContext(metarrCtx, "metarr", args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}

	if err := runMetarr(cmd, v); err != nil {
		return err
	}
	return nil
}

// runMetarr runs a Metarr command with a built argument list.
func runMetarr(cmd *exec.Cmd, v *models.Video) error {
	logger.Pl.I("Running Metarr command:\n\n%s\n", cmd.String())

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Run command and grab stdout.
	metarrStdout, err := cmd.Output()
	if err != nil {
		return err
	}

	// Retrieve filenames.
	if err := v.StoreFilenamesFromMetarr(string(metarrStdout)); err != nil {
		return err
	}

	// Success.
	logger.Pl.S("Metarr successfully processed video %q (file path: %q)", v.URL, v.VideoFilePath)
	return nil
}
