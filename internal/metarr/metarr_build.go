// Package metarr builds and runs Metarr commands.
package metarr

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/metcmd"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

type metCmdMapping struct {
	metarrValue metVals
	valType     metValTypes
	viperKey    string
	cmdKey      string
}

type metVals struct {
	str      string
	strSlice []string
	i        int
	f64      float64
}

type metValTypes int

const (
	str = iota
	strSlice
	i
	f64
)

// makeMetarrCommand combines arguments from both Viper config and model settings.
func makeMetarrCommand(v *models.Video) []string {

	fields := []metCmdMapping{

		// Metarr args:
		{
			metarrValue: metVals{i: v.MetarrArgs.Concurrency},
			valType:     i,
			viperKey:    "", // Don't use Tubarr concurrency key, Metarr has more potential resource constraints
			cmdKey:      metcmd.Concurrency,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.Ext},
			valType:     str,
			viperKey:    keys.OutputFiletype,
			cmdKey:      metcmd.Ext,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.FileDatePfx},
			valType:     str,
			viperKey:    keys.MInputFileDatePfx,
			cmdKey:      metcmd.FilenameDateTag,
		},
		{
			metarrValue: metVals{strSlice: v.MetarrArgs.FilenameReplaceSfx},
			valType:     strSlice,
			viperKey:    keys.MFilenameReplaceSuffix,
			cmdKey:      metcmd.FilenameReplaceSfx,
		},
		{
			metarrValue: metVals{f64: v.MetarrArgs.MaxCPU},
			valType:     f64,
			viperKey:    "",
			cmdKey:      metcmd.MaxCPU,
		},
		{
			metarrValue: metVals{strSlice: v.MetarrArgs.MetaOps},
			valType:     strSlice,
			viperKey:    keys.MMetaOps,
			cmdKey:      metcmd.MetaOps,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.MinFreeMem},
			valType:     str,
			viperKey:    keys.MMinFreeMem,
			cmdKey:      metcmd.MinFreeMem,
		},
		{
			metarrValue: metVals{str: parseOutputDir(v)},
			valType:     str,
			viperKey:    "", // Fallback logic already exists in parseOutputDir.
			cmdKey:      metcmd.OutputDir,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.RenameStyle},
			valType:     str,
			viperKey:    keys.MRenameStyle,
			cmdKey:      metcmd.RenameStyle,
		},
		// Transcoding
		{
			metarrValue: metVals{str: v.MetarrArgs.UseGPU},
			valType:     str,
			viperKey:    "",
			cmdKey:      metcmd.HWAccel,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.GPUDir},
			valType:     str,
			viperKey:    "",
			cmdKey:      metcmd.GPUDir,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.TranscodeCodec},
			valType:     str,
			viperKey:    "",
			cmdKey:      metcmd.TranscodeCodec,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.TranscodeQuality},
			valType:     str,
			viperKey:    "",
			cmdKey:      metcmd.TranscodeQuality,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.TranscodeAudioCodec},
			valType:     str,
			viperKey:    "",
			cmdKey:      metcmd.TranscodeAudioCodec,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.TranscodeVideoFilter},
			valType:     str,
			viperKey:    "",
			cmdKey:      metcmd.TranscodeVideoFilter,
		},
		// Other
		{
			metarrValue: metVals{str: ""},
			valType:     str,
			viperKey:    keys.DebugLevel,
			cmdKey:      metcmd.Debug,
		},
	}

	singlesLen, sliceLen := calcNumElements(fields)

	// Map use to ensure uniqueness
	argMap := make(map[string]string, singlesLen)
	argSlicesMap := make(map[string][]string, sliceLen)

	// Viper slice comma parsing issue workaround, may need to do the same for all strSlice arguments
	argMap[metcmd.VideoFile] = cleanAndWrapCommaPaths(v.VideoPath)

	if v.JSONCustomFile == "" {
		argMap[metcmd.JSONFile] = cleanAndWrapCommaPaths(v.JSONPath)
	} else {
		argMap[metcmd.JSONFile] = cleanAndWrapCommaPaths(v.JSONCustomFile)
	}

	logging.I("Making Metarr argument for video %q and JSON file %q.", argMap[metcmd.VideoFile], argMap[metcmd.JSONFile])

	// Final args
	args := make([]string, 0, singlesLen+sliceLen)

	var metaOW bool
	for _, f := range fields {
		if processField(f, argMap, argSlicesMap) {
			metaOW = true
		}
	}

	for k, v := range argMap {
		args = append(args, k, v)
	}

	for k, v := range argSlicesMap {
		for _, val := range v {
			args = append(args, k, val)
		}
	}

	if metaOW {
		args = append(args, metcmd.MetaOW)
	}

	logging.I("Built Metarr argument list: %v", args)
	return args
}

// processField processes each field in the argument map.
func processField(f metCmdMapping, argMap map[string]string, argSlicesMap map[string][]string) (metaOW bool) {
	switch f.valType {
	case i:
		if f.metarrValue.i > 0 {
			argMap[f.cmdKey] = strconv.Itoa(f.metarrValue.i)
		} else if f.viperKey != "" && cfg.IsSet(f.viperKey) {
			argMap[f.cmdKey] = strconv.Itoa(cfg.GetInt(f.viperKey))
		}
	case f64:
		if f.metarrValue.f64 > 0.0 {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", f.metarrValue.f64)
		} else if f.viperKey != "" && cfg.IsSet(f.viperKey) {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", cfg.GetFloat64(f.viperKey))
		}
	case str:
		if f.metarrValue.str != "" {
			argMap[f.cmdKey] = f.metarrValue.str
		} else if f.viperKey != "" && cfg.IsSet(f.viperKey) {
			argMap[f.cmdKey] = cfg.GetString(f.viperKey)
		}
	case strSlice:
		if len(f.metarrValue.strSlice) > 0 {
			argSlicesMap[f.cmdKey] = cleanCommaSliceValues(f.metarrValue.strSlice)
		} else if f.viperKey != "" && cfg.IsSet(f.viperKey) {
			argSlicesMap[f.cmdKey] = cleanCommaSliceValues(cfg.GetStringSlice(f.viperKey))
		}

		// Set Meta Overwrite flag if meta-ops arguments exist
		if f.cmdKey == metcmd.MetaOps {
			elemCount := len(f.metarrValue.strSlice)

			if cfg.IsSet(f.viperKey) {
				elemCount += len(cfg.GetStringSlice(f.viperKey))
			}

			if elemCount > 0 {
				logging.I("User set meta ops, will set meta overwrite key...")
				return true
			}
		}
	}
	return false
}

// parseOutputDir parses and returns the output directory.
func parseOutputDir(v *models.Video) string {
	dirParser := parsing.NewDirectoryParser(v.Channel, v)
	switch {
	case cfg.IsSet(keys.MoveOnComplete) && v.MetarrArgs.OutputDir == "":
		parsedDir, err := dirParser.ParseDirectory(cfg.GetString(keys.MoveOnComplete))
		if err != nil {
			logging.E(0, "Failed to parse directory for video with ID %d: %v", v.ID, err)
			break
		}
		cfg.Set(keys.MoveOnComplete, parsedDir)
		return parsedDir

	case v.MetarrArgs.OutputDir != "":
		parsed, err := dirParser.ParseDirectory(v.MetarrArgs.OutputDir)
		if err != nil {
			logging.E(0, "Failed to parse directory at %q: %v", v.MetarrArgs.OutputDir, err)
			break
		}
		return parsed
	}
	return ""
}

// calcNumElements returns the required map sizes.
func calcNumElements(fields []metCmdMapping) (singles, slices int) {
	singleElements := 2 // Start at 2 for VideoFile and JSONFile
	sliceElements := 0
	for _, f := range fields {
		switch f.valType {
		case str, i, f64:
			singleElements += 2 // One key and one value
		case strSlice:
			sliceElements += (len(f.metarrValue.strSlice) * 2) // One key and one value per entry
		}
	}
	return singleElements, sliceElements
}

// cleanAndWrapCommaPaths performs escaping for strings containing commas (can be misinterpreted in slices)
func cleanAndWrapCommaPaths(path string) string {

	if strings.ContainsRune(path, ',') {
		// Escape quotes if needed
		if strings.ContainsRune(path, '"') {
			escaped := strings.ReplaceAll(path, `"`, `\"`)
			if err := os.Rename(path, escaped); err != nil {
				logging.E(0, "Failed to escape quotes in filename %q: %v", path, err)
			} else {
				path = escaped
			}
		}

		// Prefix and suffix with double quotes
		b := strings.Builder{}
		b.Grow(len(path) + 2)
		b.WriteByte('"')
		b.WriteString(path)
		b.WriteByte('"')

		return b.String()
	}

	return path
}

// cleanCommaSliceValues escapes and fixes slice entries containing commas
func cleanCommaSliceValues(slice []string) []string {
	result := make([]string, 0, len(slice))
	for _, val := range slice {
		result = append(result, cleanAndWrapCommaPaths(val))
	}
	return result
}
