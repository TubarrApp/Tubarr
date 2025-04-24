// Package metarr builds and runs Metarr commands.
package metarr

import (
	"fmt"
	"log"
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
			viperKey:    keys.InputFileDatePfx,
			cmdKey:      metcmd.FilenameDateTag,
		},
		{
			metarrValue: metVals{strSlice: v.MetarrArgs.FilenameReplaceSfx},
			valType:     strSlice,
			viperKey:    keys.FilenameReplaceSuffix,
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
			viperKey:    keys.MetaOps,
			cmdKey:      metcmd.MetaOps,
		},
		{
			metarrValue: metVals{str: v.MetarrArgs.MinFreeMem},
			valType:     str,
			viperKey:    keys.MinFreeMem,
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
			viperKey:    keys.RenameStyle,
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

	if strings.ContainsRune(v.VideoPath, '"') {
		newVideoPath := strings.ReplaceAll(v.VideoPath, `"`, ``)
		err := os.Rename(v.VideoPath, newVideoPath)
		if err != nil {
			log.Printf("Failed to rename file: %v", err)
		} else {
			v.VideoPath = newVideoPath
		}
	}

	if strings.ContainsRune(v.JSONPath, '"') {
		newJSONPath := strings.ReplaceAll(v.JSONPath, `"`, ``)
		err := os.Rename(v.JSONPath, newJSONPath)
		if err != nil {
			log.Printf("Failed to rename file: %v", err)
		} else {
			v.JSONPath = newJSONPath
		}
	}

	argMap[metcmd.VideoFile] = `"` + v.VideoPath + `"`

	if v.JSONCustomFile == "" {
		argMap[metcmd.JSONFile] = `"` + v.JSONPath + `"`
		logging.I("Making Metarr argument for video %q and JSON file %q.", v.VideoPath, v.JSONPath)
	} else {
		argMap[metcmd.JSONFile] = `"` + v.JSONCustomFile + `"`
		logging.I("Making Metarr argument for video %q and JSON file %q.", v.VideoPath, v.JSONCustomFile)
	}

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
			argSlicesMap[f.cmdKey] = f.metarrValue.strSlice
		} else if f.viperKey != "" && cfg.IsSet(f.viperKey) {
			argSlicesMap[f.cmdKey] = cfg.GetStringSlice(f.viperKey)
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
