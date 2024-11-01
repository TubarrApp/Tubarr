package command

import (
	"Tubarr/internal/config"
	preset "Tubarr/internal/config/presets"
	keys "Tubarr/internal/domain/keys"
	"Tubarr/internal/models"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type MetarrCommand struct {
	Commands map[string][]string
}

// NewMetarrCommandBuilder returns a new command builder to build and run Metarr commands
func NewMetarrCommandBuilder() *MetarrCommand {
	return &MetarrCommand{}
}

func (mc *MetarrCommand) MakeMetarrCommands(d []*models.DownloadedFiles) ([]*exec.Cmd, error) {
	if len(d) == 0 {
		return nil, fmt.Errorf("no downloaded file models")
	}
	if err := mc.ParsePresets(d); err != nil {
		return nil, fmt.Errorf("failed to parse command presets")
	}
	commands := make([]*exec.Cmd, 0)
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
	var fileCommandMap = make(map[string][]string)

	for _, model := range d {
		args := make([]string, 0)

		outputPath := config.GetString(keys.MoveOnComplete)
		outputExt := config.GetString(keys.OutputFiletype)

		args = append(args, "-V", model.VideoFilename)
		args = append(args, "-J", model.JSONFilename)

		if mPresetFilepath != "" { // Parse preset file if the path exists
			content, err := os.ReadFile(mPresetFilepath)
			if err != nil {
				return fmt.Errorf("error reading file '%s': %w", mPresetFilepath, err)
			}
			cStr := string(content)

			args = append(args, mc.filenameReplaceSuffix(cStr)...)
			args = append(args, mc.metaReplaceSuffix(cStr)...)
			args = append(args, mc.metaReplacePrefix(cStr)...)
			args = append(args, mc.metaAddField(cStr)...)
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
		args = append(args, "--meta-overwrite")

		if outputPath != "" {
			args = append(args, "--move-on-complete", outputPath)
		}
		if outputExt != "" {
			args = append(args, "-o", outputExt)
		}
		fileCommandMap[model.VideoFilename] = args
	}
	mc.Commands = fileCommandMap
	return nil
}

// dateTagFormat builds the date tag format to prefix filenames with
func (mc *MetarrCommand) dateTagFormat(c string) []string {
	var args []string
	if fDateTag := strings.Index(c, "[filename-date-tag]"); fDateTag != -1 {

		content := c[fDateTag+len("[filename-date-tag]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		if len(lines) > 0 {
			args = append(args, "--filename-date-tag")
		}
		for i, line := range lines {
			if line == "" {
				continue
			}
			if i == 0 {
				switch line {
				case "Ymd", "ymd", "mdY", "mdy", "dmY", "dmy":
					args = append(args, line)
				default:
					logging.PrintE(0, "Date tag format entry syntax is incorrect, should be in a format such as Ymd (for yyyy-mm-dd) or ymd (for yy-mm-dd) and so on...")
					args = append(args, "")
				}
			}
		}
	}
	return args
}

// metaAddField builds the argument for insertion of a new metafield
func (mc *MetarrCommand) metaAddField(c string) []string {
	var args []string
	if mAddField := strings.Index(c, "[meta-add-field]"); mAddField != -1 {

		content := c[mAddField+len("[meta-add-field]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		for i, line := range lines {
			if line == "" {
				continue
			}
			entry := strings.SplitN(line, ":", 2)
			if len(entry) != 2 {
				logging.PrintE(0, "Error in meta-add-field entry, please use syntax 'metatag:value'")
			} else {
				if i == 0 {
					args = append(args, "--meta-add-field")
				}
				args = append(args, line)
			}
		}
	}
	return args
}

// filenameReplaceSuffix builds the metadata suffix replacement argument for Metarr
func (mc *MetarrCommand) filenameReplaceSuffix(c string) []string {
	var args []string
	if fReplaceSfxIdx := strings.Index(c, "[filename-replace-suffix]"); fReplaceSfxIdx != -1 {

		content := c[fReplaceSfxIdx+len("[filename-replace-suffix]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		for i, line := range lines {
			if line == "" {
				continue
			}
			entry := strings.SplitN(line, ":", 2)
			if len(entry) != 2 {
				logging.PrintE(0, "Error in filename-replace-suffix entry, please use syntax 'suffix:replacement'")
			} else {
				if i == 0 {
					args = append(args, "--filename-replace-suffix")
				}
				switch {
				case i < len(lines)-1:
					args = append(args, line)
				default:
					args = append(args, line)
				}
			}
		}
	}
	return args
}

// metaReplaceSuffix builds the metadata suffix replacement argument for Metarr
func (mc *MetarrCommand) metaReplaceSuffix(c string) []string {
	var args []string
	if mReplaceSfxIdx := strings.Index(c, "[meta-replace-suffix]"); mReplaceSfxIdx != -1 {

		content := c[mReplaceSfxIdx+len("[meta-replace-suffix]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		for i, line := range lines {
			if line == "" {
				continue
			}
			entry := strings.SplitN(line, ":", 3)
			if len(entry) != 3 {
				logging.PrintE(0, "Error in meta-replace-suffix entry, please use syntax 'metatag:suffix:replacement'")
			} else {
				if i == 0 {
					args = append(args, "--meta-replace-suffix")
				}
				switch {
				case i < len(lines)-1:
					args = append(args, line)
				default:
					args = append(args, line)
				}
			}
		}
	}
	return args
}

// metaReplacePrefix builds the metadata suffix replacement argument for Metarr
func (mc *MetarrCommand) metaReplacePrefix(c string) []string {
	var args []string
	if mReplacePfxIdx := strings.Index(c, "[meta-replace-prefix]"); mReplacePfxIdx != -1 {

		content := c[mReplacePfxIdx+len("[meta-replace-prefix]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		for i, line := range lines {
			if line == "" {
				continue
			}
			entry := strings.SplitN(line, ":", 3)
			if len(entry) != 3 {
				logging.PrintE(0, "Error in meta-replace-suffix entry, please use syntax 'metatag:prefix:replacement'")
			} else {
				if i == 0 {
					args = append(args, "--meta-replace-prefix")
				}
				switch {
				case i < len(lines)-1:
					args = append(args, line)
				default:
					args = append(args, line)
				}
			}
		}
	}
	return args
}

// renameStyle is the chosen style of renaming, e.g. spaces, underscores
func (mc *MetarrCommand) renameStyle(c string) []string {
	var args []string
	if rStyle := strings.Index(c, "[rename-style]"); rStyle != -1 {

		content := c[rStyle+len("[rename-style]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		if len(lines) > 0 {
			args = append(args, "-r")
		}
		for i, line := range lines {
			if line == "" {
				continue
			}
			if i == 0 {
				switch line {
				case "spaces", "underscores", "skip":
					args = append(args, line)
				default:
					logging.PrintE(0, "Rename style entry syntax is incorrect, should be spaces, underscores, or skip.")
					args = append(args, "skip")
				}
			}
		}
	}
	return args
}

// outputLocation designates the output directory
func (mc *MetarrCommand) outputLocation(c string) ([]string, error) {
	var args []string
	if oDir := strings.Index(c, "[output-directory]"); oDir != -1 {

		content := c[oDir+len("[output-directory]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		if len(lines) > 0 {
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

				args = append(args, "--move-on-complete", lines[0])
			}
		}
	}
	return args, nil
}

// renameStyle is the chosen style of renaming, e.g. spaces, underscores
func (mc *MetarrCommand) outputExtension(c string) ([]string, error) {
	var args []string
	if oExt := strings.Index(c, "[output-extension]"); oExt != -1 {

		content := c[oExt+len("[output-extension]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

		lines = mc.removeEmptyLines(lines)

		if len(lines) > 0 {
			lines[0] = strings.TrimPrefix(lines[0], ".")
			switch lines[0] {
			case "3gp", "avi", "f4v", "flv", "m4v", "mkv",
				"mov", "mp4", "mpeg", "mpg", "ogm", "ogv",
				"ts", "vob", "webm", "wmv":

				args = append(args, "-o", lines[0])
			default:
				return args, fmt.Errorf("incorrect syntax for file extension")
			}

		}
	}
	return args, nil
}

// removeEmptyLines strips empty lines from the result
func (mc *MetarrCommand) removeEmptyLines(lines []string) []string {
	var rtn []string
	for _, line := range lines {
		if line == "" || line == "\n" {
			continue
		}
		rtn = append(rtn, line)
	}
	switch {
	case len(rtn) > 0:
		return rtn
	default:
		rtn = append(rtn, "")
		return rtn
	}
}
