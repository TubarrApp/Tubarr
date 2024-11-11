package models

import (
	"os/exec"
	enums "tubarr/internal/domain/enums"
)

// DLRequest represents a download request
type DLRequest struct {
	URL             string
	DownloadArchive string
	Command         *exec.Cmd
}

type DLs struct {
	VideoCommand *exec.Cmd
	VideoPath    string
	VideoDir     string

	JSONCommand *exec.Cmd
	JSONPath    string
	JSONDir     string

	Metamap map[string]interface{}

	URL string
}

type DLFilter struct {
	Field      string
	Value      string
	FilterType enums.DLFilterType
}
