package command

import (
	config "Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	"Tubarr/internal/models"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type MetarrCommand struct{}

// NewMetarrCommandBuilder returns a new command builder to build and run Metarr commands
func NewMetarrCommandBuilder() *MetarrCommand {
	return &MetarrCommand{}
}

// RunMetarr runs a Metarr command with a built argument list
func (mc *MetarrCommand) RunMetarr(commands map[string][]string) error {

	var err error

	for _, args := range commands {
		command := exec.Command("metarr", args...)
		logging.PrintI("Running command: %s", command.String())

		command.Stderr = os.Stderr
		command.Stdout = os.Stdout
		command.Stdin = os.Stdin

		if err = command.Run(); err != nil {
			logging.PrintE(0, "Encountered error running command '%s': %w", command.String(), err)
		}
	}
	return err // Nil by default
}

// ParseMetarrPreset parses the Metarr from a preset file
func (mc *MetarrCommand) ParseMetarrPreset(d []models.DownloadedFiles) (map[string][]string, error) {

	logging.PrintI("Sending to Metarr for metadata insertion")

	var fileCommandMap = make(map[string][]string)

	for _, model := range d {
		mPresetFilepath := config.GetString(keys.MetarrPreset)

		content, err := os.ReadFile(mPresetFilepath)
		if err != nil {
			return nil, fmt.Errorf("error reading file '%s': %w", mPresetFilepath, err)
		}
		cStr := string(content)

		var args []string

		args = append(args, "-V", model.VideoFilename)
		args = append(args, "-J", model.JSONFilename)

		args = append(args, mc.filenameReplaceSuffix(cStr)...)
		args = append(args, mc.metaReplaceSuffix(cStr)...)
		args = append(args, mc.metaAddField(cStr)...)
		args = append(args, mc.dateTagFormat(cStr)...)
		args = append(args, mc.renameStyle(cStr)...)
		args = append(args, "--meta-overwrite")

		if config.IsSet(keys.MoveOnComplete) {
			args = append(args, "--move-on-complete", config.GetString(keys.MoveOnComplete))
		}

		fileCommandMap[model.VideoFilename] = args
	}

	return fileCommandMap, nil
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
