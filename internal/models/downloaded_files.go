package models

import "os/exec"

type DownloadedFiles struct {
	CookieSource     string
	CustomCookieFile string
	DownloadCommand  *exec.Cmd
	ExternalDler     string
	ExternalDlerArgs string
	JSONFilename     string
	URL              string
	VideoDirectory   string
	VideoFilename    string
}
