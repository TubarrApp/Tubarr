package file

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/validation"
)

// WriteMetadataJSONFile writes the custom metadata file.
func WriteMetadataJSONFile(metadata map[string]any, filename, outputDir string, v *models.Video) error {
	filePath := fmt.Sprintf("%s/%s", strings.TrimRight(outputDir, "/"), filename)

	// Ensure the directory exists
	if _, err := validation.ValidateDirectory(outputDir, true); err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Pl.E("failed to close file %v due to error: %v", filePath, err)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	v.JSONCustomFile = filePath
	return nil
}
