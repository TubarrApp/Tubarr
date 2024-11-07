package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	logging "tubarr/internal/utils/logging"
)

// appendURLsToFile appends new URLs to the specified file
func AppendURLsToFile(filename string, urls []string) error {
	if len(urls) == 0 {
		return nil
	}

	logging.PrintD(2, "Appending URLs to file %s: %v", filename, urls)

	// Ensure the directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", filename, err)
	}

	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filename, err)
	}
	defer file.Close()

	written := make(map[string]bool)

	// Load existing URLs from the file into the map
	existingFile, err := os.Open(filename)
	if err == nil {
		defer existingFile.Close()
		scanner := bufio.NewScanner(existingFile)
		for scanner.Scan() {
			written[scanner.Text()] = true
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading existing URLs: %w", err)
		}
	}

	// Append only new URLs to the file
	writer := bufio.NewWriter(file)
	for _, url := range urls {
		if url != "" && !written[url] {
			if _, err := writer.WriteString(url + "\n"); err != nil {
				return fmt.Errorf("error writing URL to file: %w", err)
			}
			written[url] = true
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("error flushing writer: %w", err)
	}

	return nil
}
