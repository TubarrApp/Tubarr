package downloads

import (
	"context"
	"os/exec"
	"strconv"
	"tubarr/internal/domain/jsoncmd"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// RequestMetaCommand builds and returns the argument for downloading metadata files for the given URL.
func buildJSONCommand(v *models.Video) *exec.Cmd {

	args := make([]string, 0, 32)

	args = append(args,
		jsoncmd.SkipVideo,
		jsoncmd.WriteInfoJSON,
		jsoncmd.P, v.JDir)

	if v.Settings.CookieSource != "" {
		args = append(args, jsoncmd.CookieSource, v.Settings.CookieSource)
	}

	if v.Settings.MaxFilesize != "" {
		args = append(args, jsoncmd.MaxFilesize, v.Settings.MaxFilesize)
	}

	if v.Settings.ExternalDownloader != "" {
		args = append(args, jsoncmd.ExternalDLer, v.Settings.ExternalDownloader)

		if v.Settings.ExternalDownloaderArgs != "" {
			args = append(args, jsoncmd.ExternalDLArgs, v.Settings.ExternalDownloaderArgs)
		}
	}

	if v.Settings.Retries != 0 {
		args = append(args, jsoncmd.Retries, strconv.Itoa(v.Settings.Retries))
	}

	args = append(args, jsoncmd.RestrictFilenames, jsoncmd.Output, jsoncmd.FilenameSyntax,
		v.URL)

	cmd := exec.CommandContext(context.Background(), jsoncmd.YTDLP, args...)
	logging.D(1, "Built metadata download command for URL %q:\n%v", v.URL, cmd.String())

	return cmd
}
