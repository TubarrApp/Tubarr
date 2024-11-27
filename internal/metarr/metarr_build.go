// Package metarr builds and runs Metarr commands.
package metarr

import (
	"fmt"
	"strconv"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/metcmd"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

type metCmdMapping struct {
	metarrValue any
	viperKey    string
	cmdKey      string
}

// makeMetarrCommand combines arguments from both Viper config and model settings.
func makeMetarrCommand(v *models.Video) []string {
	fields := []metCmdMapping{
		// Metarr args:
		{
			metarrValue: v.MetarrArgs.Concurrency,
			viperKey:    "",
			cmdKey:      metcmd.Concurrency,
		},
		{
			metarrValue: v.MetarrArgs.Ext,
			viperKey:    keys.OutputFiletype,
			cmdKey:      metcmd.Ext,
		},
		{
			metarrValue: v.MetarrArgs.FileDatePfx,
			viperKey:    keys.InputFileDatePfx,
			cmdKey:      metcmd.FilenameDateTag,
		},
		{
			metarrValue: v.MetarrArgs.FilenameReplaceSfx,
			viperKey:    keys.InputFilenameReplaceSfx,
			cmdKey:      metcmd.FilenameReplaceSfx,
		},
		{
			metarrValue: v.MetarrArgs.MaxCPU,
			viperKey:    "",
			cmdKey:      metcmd.MaxCPU,
		},
		{
			metarrValue: v.MetarrArgs.MetaOps,
			viperKey:    keys.MetaOps,
			cmdKey:      metcmd.MetaOps,
		},
		{
			metarrValue: v.MetarrArgs.MinFreeMem,
			viperKey:    keys.MinFreeMem,
			cmdKey:      metcmd.MinFreeMem,
		},
		{
			metarrValue: parseOutputDir(v), // Already parses from Viper key if set and model output dir empty.
			viperKey:    "",
			cmdKey:      metcmd.OutputDir,
		},
		{
			metarrValue: v.MetarrArgs.RenameStyle,
			viperKey:    keys.RenameStyle,
			cmdKey:      metcmd.RenameStyle,
		},
		// Other
		{
			metarrValue: "",
			viperKey:    keys.DebugLevel,
			cmdKey:      metcmd.Debug,
		},
	}

	// Map use to ensure uniqueness
	argMap := make(map[string]string, len(fields))
	argSlicesMap := make(map[string][]string, len(fields))

	// Final args
	args := make([]string, 0, len(fields))
	args = append(args, metcmd.VideoFile, v.VPath)
	args = append(args, metcmd.JSONFile, v.JPath)

	for _, f := range fields {
		processField(f, argMap, argSlicesMap)
	}

	for k, v := range argMap {
		args = append(args, k, v)
	}

	for k, v := range argSlicesMap {
		for _, val := range v {
			args = append(args, k, val)
		}
	}

	logging.I("Built Metarr argument list: %v", args)
	return args
}

// processField processes each field in the argument map.
func processField(f metCmdMapping, argMap map[string]string, argSlicesMap map[string][]string) {
	switch val := f.metarrValue.(type) {
	case int:
		if val > 0 {
			argMap[f.cmdKey] = strconv.Itoa(val)
		} else if cfg.IsSet(f.viperKey) {
			argMap[f.cmdKey] = strconv.Itoa(cfg.GetInt(f.viperKey))
		}
	case float64:
		if val > 0.0 {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", val)
		} else if cfg.IsSet(f.viperKey) {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", cfg.GetFloat64(f.viperKey))
		}
	case string:
		if val != "" {
			argMap[f.cmdKey] = val
		} else if cfg.IsSet(f.viperKey) {
			argMap[f.cmdKey] = cfg.GetString(f.viperKey)
		}
	case []string:
		if len(val) > 0 {
			argSlicesMap[f.cmdKey] = val
		} else if cfg.IsSet(f.viperKey) {
			argSlicesMap[f.cmdKey] = cfg.GetStringSlice(f.viperKey)
		}
	}
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
