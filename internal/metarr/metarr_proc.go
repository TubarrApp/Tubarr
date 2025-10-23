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

	if err := runMetarr(cmd); err != nil {
		return err
	}
	return nil
}

// RunMetarr runs a Metarr command with a built argument list
func runMetarr(cmd *exec.Cmd) error {
	var err error
	if cmd.String() == "" {
		return errors.New("command string is empty")
	}
	logging.I("Running Metarr command:\n\n%s\n", cmd.String())

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	if err = cmd.Run(); err != nil {
		logging.E("Encountered error running command %q: %v", cmd.String(), err)
		return err
	}
	return nil
}
