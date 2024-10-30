package utils

import (
	"Metarr/internal/config"
	consts "Metarr/internal/domain/constants"
	enums "Metarr/internal/domain/enums"
	keys "Metarr/internal/domain/keys"
	"Metarr/internal/types"
	logging "Metarr/internal/utils/logging"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GetVideoFiles fetches video files from a directory
func GetVideoFiles(videoDir *os.File) (map[string]*types.FileData, error) {
	files, err := videoDir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("error reading video directory: %w", err)
	}

	convertFrom := config.Get(keys.InputExtsEnum).([]enums.ConvertFromFiletype)
	videoExtensions := SetExtensions(convertFrom)
	inputPrefixFilters := config.GetStringSlice(keys.FilePrefixes)
	inputPrefixes := SetPrefixFilter(inputPrefixFilters)

	fmt.Printf(`

Filtering directory: %s:

File extensions: %v
File prefixes: %v

`, videoDir.Name(),
		videoExtensions,
		inputPrefixes)

	videoFiles := make(map[string]*types.FileData)

	for _, file := range files {
		if !file.IsDir() && HasFileExtension(file.Name(), videoExtensions) && HasPrefix(file.Name(), inputPrefixes) {
			filenameBase := filepath.Base(file.Name())

			m := types.NewFileData()
			m.OriginalVideoPath = filepath.Join(videoDir.Name(), file.Name())
			m.OriginalVideoBaseName = strings.TrimSuffix(filenameBase, filepath.Ext(file.Name()))
			m.VideoDirectory = videoDir.Name()

			if !strings.HasSuffix(m.OriginalVideoBaseName, consts.OldTag) {
				videoFiles[file.Name()] = m
			} else {
				logging.PrintI("Skipping file '%s' containing backup tag ('%s')", m.OriginalVideoBaseName, consts.OldTag)
			}

			logging.PrintI(`Added video to queue: %v`, filenameBase)
		}
	}

	if len(videoFiles) == 0 {
		return nil, fmt.Errorf("no video files with extensions: %v and prefixes: %v found in directory: %s", videoExtensions, inputPrefixes, videoDir.Name())
	}
	return videoFiles, nil
}

// GetMetadataFiles fetches metadata files from a directory
func GetMetadataFiles(metaDir *os.File) (map[string]*types.FileData, error) {
	files, err := metaDir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("error reading metadata directory: %w", err)
	}

	metaExtensions := []string{".json", ".nfo"}
	inputPrefixFilters := config.GetStringSlice(keys.FilePrefixes)
	inputPrefixes := SetPrefixFilter(inputPrefixFilters)

	metaFiles := make(map[string]*types.FileData)

	for _, extension := range metaExtensions {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), extension) && HasPrefix(file.Name(), inputPrefixes) {

				logging.PrintD(3, "Iterating over file '%s'", file.Name())

				filenameBase := filepath.Base(file.Name())

				logging.PrintD(3, "Made base name for file: %s", filenameBase)

				m := types.NewFileData()

				filePath := filepath.Join(metaDir.Name(), file.Name())
				baseName := strings.TrimSuffix(filenameBase, filepath.Ext(file.Name()))

				logging.PrintD(3, "Filepath and base name: %s, %s", filePath, baseName)

				switch filepath.Ext(filePath) {
				case ".json":
					logging.PrintD(1, "Detected JSON file '%s'", file.Name())
					m.JSONFilePath = filePath
					m.JSONBaseName = baseName
					m.JSONDirectory = metaDir.Name()

					m.MetaFileType = enums.METAFILE_JSON

					logging.PrintD(3, "Meta file type set in model to %v", m.MetaFileType)

				case ".nfo":
					logging.PrintD(1, "Detected NFO file '%s'", file.Name())
					m.NFOFilePath = filePath
					m.NFOBaseName = baseName
					m.NFODirectory = metaDir.Name()

					m.MetaFileType = enums.METAFILE_NFO

					logging.PrintD(3, "Meta file type set in model to %v", m.MetaFileType)
				}

				if !strings.Contains(baseName, consts.OldTag) {
					metaFiles[file.Name()] = m
				} else {
					logging.PrintI("Skipping file '%s' containing backup tag ('%s')", m.JSONBaseName, consts.OldTag)
				}
			}
		}
	}
	if len(metaFiles) == 0 {
		return nil, fmt.Errorf("no meta files with extensions: %v and prefixes: %v found in directory: %s", metaExtensions, inputPrefixes, metaDir.Name())
	}

	logging.PrintD(3, "Returning meta files %v", metaFiles)
	return metaFiles, nil
}

// MatchVideoWithMetadata matches video files with their corresponding metadata files
func MatchVideoWithMetadata(videoFiles, metaFiles map[string]*types.FileData) (map[string]*types.FileData, error) {

	logging.PrintD(3, "Entering metadata and video file matching loop...")

	matchedFiles := make(map[string]*types.FileData)

	specialChars := regexp.MustCompile(`[^\w\s-]`)
	extraSpaces := regexp.MustCompile(`\s+`)

	for videoName, videoData := range videoFiles {

		// Normalize video name
		videoBase := strings.TrimSuffix(videoName, filepath.Ext(videoName))
		normalizedVideoBase := NormalizeFilename(videoBase, specialChars, extraSpaces)
		logging.PrintD(3, "Normalized video base: %s", normalizedVideoBase)

		for metaName, metaData := range metaFiles {

			metaBase := TrimMetafileSuffixes(metaName, videoBase)
			normalizedMetaBase := NormalizeFilename(metaBase, specialChars, extraSpaces)
			logging.PrintD(3, "Normalized metadata base: %s", normalizedMetaBase)

			if strings.Contains(normalizedMetaBase, normalizedVideoBase) {
				matchedFiles[videoName] = videoData
				matchedFiles[videoName].MetaFileType = metaData.MetaFileType

				logging.PrintD(3, "Entering meta filetype switch for matching videos and metadata...")

				switch videoData.MetaFileType {

				case enums.METAFILE_JSON:

					logging.PrintD(3, "Detected JSON")
					matchedFiles[videoName].JSONFilePath = metaData.JSONFilePath
					matchedFiles[videoName].JSONBaseName = metaData.JSONBaseName
					matchedFiles[videoName].JSONDirectory = metaData.JSONDirectory

				case enums.METAFILE_NFO:

					logging.PrintD(3, "Detected NFO")
					matchedFiles[videoName].NFOFilePath = metaData.NFOFilePath
					matchedFiles[videoName].NFOBaseName = metaData.NFOBaseName
					matchedFiles[videoName].NFODirectory = metaData.NFODirectory
				}
			}
		}
	}

	if len(matchedFiles) == 0 {
		return nil, fmt.Errorf("no matching metadata files found for any videos")
	}

	return matchedFiles, nil
}
