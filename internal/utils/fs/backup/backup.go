package utils

import (
	consts "Tubarr/internal/domain/constants"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BackupFile creates a backup copy of the original file before modifying it.
// helps save important data if the program fails in some way
func BackupFile(file *os.File) error {

	// Get the original filename
	originalFilePath := file.Name()

	backupFilePath := generateBackupFilename(originalFilePath)
	logging.PrintD(3, "Creating backup of file '%s' as '%s'", originalFilePath, backupFilePath)

	// Open the backup file for writing
	backupFile, err := os.Create(backupFilePath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer backupFile.Close()

	// Seek to the beginning of the original file
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to beginning of original file: %w", err)
	}

	// Copy the content of the original file to the backup file
	_, err = io.Copy(backupFile, file)
	if err != nil {
		return fmt.Errorf("failed to copy content to backup file: %w", err)
	}

	logging.PrintD(3, "Backup successfully created at '%s'", backupFilePath)
	return nil
}

// generateBackupFilename creates a backup filename by appending "_backup" to the original filename
func generateBackupFilename(originalFilePath string) string {
	ext := filepath.Ext(originalFilePath)
	base := strings.TrimSuffix(originalFilePath, ext)
	return fmt.Sprintf(base + consts.OldTag + ext)
}

// RenameToBackup renames the passed in file
func RenameToBackup(filename string) error {

	if filename == "" {
		logging.PrintE(0, "filename was passed in to backup empty")
	}

	backupName := generateBackupFilename(filename)

	if err := os.Rename(filename, backupName); err != nil {
		return fmt.Errorf("failed to backup filename '%s' to '%s'", filename, backupName)
	}
	return nil
}
