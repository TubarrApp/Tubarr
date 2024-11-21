package cfg

import (
	"fmt"
	"strings"
	"tubarr/internal/utils/logging"
)

var (
	filenameReplaceSuffixInput []string
)

// validateMetaOps parses the meta transformation operations
func validateMetaOps(metaOps []string) ([]string, error) {
	if len(metaOps) == 0 {
		logging.I("No meta operations passed in to verification")
		return metaOps, nil
	}

	logging.D(1, "Validating meta operations...")
	valid := make([]string, 0, len(metaOps))

	for _, m := range metaOps {
		split := strings.Split(m, ":")
		switch len(split) {
		case 3:
			switch split[1] {
			case "append", "copy-to", "paste-from", "prefix", "trim-prefix", "trim-suffix", "replace", "set":
				valid = append(valid, m)
			default:
				return nil, fmt.Errorf("invalid meta operation %q", split[1])
			}
		case 4:
			if split[1] == "date-tag" {
				switch split[2] {
				case "prefix", "suffix":
					if dateFormat(split[3]) {
						valid = append(valid, m)
					}
				default:
					return nil, fmt.Errorf("invalid date tag location %q, use prefix or suffix", split[2])
				}
			}
		default:
			return nil, fmt.Errorf("invalid meta op %q", m)
		}
	}

	if len(valid) != 0 {
		return valid, nil
	}

	return nil, fmt.Errorf("no valid meta operations")
}

// validateFilenameSuffixReplace checks if the input format for filename suffix replacement is valid
func validateFilenameSuffixReplace(fileSfxReplace []string) ([]string, error) {
	valid := make([]string, 0, len(fileSfxReplace))

	for _, pair := range fileSfxReplace {
		parts := strings.Split(pair, ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid use of filename-replace-suffix, values must be written as (suffix:replacement)")
		}
		valid = append(valid, pair)
	}
	return valid, nil
}

// setRenameFlag sets the rename style to apply
func setRenameFlag(flag string) error {

	// Trim whitespace for more robust validation
	flag = strings.TrimSpace(flag)
	flag = strings.ToLower(flag)

	switch flag {
	case "spaces", "space", "underscores", "underscore", "fixes", "fix", "fixes-only", "fixesonly":
		return nil
	default:
		return fmt.Errorf("'spaces', 'underscores' or 'fixes-only' not selected for renaming style, skipping these modifications")
	}
}

// dateEnum returns the date format enum type
func dateFormat(dateFmt string) bool {
	if len(dateFmt) > 2 {
		switch dateFmt {
		case "Ymd", "ymd", "Ydm", "ydm", "dmY", "dmy", "mdY", "mdy", "md", "dm":
			return true
		}
	}
	logging.E(0, "Invalid date format entered as %q, please enter up to three characters (where 'Y' is yyyy and 'y' is yy)", dateFmt)
	return false
}
