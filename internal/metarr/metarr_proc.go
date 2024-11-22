package metarr

import (
	"fmt"
	"os"
	"os/exec"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// InitMetarr begins processing with Metarr
func InitMetarr(v *models.Video) error {
	args := makeMetarrCommand(v)
	cmd := exec.Command("metarr", args...)

	if err := runMetarr(cmd); err != nil {
		return err
	}
	logging.S(1, "Finished Metarr command for %q", v.VPath)
	return nil
}

// RunMetarr runs a Metarr command with a built argument list
func runMetarr(cmd *exec.Cmd) error {
	var err error = nil
	if cmd.String() == "" {
		return fmt.Errorf("command string is empty")
	}
	logging.I("Running command: %s", cmd.String())

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	if err = cmd.Run(); err != nil {
		logging.E(0, "Encountered error running command %q: %v", cmd.String(), err)
	}
	return err // Returns nil by default unless an error is grabbed
}
