package metarr

import (
	"fmt"
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
	metarrValue interface{}
	viperKey    string
	cmdKey      string
}

// mergeArguments combines arguments from both Viper config and model settings
func makeMetarrCommand(v *models.Video) []string {
	args := []string{
		metcmd.VideoFile, v.VPath,
		metcmd.JSONFile, v.JPath,
	}

	// Output directory
	parsedOutputDir := parseOutputDir(v)

	// Use map for other unique arguments
	argMap := make(map[string]string)

	fields := []metCmdMapping{
		{v.MetarrArgs.Concurrency,
			"",
			metcmd.Concurrency},

		{v.MetarrArgs.Ext,
			keys.OutputFiletype,
			metcmd.Ext},

		{v.MetarrArgs.FileDatePfx,
			keys.InputFileDatePfx,
			metcmd.FilenameDateTag},

		{v.MetarrArgs.FilenameReplaceSfx,
			keys.InputFilenameReplaceSfx,
			metcmd.FilenameReplaceSfx},

		{v.MetarrArgs.MaxCPU,
			"",
			metcmd.MaxCPU},

		{v.MetarrArgs.MinFreeMem,
			keys.MinFreeMem,
			metcmd.MinFreeMem},

		{v.MetarrArgs.RenameStyle,
			keys.RenameStyle,
			metcmd.RenameStyle},

		{parsedOutputDir,
			keys.MoveOnComplete,
			metcmd.OutputDir},

		{"",
			keys.DebugLevel,
			metcmd.Debug},
	}

	for _, f := range fields {
		switch val := f.metarrValue.(type) {

		case int:
			if val != 0 {
				argMap[f.cmdKey] = strconv.Itoa(val)

			} else if cfg.IsSet(f.viperKey) {
				argMap[f.cmdKey] = strconv.Itoa(cfg.GetInt(f.viperKey))
			}

		case float64:
			if val != 0 {
				argMap[f.cmdKey] = fmt.Sprintf("%.2f", val)

			} else if cfg.IsSet(f.viperKey) {
				argMap[f.cmdKey] = fmt.Sprintf("%.2f", cfg.GetFloat64(f.viperKey))
			}

		case string:
			if val != "" {
				argMap[f.cmdKey] = val

			} else if cfg.IsSet(f.viperKey) {
				viperVal := cfg.Get(f.viperKey)

				switch strVal := viperVal.(type) {
				case string:
					argMap[f.cmdKey] = strVal
				case []string:
					argMap[f.cmdKey] = strings.Join(strVal, ",")
				}
			}
		}
	}

	for key, value := range argMap {
		args = append(args, key, value)
	}

	logging.I("Built Metarr argument list: %v", args)
	return args
}

// parseOutputDir returns a parsed output directory
func parseOutputDir(v *models.Video) string {
	dirParser := parsing.NewDirectoryParser(v.Channel, v)
	switch {
	case cfg.IsSet(keys.MoveOnComplete) && v.MetarrArgs.OutputDir == "":

		parsedDir, err := dirParser.ParseDirectory(cfg.GetString(keys.MoveOnComplete))
		if err != nil {
			logging.E(0, err.Error())
			break
		}
		cfg.Set(keys.MoveOnComplete, parsedDir)
		return parsedDir

	case v.MetarrArgs.OutputDir != "":

		parsed, err := dirParser.ParseDirectory(v.MetarrArgs.OutputDir)
		if err != nil {
			logging.E(0, err.Error())
			break
		}
		return parsed
	}
	return ""
}
