package command

import (
	builder "Tubarr/internal/command/builder"
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	"Tubarr/internal/models"
	utils "Tubarr/internal/utils/fs/write"
	logging "Tubarr/internal/utils/logging"
	"bufio"
	"encoding/json"
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

// DownloadVideos takes in a list of URLs and downloads them
func DownloadVideos(urls []string) ([]*models.DownloadedFiles, error) {
	muDl.Lock()
	defer muDl.Unlock()

	if len(urls) == 0 {
		return nil, nil
	}

	// Get configuration values
	vDir := config.GetString(keys.VideoDir)
	cookieSource := config.GetString(keys.CookieSource)
	eDl := config.GetString(keys.ExternalDownloader)
	eDlArgs := config.GetString(keys.ExternalDownloaderArgs)

	var dlFiles []*models.DownloadedFiles
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

		// Build command
		vcb := builder.NewVideoDLCommandBuilder(&dlFile)
		cmd, err := vcb.VideoFetchCommand()
		if err != nil {
			logging.PrintE(0, "Failed to build command for URL '%s': %v", dlFile.URL, err)
			continue
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

		// Channel to receive the output filename
		filenameChan := make(chan string, 1)
		doneChan := make(chan struct{})

		// Start output scanner in goroutine
		go func() {
			defer close(doneChan)
			scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Println(line)

				// Capture the actual output filename
				if strings.HasPrefix(line, "/") {
					ext := filepath.Ext(line)
					switch ext {
					case ".3gp", ".avi", ".f4v", ".flv", ".m4v", ".mkv",
						".mov", ".mp4", ".mpeg", ".mpg", ".ogm", ".ogv",
						".ts", ".vob", ".webm", ".wmv":
						select {
						case filenameChan <- line:
						default:
							// Channel already has a filename
						}
					default:
						logging.PrintD(1, "Did not find yt-dlp supported video format in output %s", line)
					}
				}
			}
			if err := scanner.Err(); err != nil {
				logging.PrintE(0, "Scanner error: %v", err)
			}
		}()

		// Start command
		logging.PrintI("Executing download command: %s", cmd.String())
		if err := cmd.Start(); err != nil {
			logging.PrintE(0, "Failed to start download: %v", err)
			close(filenameChan)
			continue
		}

		// Wait for command to complete
		if err := cmd.Wait(); err != nil {
			logging.PrintE(0, "Download failed: %v", err)
			close(filenameChan)
			continue
		}

		// Wait for scanner to finish
		<-doneChan
		close(filenameChan)

		// Get output filename
		outputFilename := ""
		select {
		case filename := <-filenameChan:
			outputFilename = filename
		default:
			logging.PrintE(0, "No output filename captured for URL: %s", entry)
			continue
		}

		// Short wait time for filesystem sync
		time.Sleep(1 * time.Second)

		// Set filenames
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
		dlFiles = append(dlFiles, &dlFile)
		successfulURLs = append(successfulURLs, entry)
	}

	if len(dlFiles) == 0 {
		return nil, fmt.Errorf("no files successfully downloaded")
	}

	// Update grabbed URLs file
	grabbedURLsPath := filepath.Join(vDir, "grabbed-urls.txt")
	if err := utils.AppendURLsToFile(grabbedURLsPath, successfulURLs); err != nil {
		logging.PrintE(0, "Failed to update grabbed-urls.txt: %v", err)
		// Don't return error because downloads were successful
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
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read JSON file: %w", err)
	}

	if !json.Valid(jsonData) {
		return fmt.Errorf("invalid JSON content in file: %s", jsonPath)
	}

	return nil
}
