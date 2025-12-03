// Package metarr builds and runs Metarr commands.
package metarr

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/metkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/abstractions"
)

// metCmdMapping holds mapping data for building Metarr commands.
//
// valType: the type of value being processed (e.g. string, int, bool).
// metarrValue: holds the value set for the type (e.g. "/path/to/file").
// viperKey: is the Viper configuration key to check if no value is set in metarrValue (e.g. 'metarr-filename-ops').
// cmdKey: is the Metarr command-line argument key (e.g. '--video-file').
type metCmdMapping struct {
	valType     metValTypes
	metarrValue metVals
	viperKey    string
	cmdKey      string
}

// metVals holds possible Metarr argument values.
type metVals struct {
	str      string
	strSlice []string
	i        int
	f64      float64
	b        bool
}

// metValTypes defines the type of Metarr argument value.
type metValTypes int

const (
	str = iota
	strSlice
	i
	f64
	b
)

// makeMetarrCommand combines arguments from both Viper config and model settings.
func makeMetarrCommand(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// Load and merge meta ops: file ops override DB ops, then apply filtering.
	validMetaOps := loadAndMergeMetaOps(v, cu, c, dirParser)
	validFilenameOps := loadAndMergeFilenameOps(v, cu, c, dirParser)

	// Purge meta file flag.
	purgeMetaFile := false
	if abstractions.IsSet(keys.PurgeMetaFile) {
		purgeMetaFile = abstractions.GetBool(keys.PurgeMetaFile)
	}

	mOutDirParser := parsing.NewDirectoryParser(c, parsing.AllTags)
	// Define fields to process.
	fields := []metCmdMapping{
		// Metarr args:
		{
			metarrValue: metVals{strSlice: validFilenameOps},
			valType:     strSlice,
			viperKey:    keys.MFilenameOps,
			cmdKey:      metkeys.FilenameOps,
		},
		{
			metarrValue: metVals{f64: cu.ChanURLMetarrArgs.MaxCPU},
			valType:     f64,
			viperKey:    "",
			cmdKey:      metkeys.MaxCPU,
		},
		{
			metarrValue: metVals{strSlice: validMetaOps},
			valType:     strSlice,
			viperKey:    keys.MMetaOps,
			cmdKey:      metkeys.MetaOps,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.MinFreeMem},
			valType:     str,
			viperKey:    keys.MMinFreeMem,
			cmdKey:      metkeys.MinFreeMem,
		},
		{
			metarrValue: metVals{str: parseMetarrOutputDir(v, cu, c, mOutDirParser)},
			valType:     str,
			viperKey:    "", // Fallback logic already exists in parseMetarrOutputDir.
			cmdKey:      metkeys.OutputDir,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.OutputExt},
			valType:     str,
			viperKey:    keys.OutputFiletype,
			cmdKey:      metkeys.Ext,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.RenameStyle},
			valType:     str,
			viperKey:    keys.MRenameStyle,
			cmdKey:      metkeys.RenameStyle,
		},
		// Transcoding.
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.ExtraFFmpegArgs},
			valType:     str,
			viperKey:    keys.MExtraFFmpegArgs,
			cmdKey:      metkeys.ExtraFFmpegArgs,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.TranscodeGPU},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeGPU,
		},
		{
			metarrValue: metVals{strSlice: cu.ChanURLMetarrArgs.TranscodeVideoCodecs},
			valType:     strSlice,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeVideoCodecs,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.TranscodeQuality},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeQuality,
		},
		{
			metarrValue: metVals{strSlice: cu.ChanURLMetarrArgs.TranscodeAudioCodecs},
			valType:     strSlice,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeAudioCodecs,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.TranscodeVideoFilter},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeVideoFilter,
		},
		// Other
		{
			metarrValue: metVals{str: ""},
			valType:     str,
			viperKey:    keys.DebugLevel,
			cmdKey:      metkeys.Debug,
		},
		{
			metarrValue: metVals{b: purgeMetaFile},
			valType:     b,
			viperKey:    keys.PurgeMetaFile,
			cmdKey:      metkeys.PurgeMetaFile,
		},
	}
	singlesLen, sliceLen, flagLen := calcNumElements(fields)

	// Map use to ensure uniqueness.
	argMap := make(map[string]string, singlesLen)
	argSlicesMap := make(map[string][]string, sliceLen)
	argFlags := make([]string, 0, flagLen)

	// Viper slice comma parsing issue workaround.
	argMap[metkeys.VideoFile] = cleanAndWrapCommaPaths(v.VideoFilePath)

	if v.JSONCustomFile == "" {
		argMap[metkeys.MetaFile] = cleanAndWrapCommaPaths(v.JSONFilePath)
	} else {
		argMap[metkeys.MetaFile] = cleanAndWrapCommaPaths(v.JSONCustomFile)
	}

	// Build argument list.
	logger.Pl.I("Making Metarr argument for video %q and JSON file %q.", argMap[metkeys.VideoFile], argMap[metkeys.MetaFile])
	args := make([]string, 0, singlesLen+sliceLen)

	var metaOW bool
	// Process each field.
	for _, f := range fields {
		if processField(f, argMap, argSlicesMap, &argFlags) {
			metaOW = true
		}
	}
	if metaOW {
		args = append(args, metkeys.MetaOW)
	}

	// Combine all arguments from map.
	for k, v := range argMap {
		args = append(args, k, v)
	}

	// Combine all slice arguments from map.
	for k, v := range argSlicesMap {
		for _, val := range v {
			args = append(args, k, val)
		}
	}

	// Flags.
	args = append(args, argFlags...)

	return args
}

// processField processes each field in the argument map.
func processField(f metCmdMapping, argMap map[string]string, argSlicesMap map[string][]string, argFlags *[]string) (metaOW bool) {
	switch f.valType {
	case b: // Boolean flag: Set directly into the flags slice.
		if f.metarrValue.b {
			*argFlags = append(*argFlags, f.cmdKey)
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) && abstractions.GetBool(f.viperKey) {
			*argFlags = append(*argFlags, f.cmdKey)
		}
	case i:
		if f.metarrValue.i > 0 {
			argMap[f.cmdKey] = strconv.Itoa(f.metarrValue.i)
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			argMap[f.cmdKey] = strconv.Itoa(abstractions.GetInt(f.viperKey))
		}
	case f64:
		if f.metarrValue.f64 > 0.0 {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", f.metarrValue.f64)
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", abstractions.GetFloat64(f.viperKey))
		}
	case str:
		if f.metarrValue.str != "" {
			argMap[f.cmdKey] = f.metarrValue.str
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			argMap[f.cmdKey] = abstractions.GetString(f.viperKey)
		}
	case strSlice:
		if len(f.metarrValue.strSlice) > 0 {
			argSlicesMap[f.cmdKey] = cleanCommaSliceValues(f.metarrValue.strSlice)
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			argSlicesMap[f.cmdKey] = cleanCommaSliceValues(abstractions.GetStringSlice(f.viperKey))
		}
		// Set Meta Overwrite flag if meta-ops arguments exist.
		if f.cmdKey == metkeys.MetaOps {
			elemCount := len(f.metarrValue.strSlice)

			if abstractions.IsSet(f.viperKey) {
				elemCount += len(abstractions.GetStringSlice(f.viperKey))
			}

			if elemCount > 0 {
				logger.Pl.I("User set meta ops, will set meta overwrite key...")
				return true
			}
		}
	}
	return false
}

// parseMetarrOutputDir parses and returns the output directory.
func parseMetarrOutputDir(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) string {
	var (
		mArgs = cu.ChanURLMetarrArgs
		err   error
	)

	// Parse and validate output directory mappings.
	if mArgs.OutputDirMap, err = parsing.ParseMetarrOutputDirs(mArgs.OutputDir, mArgs.URLOutputDirs, c); err != nil {
		logger.Pl.E("Could not parse output directory map: %v", err)
	}
	if err := validation.ValidateMetarrOutputDirs(mArgs.OutputDirMap); err != nil {
		logger.Pl.E("Invalid output directory map: %v", err)
	}

	switch {
	// #1 Priority: Explicit Viper flag set.
	case abstractions.IsSet(keys.MoveOnComplete):
		d := abstractions.GetString(keys.MoveOnComplete)

		parsed, err := dirParser.ParseDirectory(d, "Metarr output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", d, v.URL, err)
			break
		}

		abstractions.Set(keys.MoveOnComplete, parsed)
		return parsed

	// #2 Priority: Move operation filter output directory.
	case v.MoveOpOutputDir != "":
		parsed, err := dirParser.ParseDirectory(v.MoveOpOutputDir, "Metarr output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", v.MoveOpOutputDir, v.URL, err)
			break
		}
		return parsed

	// #3 Priority: Channel default output directory.
	case mArgs.OutputDirMap[cu.URL] != "":
		parsed, err := dirParser.ParseDirectory(mArgs.OutputDirMap[cu.URL], "Metarr output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", mArgs.OutputDirMap[cu.URL], v.URL, err)
			break
		}
		return parsed

	// #4 Priority: Use the output directory stored in channel directly.
	case mArgs.OutputDir != "":
		parsed, err := dirParser.ParseDirectory(mArgs.OutputDir, "Metarr output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", mArgs.OutputDir, v.URL, err)
			break
		}
		return parsed
	}
	// Return blank
	return ""
}

// calcNumElements returns the required map sizes.
func calcNumElements(fields []metCmdMapping) (singles, slices, flags int) {
	singleElements := 2 // Start at 2 for VideoFile and JSONFile.
	sliceElements := 0
	flagElements := 0
	for _, f := range fields {
		switch f.valType {
		case str, i, f64:
			singleElements += 2 // One key and one value.
		case strSlice:
			sliceElements += (len(f.metarrValue.strSlice) * 2) // One key and one value per entry.
		case b:
			flagElements++ // Flag only.
		}
	}
	return singleElements, sliceElements, flagElements
}

// cleanAndWrapCommaPaths performs escaping for strings containing commas (can be misinterpreted in slices).
func cleanAndWrapCommaPaths(path string) string {

	if strings.ContainsRune(path, ',') {
		// Escape quotes if needed.
		if strings.ContainsRune(path, '"') {
			escaped := strings.ReplaceAll(path, `"`, `\"`)
			if err := os.Rename(path, escaped); err != nil {
				logger.Pl.E("Failed to escape quotes in filename %q: %v", path, err)
			} else {
				path = escaped
			}
		}

		// Prefix and suffix with double quotes.
		b := strings.Builder{}
		b.Grow(len(path) + 2)
		b.WriteByte('"')
		b.WriteString(path)
		b.WriteByte('"')

		return b.String()
	}
	return path
}

// cleanCommaSliceValues escapes and fixes slice entries containing commas.
func cleanCommaSliceValues(slice []string) []string {
	result := make([]string, 0, len(slice))
	for _, val := range slice {
		result = append(result, cleanAndWrapCommaPaths(val))
	}
	return result
}
