package command

import (
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
	"os/exec"
)

// RunMetarr runs a Metarr command with a built argument list
func RunMetarr(commands []*exec.Cmd) error {
	if len(commands) == 0 {
		return fmt.Errorf("no commands passed in")
	}

	var err error = nil
	for _, command := range commands {
		if len(command.String()) == 0 {
			logging.PrintE(0, "Command string is empty? %s", command.String())
			continue
		}
		logging.PrintI("Running command: %s", command.String())

		command.Stderr = os.Stderr
		command.Stdout = os.Stdout
		command.Stdin = os.Stdin

		if err = command.Run(); err != nil {
			logging.PrintE(0, "Encountered error running command '%s': %w", command.String(), err)
		}
	}
	return err // Returns nil by default unless an error is grabbed
}
