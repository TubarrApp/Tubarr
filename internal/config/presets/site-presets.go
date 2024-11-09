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
	// Filenames come appended with "_1"

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

		args = append(args, "--meta-ops",
			"author:"+"set:"+creator,
			"actor:"+"set:"+creator,
			"publisher:"+"set:"+creator,
			"uploader:"+"set:"+creator,
			"channel:"+"set:"+creator)

		args = append(args, "--meta-overwrite")
	}

	args = append(args, "--meta-ops", "title:trim-suffix: (1)", "fulltitle:trim-suffix: (1)", "id:trim-suffix:-1", "display_id:trim-suffix:-1")
	args = append(args, "--filename-replace-suffix", "_1:")

	return args
}

// defaultArgs provides default Metarr arguments
func defaultArgs(args []string) []string {

	// Currently returns no arguments

	return args
}
