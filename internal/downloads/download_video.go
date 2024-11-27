package downloads

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// executeVideoDownload executes a video download command.
func executeVideoDownload(v *models.Video, cmd *exec.Cmd) error {

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe error: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe error: %w", err)
	}

	filenameChan := make(chan string, 1)

	go scanVCmdOutput(io.MultiReader(stdout, stderr), filenameChan)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("command start error: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command wait error: %w", err)
	}

	filename := <-filenameChan
	if filename == "" {
		return errors.New("no output filename captured")
	}

	v.VPath = filename

	if err := waitForFile(v.VPath, 5*time.Second); err != nil {
		return err
	}

	if err := verifyVideoDownload(v.VPath); err != nil {
		return err
	}

	logging.S(0, "Download successful: %s", v.VPath)
	return nil
}

// scanVCmdOutput scans the video command output for the video filename.
func scanVCmdOutput(r io.Reader, filenameChan chan<- string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)

		if strings.HasPrefix(line, "/") {
			ext := filepath.Ext(line)
			for _, validExt := range consts.AllVidExtensions {
				if ext == validExt {
					filenameChan <- line
					return
				}
			}
		}
	}
	close(filenameChan)
}
