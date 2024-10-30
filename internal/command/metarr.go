package command

import (
	logging "Tubarr/internal/utils/logging"
	"os"
	"os/exec"
)

func RunMetarr(args []string) error {

	command := exec.Command("metarr", args...)
	logging.PrintI("Running command: %s", command.String())

	command.Stderr = os.Stderr
	command.Stdout = os.Stdout

	if err := command.Run(); err != nil {
		logging.PrintE(0, "Encountered error running command '%s': %v", command.String(), err)
		return err
	}

	return nil
}
