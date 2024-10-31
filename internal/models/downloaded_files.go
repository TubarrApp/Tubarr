package models

import "os/exec"

type DownloadedFiles struct {
	VideoDirectory   string
	VideoFilename    string
	JSONFilename     string
	URL              string
	CookieSource     string
	ExternalDler     string
	ExternalDlerArgs string
	DownloadCommand  *exec.Cmd
}
