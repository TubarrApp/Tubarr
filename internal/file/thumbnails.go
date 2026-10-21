package file

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// ErrThumbnailType is returned for an uploaded thumbnail that isn't a JPEG, PNG or WebP image.
var ErrThumbnailType = errors.New("thumbnails must be JPEG, PNG or WebP images")

// thumbnailExts maps the image types accepted for uploaded thumbnails to their file extensions.
var thumbnailExts = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

// SaveThumbnail stores an uploaded image as a video's thumbnail, replacing any earlier one, and returns its path.
func SaveThumbnail(videoID int64, data []byte) (string, error) {
	ext, ok := thumbnailExts[http.DetectContentType(data)]
	if !ok {
		return "", ErrThumbnailType
	}
	if err := os.MkdirAll(paths.ThumbnailDir, sharedconsts.PermsHomeProgDir); err != nil {
		return "", fmt.Errorf("failed to make thumbnail directory: %w", err)
	}
	base := strconv.FormatInt(videoID, 10)
	for _, e := range thumbnailExts {
		if e != ext {
			if err := os.Remove(filepath.Join(paths.ThumbnailDir, base+e)); err != nil && !errors.Is(err, os.ErrNotExist) {
				logger.Pl.W("Failed to remove video %d's earlier thumbnail: %v", videoID, err)
			}
		}
	}
	path := filepath.Join(paths.ThumbnailDir, base+ext)
	if err := os.WriteFile(path, data, sharedconsts.PermsPrivateFile); err != nil {
		return "", fmt.Errorf("failed to save thumbnail: %w", err)
	}
	return path, nil
}

// RemoveUnusedThumbnails deletes uploaded thumbnails which no video's Download As options use.
func RemoveUnusedThumbnails(overrides map[int64]models.VideoOverrides) {
	entries, err := os.ReadDir(paths.ThumbnailDir)
	if err != nil {
		return // None uploaded yet.
	}
	inUse := make(map[string]bool, len(overrides))
	for _, o := range overrides {
		inUse[o.Thumbnail] = true
	}
	for _, e := range entries {
		path := filepath.Join(paths.ThumbnailDir, e.Name())
		if e.IsDir() || inUse[path] {
			continue
		}
		if err := os.Remove(path); err != nil {
			logger.Pl.W("Failed to remove unused thumbnail %q: %v", path, err)
			continue
		}
		logger.Pl.I("Removed unused thumbnail %q", path)
	}
}
