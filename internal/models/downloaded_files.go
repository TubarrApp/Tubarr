package models

import "os/exec"

type DownloadedFiles struct {
	VideoFilename   string
	JSONFilename    string
	URL             string
	DownloadCommand *exec.Cmd
}
