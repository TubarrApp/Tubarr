package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// ErrThumbnailType is returned for an uploaded thumbnail that isn't a JPEG, PNG, or WebP image.
var ErrThumbnailType = errors.New("thumbnails must be JPEG, PNG, or GIF images")

// thumbnailExts maps the image types accepted for uploaded thumbnails to their file extensions.
var thumbnailExts = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
}

// SaveThumbnail stores an uploaded image as a video's thumbnail, replacing any earlier one, and returns its path.
func SaveThumbnail(videoID int64, data []byte) (string, error) {
	ext, ok := thumbnailExts[http.DetectContentType(data)]
	if !ok {
		return "", ErrThumbnailType
	}
	if err := os.MkdirAll(paths.UploadedThumbnailDir, sharedconsts.PermsHomeProgDir); err != nil {
		return "", fmt.Errorf("failed to make thumbnail directory: %w", err)
	}
	base := strconv.FormatInt(videoID, 10)
	for _, e := range thumbnailExts {
		if e != ext {
			if err := os.Remove(filepath.Join(paths.UploadedThumbnailDir, base+e)); err != nil && !errors.Is(err, os.ErrNotExist) {
				logger.Pl.W("Failed to remove video %d's earlier thumbnail: %v", videoID, err)
			}
		}
	}
	path := filepath.Join(paths.UploadedThumbnailDir, base+ext)
	if err := os.WriteFile(path, data, sharedconsts.PermsPrivateFile); err != nil {
		return "", fmt.Errorf("failed to save thumbnail: %w", err)
	}
	return path, nil
}

// RemoveUnusedThumbnails deletes uploaded thumbnails which no video's Download As options use.
func RemoveUnusedThumbnails(overrides map[int64]models.VideoOverrides) {
	entries, err := os.ReadDir(paths.UploadedThumbnailDir)
	if err != nil {
		return // None uploaded yet.
	}
	inUse := make(map[string]bool, len(overrides))
	for _, o := range overrides {
		inUse[o.Thumbnail] = true
	}
	for _, e := range entries {
		path := filepath.Join(paths.UploadedThumbnailDir, e.Name())
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

// maxCachedThumbnail is the largest thumbnail image the cache will fetch.
const maxCachedThumbnail = 10 << 20

// thumbnailClient fetches thumbnails for the cache.
var thumbnailClient = &http.Client{Timeout: 15 * time.Second}

// cachedThumbnailPath returns where a thumbnail URL's image is cached.
func cachedThumbnailPath(thumbURL string) string {
	sum := sha256.Sum256([]byte(thumbURL))
	return filepath.Join(paths.CacheThumbnailDir, hex.EncodeToString(sum[:16]))
}

// CacheThumbnail returns the path of a thumbnail URL's cached image, fetching it on first use.
func CacheThumbnail(ctx context.Context, thumbURL string) (string, error) {
	path := cachedThumbnailPath(thumbURL)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, thumbURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := thumbnailClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Pl.E("Failed to close thumbnail response for %q: %v", thumbURL, err)
		}
	}()
	if resp == nil {
		return "", fmt.Errorf("no response for thumbnail request")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("thumbnail request returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCachedThumbnail+1))
	if err != nil {
		return "", fmt.Errorf("failed to read thumbnail: %w", err)
	}
	if len(data) > maxCachedThumbnail {
		return "", errors.New("thumbnail is over 10 MB")
	}
	if !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return "", errors.New("thumbnail URL did not return an image")
	}

	// Written to a temporary file then renamed, so a request at the same time never serves half an image.
	if err := os.MkdirAll(paths.CacheThumbnailDir, sharedconsts.PermsHomeProgDir); err != nil {
		return "", fmt.Errorf("failed to make thumbnail cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(paths.CacheThumbnailDir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("failed to cache thumbnail: %w", err)
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		if rmErr := os.Remove(tmp.Name()); rmErr != nil {
			logger.Pl.W("Failed to remove temporary thumbnail %q: %v", tmp.Name(), rmErr)
		}
		return "", fmt.Errorf("failed to cache thumbnail: %w", err)
	}
	return path, nil
}

// RemoveUnusedCachedThumbnails deletes cached thumbnails which no video's crawled or Download As thumbnail URL uses.
func RemoveUnusedCachedThumbnails(thumbURLs []string, overrides map[int64]models.VideoOverrides) {
	entries, err := os.ReadDir(paths.CacheThumbnailDir)
	if err != nil {
		return // None cached yet.
	}
	inUse := make(map[string]bool, len(thumbURLs)+len(overrides))
	for _, u := range thumbURLs {
		inUse[cachedThumbnailPath(u)] = true
	}
	for _, o := range overrides {
		if o.Thumbnail != "" {
			inUse[cachedThumbnailPath(o.Thumbnail)] = true
		}
	}

	removed := 0
	for _, e := range entries {
		path := filepath.Join(paths.CacheThumbnailDir, e.Name())
		if e.IsDir() || inUse[path] {
			continue
		}
		if err := os.Remove(path); err != nil {
			logger.Pl.W("Failed to remove unused cached thumbnail %q: %v", path, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		logger.Pl.I("Removed %d unused cached thumbnail(s)", removed)
	}
}
