package files

import (
	"bufio"
	"os"
	"strings"
	"tubarr/internal/utils/logging"
)

// ReadFileLines loads lines from a file (one per line, ignoring '#' comment lines).
func ReadFileLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			logging.E(0, "failed to close file %v due to error: %v", path, err)
		}
	}()

	f := []string{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip blank lines and comments
		}
		f = append(f, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return f, nil
}
