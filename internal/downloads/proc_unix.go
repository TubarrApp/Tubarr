//go:build unix

package downloads

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// cancelWaitDelay is how long a canceled command's process group has to exit before its pipes are closed.
const cancelWaitDelay = 15 * time.Second

// useProcessGroup starts cmd in its own process group, so cancelling it stops its children (e.g. ffmpeg, aria2c) too.
func useProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return signalGroup(cmd, syscall.SIGTERM)
	}
	cmd.WaitDelay = cancelWaitDelay
}

// killProcessGroup kills whatever is left of cmd's process group once cmd has exited.
func killProcessGroup(cmd *exec.Cmd) {
	_ = signalGroup(cmd, syscall.SIGKILL)
}

// signalGroup sends sig to cmd's process group, ignoring a group that has already exited.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
