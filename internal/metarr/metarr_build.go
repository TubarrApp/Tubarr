package metarr

import (
	"fmt"
	"strconv"
	"strings"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// mergeArguments combines arguments from both Viper config and model settings
func makeMetarrCommand(v *models.Video) ([]string, error) {

	baseArgs := []string{
		"-V", v.VPath,
		"-J", v.JPath,
	}

	dirParser := parsing.NewDirectoryParser(v.Channel, v)

	if cfg.IsSet(keys.MoveOnComplete) && v.MetarrArgs.OutputDir == "" {
		logging.I("Move on complete flag set, checking output directory for template directives...")
		if parsedDir, err := dirParser.ParseDirectory(cfg.GetString(keys.MoveOnComplete)); err != nil {
			logging.E(0, err.Error())
		} else {
			logging.S(0, "Updated output directory to %q", parsedDir)
			cfg.Set(keys.MoveOnComplete, parsedDir)
		}
	}

	var parsedOutputDir string
	if v.MetarrArgs.OutputDir != "" {
		dirParser := parsing.NewDirectoryParser(v.Channel, v)
		parsed, err := dirParser.ParseDirectory(v.MetarrArgs.OutputDir)
		if err != nil {
			return nil, fmt.Errorf("failed to parse output directory template: %w", err)
		}
		parsedOutputDir = parsed
	}

	// Map to deduplicate meta
	metaOpsMap := make(map[string]struct{})

	// Helper to add meta operations with deduplication
	addMetaOps := func(ops []string) {
		for _, op := range ops {
			metaOpsMap[op] = struct{}{}
		}
	}

	// Add model-based meta operations
	if v.MetarrArgs.MetaOps != nil {
		logging.D(2, "Adding MetarrArgs meta-ops: %v", v.MetarrArgs.MetaOps)
		addMetaOps(v.MetarrArgs.MetaOps)
	}

	// Add viper meta operations
	if cfg.IsSet(keys.MetaOps) {
		viperOps := cfg.GetStringSlice(keys.MetaOps)
		logging.D(2, "Adding Viper meta-ops: %v", viperOps)
		addMetaOps(viperOps)
	}

	// Use map for other unique arguments
	argMap := make(map[string]string)

	// Add other model-based arguments
	if v.MetarrArgs.FileDatePfx != "" {
		argMap["--filename-date-tag"] = v.MetarrArgs.FileDatePfx
	}
	if v.MetarrArgs.RenameStyle != "" {
		argMap["-r"] = v.MetarrArgs.RenameStyle
	}
	if v.MetarrArgs.FilenameReplaceSfx != "" {
		argMap["--filename-replace-suffix"] = v.MetarrArgs.FilenameReplaceSfx
	}
	if parsedOutputDir != "" {
		argMap["-o"] = parsedOutputDir
	}

	// Add Viper config arguments
	if cfg.IsSet(keys.InputFileDatePfx) {
		argMap["--filename-date-tag"] = cfg.GetString(keys.InputFileDatePfx)
	}

	if cfg.IsSet(keys.RenameStyle) {
		argMap["-r"] = cfg.GetString(keys.RenameStyle)
	}

	if cfg.IsSet(keys.InputFilenameReplaceSfx) {
		replacements := cfg.GetStringSlice(keys.InputFilenameReplaceSfx)
		if len(replacements) > 0 {
			argMap["--filename-replace-suffix"] = replacements[len(replacements)-1]
		}
	}

	if cfg.IsSet(keys.MoveOnComplete) {
		argMap["-o"] = cfg.GetString(keys.MoveOnComplete)
	}

	if cfg.IsSet(keys.OutputFiletype) {
		argMap["--ext"] = cfg.GetString(keys.OutputFiletype)
	}

	if cfg.IsSet(keys.DebugLevel) {
		argMap["--debug"] = strconv.Itoa(cfg.GetInt(keys.DebugLevel))
	}

	// Build final argument list
	args := baseArgs

	// Add regular arguments from map
	for flag, value := range argMap {
		args = append(args, flag, value)
	}

	var uniqueMetaOps []string
	for op := range metaOpsMap {
		uniqueMetaOps = append(uniqueMetaOps, op)
	}

	// Add all unique meta operations
	for _, op := range uniqueMetaOps {
		args = append(args, "--meta-ops", op)
	}

	logging.D(1, "Final Metarr arguments: %s", strings.Join(args, " "))
	logging.D(2, "Unique meta operations: %v", uniqueMetaOps)

	return args, nil
}
