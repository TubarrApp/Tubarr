package command

import (
	"os/exec"
	"tubarr/internal/cfg"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

type MetaDLRequest struct {
	DLs []*models.DLs
}

func NewMetaDLRequest(dls []*models.DLs) *MetaDLRequest {
	return &MetaDLRequest{
		DLs: dls,
	}
}

// RequestMetaCommand builds and returns the argument for downloading metadata
// files for the given URL
func (mdl *MetaDLRequest) RequestMetaCommand() {

	jsonDir := "."
	if cfg.IsSet(keys.JsonDir) {
		jsonDir = cfg.GetString(keys.JsonDir)
	}

	var buildArgs []string
	buildArgs = append(buildArgs, "--skip-download", "--write-info-json", "-P", jsonDir)

	if cfg.IsSet(keys.RestrictFilenames) {
		buildArgs = append(buildArgs, "--restrict_filenames", cfg.GetString(keys.RestrictFilenames))
	}

	if cfg.IsSet(keys.CookieSource) {
		buildArgs = append(buildArgs, "--cookies-from-browser", cfg.GetString(keys.CookieSource))
	} else if cfg.IsSet(keys.CookiePath) {
		buildArgs = append(buildArgs, "--cookies", cfg.GetString(keys.CookiePath))
	}

	if cfg.IsSet(keys.DLRetries) {
		buildArgs = append(buildArgs, "--retries", cfg.GetString(keys.DLRetries))
	}

	//buildArgs = append(buildArgs, "--quiet")
	buildArgs = append(buildArgs, "--restrict-filenames", "-o", "%(title)s.%(ext)s")

	for _, dl := range mdl.DLs {
		args := buildArgs
		args = append(args, dl.URL)
		dl.JSONCommand = exec.Command("yt-dlp", args...)
		logging.D(3, "Built command for URL '%s':\n%v", dl.URL, dl.JSONCommand.String())
	}
}
