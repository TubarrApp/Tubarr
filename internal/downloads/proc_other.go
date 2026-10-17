//go:build !unix

package downloads

import (
	"os/exec"
	"time"
)

// useProcessGroup gives a canceled cmd time to exit before its pipes are closed.
func useProcessGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 15 * time.Second
}

// killProcessGroup is a no-op without process groups.
func killProcessGroup(*exec.Cmd) {}
