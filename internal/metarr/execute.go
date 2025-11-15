package metarr

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// InitMetarr begins processing with Metarr
func InitMetarr(procCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) error {
	args := makeMetarrCommand(v, cu, c, dirParser)
	if len(args) == 0 {
		logging.I("No Metarr arguments built, returning...")
		return nil
	}

	cmd := exec.CommandContext(procCtx, "metarr", args...)

	if err := runMetarr(cmd, v); err != nil {
		return err
	}
	return nil
}

// runMetarr runs a Metarr command with a built argument list
func runMetarr(cmd *exec.Cmd, v *models.Video) error {
	var err error
	if cmd.String() == "" {
		return errors.New("command string is empty")
	}
	logging.I("Running Metarr command:\n\n%s\n", cmd.String())

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	metarrStdout, err := cmd.Output()
	if err != nil {
		logging.E("Encountered error running command %q: %v", cmd.String(), err)
		return err
	}

	// Retrieve filenames
	v.StoreFilenamesFromMetarr(string(metarrStdout))
	return nil
}
