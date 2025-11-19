package file

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// WriteMetadataJSONFile writes the custom metadata file.
func WriteMetadataJSONFile(metadata map[string]any, filename, outputDir string, v *models.Video, c *models.Channel) error {
	filePath := fmt.Sprintf("%s/%s", strings.TrimRight(outputDir, "/"), filename)

	// Ensure the directory exists.
	if stillHasTemplating, _, err := sharedvalidation.ValidateDirectory(outputDir, true, sharedtemplates.AllTemplatesMap); stillHasTemplating || err != nil {
		if stillHasTemplating {
			logger.Pl.E("Dev Error: Element still has template tags before downloading to destination %q.", outputDir)

			if c != nil {
				logger.Pl.I("Attempting to fill tags before downloading to destination %q.", outputDir)
				dp := parsing.NewDirectoryParser(c)
				if outputDir, err = dp.ParseDirectory(outputDir, v, "JSON"); err != nil {
					return fmt.Errorf("directory %q was not able to fill all template elements", outputDir)
				}
			}
		}
		return err
	}

	// Create the file.
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Pl.E("failed to close file %v due to error: %v", filePath, err)
		}
	}()

	// Write JSON data with indentation.
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	v.JSONCustomFile = filePath
	return nil
}
