package config

import (
	"strings"
)

func AutoPreset(url string) {
	if strings.Contains(url, "censored.tv") {
		censoredTvPreset()
	}
}

func censoredTvPreset() {}
