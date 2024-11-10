package command

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"tubarr/internal/config"
	preset "tubarr/internal/config/presets"
	consts "tubarr/internal/domain/constants"
	enums "tubarr/internal/domain/enums"
	keys "tubarr/internal/domain/keys"
	tags "tubarr/internal/domain/tags"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

type MetarrCommand struct {
	Commands map[string][]string
}

// NewMetarrCommandBuilder returns a new command builder to build and run Metarr commands
func NewMetarrCommandBuilder() *MetarrCommand {
	return &MetarrCommand{
		Commands: make(map[string][]string),
	}
}

// MakeMetarrCommands builds the command list for Metarr
func (mc *MetarrCommand) MakeMetarrCommands(d []*models.DownloadedFiles) ([]*exec.Cmd, error) {
	if len(d) == 0 {
		return nil, fmt.Errorf("no downloaded file models")
	}

	if _, err := exec.LookPath("metarr"); err != nil {
		return nil, fmt.Errorf("metarr command not found in PATH: %w", err)
	}

	if err := mc.ParsePresets(d); err != nil {
		return nil, fmt.Errorf("failed to parse command presets")
	}

	commands := make([]*exec.Cmd, 0, len(mc.Commands))
	for _, args := range mc.Commands {
		command := exec.Command("metarr", args...)
		commands = append(commands, command)
	}

	return commands, nil
}

// ParseMetarrPreset parses the Metarr from a preset file
func (mc *MetarrCommand) ParsePresets(d []*models.DownloadedFiles) error {

	logging.PrintI("Sending to Metarr for metadata insertion")

	mPresetFilepath := config.GetString(keys.MetarrPreset)
	if mPresetFilepath != "" {
		if _, err := os.Stat(mPresetFilepath); err != nil {
			logging.PrintE(0, "Preset file not accessible: (%v) Clearing key...", err)
			config.Set(keys.MetarrPreset, "")
		}
	}

	if mc.Commands == nil {
		mc.Commands = make(map[string][]string)
	}

	for _, model := range d {
		if model == nil {
			continue
		}

		args := []string{}

		var (
			outputPath, outputExt string
		)

		if config.IsSet(keys.MoveOnComplete) {
			outputPath = config.GetString(keys.MoveOnComplete)
		}

		if config.IsSet(keys.OutputFiletype) {
			outputExt = config.GetString(keys.OutputFiletype)
		}

		args = append(args, "-V", model.VideoFilename)
		args = append(args, "-J", model.JSONFilename)

		if mPresetFilepath != "" { // Parse preset file
			content, err := os.ReadFile(mPresetFilepath)
			if err != nil {
				return fmt.Errorf("error reading file '%s': %w", mPresetFilepath, err)
			}
			cStr := string(content)

			args = append(args, mc.metaOps(cStr)...)
			args = append(args, mc.dateTagFormat(cStr)...)
			args = append(args, mc.renameStyle(cStr)...)

			if outputPath == "" {
				if rtn, err := mc.outputLocation(cStr); err == nil {
					args = append(args, rtn...)
				}
			}
			if outputExt == "" {
				if rtn, err := mc.outputExtension(cStr); err == nil {
					args = append(args, rtn...)
				}
			}
		} else { // Fallback to auto-preset detection
			args = preset.AutoPreset(model.URL)
		}

		if outputPath != "" {
			args = append(args, "-o", outputPath)
		}

		if outputExt != "" {
			args = append(args, "--ext", outputExt)
		}

		if config.IsSet(keys.DebugLevel) {
			dLevel := config.GetInt(keys.DebugLevel)
			args = append(args, "-d", strconv.Itoa(dLevel))
		}

		mc.Commands[model.VideoFilename] = args
	}
	return nil
}

// dateTagFormat builds the date tag format to prefix filenames with
func (mc *MetarrCommand) dateTagFormat(c string) []string {

	lines, exists := mc.getFieldContent(c, tags.MetarrFilenameDate, enums.L_SINGLE)
	var args = make([]string, 0, len(lines)*2)

	if exists && len(lines) > 0 {

		lines[0] = strings.TrimSpace(lines[0])

		switch lines[0] {
		case "Ymd", "ymd", "mdY", "mdy", "dmY", "dmy":
			args = append(args, "--filename-date-tag", lines[0])
		default:
			logging.PrintE(0, "Date tag format entry syntax is incorrect, should be in a format such as Ymd (for yyyy-mm-dd) or ymd (for yy-mm-dd) and so on...")
		}
	}
	return args
}

// metaOps adds the commands for manipulating metadata
func (mc *MetarrCommand) metaOps(c string) []string {

	var shouldOW bool

	lines, exists := mc.getFieldContent(c, tags.MetarrMetaOps, enums.L_MULTI)
	var args = make([]string, 0, len(lines)*2)

	if exists && len(lines) > 0 {
		for _, line := range lines {
			if line == "" {
				continue
			}

			entry := strings.Split(line, ":")
			if len(entry) < 3 {
				logging.PrintE(0, "Error in new metadata field entry, entry shorter than 3 (should be at least 'field:operation:value')")
			} else {
				args = append(args, "--meta-ops", line)
				if entry[1] == "set" {
					shouldOW = true
				}
			}
		}
	}

	if shouldOW {
		args = append(args, "--meta-overwrite")
	}

	return args
}

// renameStyle is the chosen style of renaming, e.g. spaces, underscores
func (mc *MetarrCommand) renameStyle(c string) []string {

	lines, exists := mc.getFieldContent(c, tags.MetarrRenameStyle, enums.L_SINGLE)
	var args = make([]string, 0, len(lines)*2)

	if exists && len(lines) > 0 {
		lines[0] = strings.TrimSpace(lines[0])

		switch lines[0] {
		case "spaces", "underscores", "skip":
			args = append(args, "-r", lines[0])
		default:
			logging.PrintE(0, "Rename style entry syntax is incorrect, should be spaces, underscores, or skip.")
			args = append(args, "-r", "skip")
		}
	}
	return args
}

// outputLocation designates the output directory
func (mc *MetarrCommand) outputLocation(c string) ([]string, error) {

	lines, exists := mc.getFieldContent(c, tags.MetarrOutputDir, enums.L_SINGLE)
	var args = make([]string, 0, len(lines)*2)

	if exists && len(lines) > 0 {
		if lines[0] != "" {
			dir, err := os.Stat(lines[0])
			if err != nil {
				return args, fmt.Errorf("error with output directory: %w", err)
			} else if os.IsNotExist(err) {
				return args, fmt.Errorf("target output directory does not exist: %w", err)
			}

			if !dir.IsDir() {
				return args, fmt.Errorf("output location is not a directory")
			}

			args = append(args, "-o", lines[0])
		}
	}
	return args, nil
}

// outputExtension is the filetype extension to output files as
func (mc *MetarrCommand) outputExtension(c string) ([]string, error) {

	lines, exists := mc.getFieldContent(c, tags.MetarrOutputExt, enums.L_SINGLE)
	var args = make([]string, 0, len(lines)*2)

	if exists && len(lines) > 0 {
		lines[0] = strings.TrimSpace(lines[0])
		lines[0] = strings.TrimPrefix(lines[0], ".")

		filled := false
		for _, ext := range consts.AllVidExtensions {
			if ext == lines[0] {
				args = append(args, "--ext", ext)
				filled = true
			}
		}
		if !filled {
			return args, fmt.Errorf("incorrect file extension '%s' entered for yt-dlp", lines[0])
		}
	}
	return args, nil
}

// removeEmptyLines strips empty lines from the result
func (mc *MetarrCommand) removeEmptyLines(input []string) []string {
	var lines = make([]string, 0, len(input))
	for _, line := range input {
		if line == "" || line == "\r" { // \n delimiter already removed by strings.Split
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// getFieldContent extracts the content inside the field
func (mc *MetarrCommand) getFieldContent(c, tag string, selectType enums.LineSelectType) ([]string, bool) {
	if tagLoc := strings.Index(c, tag); tagLoc != -1 {

		content := c[tagLoc+len(tag)+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx]
		}

		content = cleanNewLines(content)
		gotLines := strings.Split(content, "\n")

		lines := mc.removeEmptyLines(gotLines)

		if len(lines) > 0 {
			if selectType == enums.L_SINGLE {
				return []string{lines[0]}, true
			}
			// Returns multi
			return lines, true
		}
		logging.PrintD(2, "Lines grabbed empty for tag '%s' and content '%s'", tag, c)
		return nil, false
	}
	logging.PrintD(2, "Tag '%s' not found in content '%s'", tag, c)
	return nil, false
}

// cleanNewLines normalizes the newline patterns across different OS's
func cleanNewLines(s string) string {
	rep := strings.NewReplacer("\r\n", "\n", "\r", "\n")
	rep.Replace(s)
	return s
}
