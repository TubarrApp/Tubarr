package app

import (
	"sync/atomic"
)

var jobsRunning atomic.Int32

// AddRunningProcess adds a process to the list of running processes. Can be
// polled to determine when it's safe to exit the program (etc.).
func AddRunningProcess() {
	jobsRunning.Add(1)
}

// SubtractRunningProcess subtracts a process from the list of running processes. Can be
// polled to determine when it's safe to exit the program (etc.).
func SubtractRunningProcess() {
	jobsRunning.Add(-1)
}

// RunningProcesses returns the number of currently running processes.
func RunningProcesses() int32 {
	return jobsRunning.Load()
}
