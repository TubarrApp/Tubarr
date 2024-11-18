package command

import (
	"fmt"
	"os"
	"os/exec"
	logging "tubarr/internal/utils/logging"
)

// RunMetarr runs a Metarr command with a built argument list
func RunMetarr(cmd *exec.Cmd) error {
	var err error = nil
	if len(cmd.String()) == 0 {
		return fmt.Errorf("command string is empty? %s", cmd.String())
	}
	logging.I("Running command: %s", cmd.String())

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	if err = cmd.Run(); err != nil {
		logging.E(0, "Encountered error running command '%s': %w", cmd.String(), err)
	}
	return err // Returns nil by default unless an error is grabbed
}
