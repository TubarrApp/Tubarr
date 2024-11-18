package command

import (
	"fmt"
	"os"
	"time"
)

// waitForFile waits until the file is ready in the file system
func waitForFile(filePath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {

		_, err := os.Stat(filePath)
		if err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("unexpected error while checking file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("file not ready or empty after %v: %s", timeout, filePath)
}
