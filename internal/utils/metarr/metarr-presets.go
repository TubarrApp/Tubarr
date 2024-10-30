package metarr

import (
	config "Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
	"strings"
)

// ParseMetarrPreset parses the Metarr from a preset file
func ParseMetarrPreset() ([]string, error) {

	logging.PrintI("Sending to Metarr for metadata insertion")

	mPresetFilepath := config.GetString(keys.MetarrPreset)

	file, err := os.OpenFile(mPresetFilepath, os.O_RDONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("unable to open Metarr preset file '%s': %v", mPresetFilepath, err)
	}
	defer file.Close()

	content, err := os.ReadFile(mPresetFilepath)
	if err != nil {
		return nil, fmt.Errorf("error reading file '%s': %v", mPresetFilepath, err)
	}
	cStr := string(content)

	var args []string

	vDir := config.GetString(keys.VideoDir)
	mDir := config.GetString(keys.MetaDir)

	args = append(args, "-v", vDir)
	args = append(args, "-j", mDir)

	args = append(args, filenameReplaceSuffix(cStr)...)
	args = append(args, metaReplaceSuffix(cStr)...)
	args = append(args, metaAddField(cStr)...)
	args = append(args, dateTagFormat(cStr)...)
	args = append(args, renameStyle(cStr)...)
	args = append(args, "--meta-overwrite")

	return args, nil
}

// dateTagFormat builds the date tag format to prefix filenames with
func dateTagFormat(c string) []string {
	var args []string
	if fDateTag := strings.Index(c, "[filename-date-tag]"); fDateTag != -1 {

		content := c[fDateTag+len("[filename-date-tag]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

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
func metaAddField(c string) []string {
	var args []string
	if mAddField := strings.Index(c, "[meta-add-field]"); mAddField != -1 {

		content := c[mAddField+len("[meta-add-field]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

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
func filenameReplaceSuffix(c string) []string {
	var args []string
	if fReplaceSfxIdx := strings.Index(c, "[filename-replace-suffix]"); fReplaceSfxIdx != -1 {

		content := c[fReplaceSfxIdx+len("[filename-replace-suffix]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

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
func metaReplaceSuffix(c string) []string {
	var args []string
	if mReplaceSfxIdx := strings.Index(c, "[meta-replace-suffix]"); mReplaceSfxIdx != -1 {

		content := c[mReplaceSfxIdx+len("[meta-replace-suffix]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

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
func renameStyle(c string) []string {
	var args []string
	if rStyle := strings.Index(c, "[rename-style]"); rStyle != -1 {

		content := c[rStyle+len("[rename-style]")+1:]

		endIdx := strings.Index(content, "[")
		if endIdx != -1 {
			content = content[:endIdx-1]
		}
		lines := strings.Split(content, "\n")

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
