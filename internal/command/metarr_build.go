package command

import (
	"os/exec"
	"strconv"
	"strings"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

type MetarrCommand struct {
	Video *models.Video
}

// NewMetarrCommandBuilder returns a new command builder to build and run Metarr commands
func NewMetarrCommandBuilder(v *models.Video) *MetarrCommand {
	return &MetarrCommand{
		Video: v,
	}
}

// MakeMetarrCommands builds the command list for Metarr
func (mc *MetarrCommand) MakeMetarrCommands() (*exec.Cmd, error) {
	// Get merged arguments from both sources
	args := mc.mergeArguments()

	cmd := exec.Command("metarr", args...)
	return cmd, nil
}

// mergeArguments combines arguments from both Viper config and model settings
func (mc *MetarrCommand) mergeArguments() []string {

	baseArgs := []string{
		"-V", mc.Video.VPath,
		"-J", mc.Video.JPath,
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
	if mc.Video.MetarrArgs.MetaOps != nil {
		logging.D(2, "Adding MetarrArgs meta-ops: %v", mc.Video.MetarrArgs.MetaOps)
		addMetaOps(mc.Video.MetarrArgs.MetaOps)
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
	if mc.Video.MetarrArgs.FileDatePfx != "" {
		argMap["--filename-date-tag"] = mc.Video.MetarrArgs.FileDatePfx
	}
	if mc.Video.MetarrArgs.RenameStyle != "" {
		argMap["-r"] = mc.Video.MetarrArgs.RenameStyle
	}
	if mc.Video.MetarrArgs.FilenameReplaceSfx != "" {
		argMap["--filename-replace-suffix"] = mc.Video.MetarrArgs.FilenameReplaceSfx
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

	return args
}
