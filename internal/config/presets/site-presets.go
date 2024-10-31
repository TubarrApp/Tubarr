package config

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func AutoPreset(url string) []string {

	var args []string
	switch {
	case strings.Contains(url, "censored.tv"):
		return censoredTvPreset(args, url)
	}
	return nil
}

func censoredTvPreset(args []string, url string) []string {

	// Titles come appended with " (1)"
	// Ids come appended with "-1"
	// Filenames come appended with " 1"

	splitUrl := strings.Split(url, "/")
	creator := ""

	for i, entry := range splitUrl {
		if !strings.HasSuffix(entry, ".com/") {
			continue
		}
		if len(splitUrl) >= i+1 {
			creator = strings.ReplaceAll(splitUrl[i+1], "-", " ")
		}
	}

	if creator != "" { // Should ensure the segment directly after .com/ was grabbed

		titleCase := cases.Title(language.English)
		creator := titleCase.String(creator)

		switch strings.ToLower(creator) {
		case "atheism is unstoppable": // Devon Tracey uses a hyphenated username
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
