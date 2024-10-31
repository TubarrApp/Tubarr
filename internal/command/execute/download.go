package command

import (
	builder "Tubarr/internal/command/builder"
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	"Tubarr/internal/models"
	utils "Tubarr/internal/utils/fs/write"
	logging "Tubarr/internal/utils/logging"
	"bufio"
	"fmt"
	"io"
	"os"
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

	cookieSource := config.GetString(keys.CookieSource)
	if len(urls) == 0 {
		return nil, nil
	}

	vDir := config.GetString(keys.VideoDir)
	eDl := config.GetString(keys.ExternalDownloader)
	eDlArgs := config.GetString(keys.ExternalDownloaderArgs)

	var dlFiles []models.DownloadedFiles
	var successfulURLs []string

	for _, entry := range urls {
		if entry == "" {
			continue
		}

		dlFile := models.DownloadedFiles{
			CookieSource:     cookieSource,
			ExternalDler:     eDl,
			ExternalDlerArgs: eDlArgs,
			URL:              entry,
			VideoDirectory:   vDir,
		}

		vcb := builder.NewVideoCommandBuilder(&dlFile)
		cmd, err := vcb.VideoFetchCommand()
		if err != nil {
			logging.PrintE(0, "Failed to build command for URL '%s': %v", dlFile.URL, err)
		}

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
				if strings.HasPrefix(line, "/") {
					switch {
					case strings.HasSuffix(line, ".avi"),
						strings.HasSuffix(line, ".mkv"),
						strings.HasSuffix(line, ".mov"),
						strings.HasSuffix(line, ".mp4"),
						strings.HasSuffix(line, ".webm"),
						strings.HasSuffix(line, ".wmv"):

						outputFilename = line

					default:
						logging.PrintD(1, "Did not find video format in output %s", line)
					}
				}
			}
		}()

		// Wait for command to complete...
		if err := cmd.Wait(); err != nil {
			logging.PrintE(0, "Download failed: %v", err)
			continue
		}

		// Short wait time for filesystem sync
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
		successfulURLs = append(successfulURLs, entry)
	}

	if len(dlFiles) == 0 {
		return nil, fmt.Errorf("no files successfully downloaded")
	}

	grabbedURLsPath := filepath.Join(vDir, "grabbed-urls.txt")
	if err := utils.AppendURLsToFile(grabbedURLsPath, successfulURLs); err != nil {
		logging.PrintE(0, "Failed to update grabbed-urls.txt: %v", err)
		// Don't return error because downloads were successful at least
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
