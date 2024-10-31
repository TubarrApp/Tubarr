package command

import (
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	"Tubarr/internal/models"
	logging "Tubarr/internal/utils/logging"
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	muDl sync.Mutex
)

// DownloadVideos takes in a list of URLs
func DownloadVideos(urls []string) ([]models.DownloadedFiles, error) {
	muDl.Lock()
	defer muDl.Unlock()

	cookies := config.GetString(keys.CookieSource)
	if len(urls) == 0 {
		return nil, nil
	}

	vDir := config.GetString(keys.VideoDir)
	eDl := config.GetString(keys.ExternalDownloader)
	eDlArgs := config.GetString(keys.ExternalDownloaderArgs)

	var dlFiles []models.DownloadedFiles

	for _, entry := range urls {
		if entry == "" {
			continue
		}

		dlFile := models.DownloadedFiles{
			URL: entry,
		}

		// Create the yt-dlp command
		args := []string{
			"--write-info-json",
			"-P", vDir,
			"--restrict-filenames",
			"-o", "%(title)s.%(ext)s",
			"--print", "after_move:%(filepath)s", // Print filepath after download
			"--retries", "999",
			"--retry-sleep", "10",
		}

		// Add external downloader if specified
		if eDl != "" {
			args = append(args, "--external-downloader", eDl)
			if eDlArgs != "" {
				args = append(args, "--external-downloader-args", eDlArgs)
			}
		}

		// Add cookies
		if cookies != "" {
			args = append(args, "--cookies-from-browser", cookies)
		}

		// Add URL
		args = append(args, entry)

		cmd := exec.Command("yt-dlp", args...)

		// Create pipes for stdout and stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			logging.PrintE(0, "Failed to create stdout pipe: %v", err)
			continue
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			logging.PrintE(0, "Failed to create stderr pipe: %v", err)
			continue
		}

		// Start command
		logging.PrintI("Executing download command: %s", cmd.String())
		if err := cmd.Start(); err != nil {
			logging.PrintE(0, "Failed to start download: %v", err)
			continue
		}

		// Create output scanner
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		var outputFilename string

		// Read output in real-time
		go func() {
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Println(line)
				// Capture the actual output filename
				if strings.HasPrefix(line, "/") && strings.HasSuffix(line, ".mp4") {
					outputFilename = line
				}
			}
		}()

		// Wait for command to complete
		if err := cmd.Wait(); err != nil {
			logging.PrintE(0, "Download failed: %v", err)
			continue
		}

		// Wait a moment for filesystem to sync
		time.Sleep(1 * time.Second)

		if outputFilename == "" {
			logging.PrintE(0, "No output filename captured for URL: %s", entry)
			continue
		}

		dlFile.VideoFilename = outputFilename
		baseName := strings.TrimSuffix(filepath.Base(outputFilename), filepath.Ext(outputFilename))
		dlFile.JSONFilename = filepath.Join(vDir, baseName+".info.json")

		// Verify the download
		if err := verifyDownload(dlFile.VideoFilename, dlFile.JSONFilename); err != nil {
			logging.PrintE(0, "Download verification failed: %v", err)
			continue
		}

		logging.PrintS(0, "Successfully downloaded files:\nVideo: %s\nJSON: %s",
			dlFile.VideoFilename, dlFile.JSONFilename)
		dlFiles = append(dlFiles, dlFile)
	}

	if len(dlFiles) == 0 {
		return nil, fmt.Errorf("no files were successfully downloaded")
	}

	return dlFiles, nil
}

// verifyDownload checks if the specified files exist and are non-empty
func verifyDownload(videoPath, jsonPath string) error {
	// Check video file
	videoInfo, err := os.Stat(videoPath)
	if err != nil {
		return fmt.Errorf("video file verification failed: %w", err)
	}
	if videoInfo.Size() == 0 {
		return fmt.Errorf("video file is empty: %s", videoPath)
	}

	// Check JSON file
	jsonInfo, err := os.Stat(jsonPath)
	if err != nil {
		return fmt.Errorf("JSON file verification failed: %w", err)
	}
	if jsonInfo.Size() == 0 {
		return fmt.Errorf("JSON file is empty: %s", jsonPath)
	}

	return nil
}
