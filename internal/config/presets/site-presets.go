package config

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// AutoPreset is the entrypoint for command presets
func AutoPreset(url string) []string {
	args := make([]string, 0)
	switch {
	case strings.Contains(url, "censored.tv"):
		return censoredTvPreset(args, url)
	default:
		return defaultArgs(args)
	}
}

// censoredTvPreset sets common presets for censored.tv links
func censoredTvPreset(args []string, url string) []string {

	// Titles come appended with " (1)"
	// Ids come appended with "-1"
	// Filenames come appended with " 1"

	splitUrl := strings.Split(url, "/")
	creator := ""

	for i, entry := range splitUrl {
		if !strings.HasSuffix(entry, ".com") {
			continue
		}
		if len(splitUrl) >= i+1 {
			creator = strings.ReplaceAll(splitUrl[i+1], "-", " ")
		}
	}

	if creator != "" { // Should ensure the segment directly after .com/ was grabbed

		titleCase := cases.Title(language.English)
		creator = titleCase.String(creator)

		switch strings.ToLower(creator) {
		case "atheism is unstoppable": // Special case for Atheism-is-Unstoppable
			creator = "Atheism-is-Unstoppable"
		}

		args = append(args, "--meta-add-field", "author:"+creator, "actor:"+creator, "publisher:"+creator)
	}

	args = append(args, "--filename-date-tag", "ymd")
	args = append(args, "--meta-replace-suffix", "title: (1):", "fulltitle: (1):", "id:-1:", "display_id:-1:")
	args = append(args, "-r", "spaces")
	args = append(args, "--filename-replace-suffix", " 1:")
	args = append(args, "--meta-overwrite")

	return args
}

// defaultArgs provides default Metarr arguments
func defaultArgs(args []string) []string {

	// Currently returns no arguments

	return args
}
