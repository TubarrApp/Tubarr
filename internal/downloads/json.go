package downloads

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/siterules"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// buildJSONCommand builds and returns the argument for downloading metadata files for the given URL.
func (d *JSONDownload) buildJSONCommand() (*exec.Cmd, error) {
	args := make([]string, 0, 32)

	// Restrict filenames.
	args = append(args, command.RestrictFilenames)

	// Download JSON to directory.
	args = append(args,
		command.SkipVideo,
		command.WriteInfoJSON,
		command.P, d.Video.Files.JSONDir,
		command.Output, command.FilenameSyntax)

	// Cookie file and browser cookies.
	cookies := cookieArgs(d.ChannelURL)
	if len(cookies) == 0 {
		d.Channel.Log().D(1, "No cookies set for channel %q and URL %q, skipping cookies in JSON download", d.Channel.Name, d.Video.URL)
	}
	args = append(args, cookies...)

	// Max filesize specified.
	if d.ChannelURL.ChanURLSettings.MaxFilesize != "" {
		args = append(args, command.MaxFilesize, d.ChannelURL.ChanURLSettings.MaxFilesize)
	}

	// Max resolution specified, so the metadata describes the format that's downloaded.
	if d.ChannelURL.ChanURLSettings.MaxResolution != "" {
		args = append(args, command.FormatSort, command.FormatSortMaxRes+d.ChannelURL.ChanURLSettings.MaxResolution)
	}

	// Randomize requests (avoid detection as bot).
	args = append(args, command.SleepRequests...)

	// Additional user arguments, refused if one is a blocked option.
	extra, err := validation.ValidateExtraYtdlpArgs(d.ChannelURL.ChanURLSettings.ExtraYTDLPMetaArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid extra yt-dlp metadata args: %w", err)
	}
	args = append(args, extra...)

	// Site rule impersonation, and FlareSolverr's user agent (its cookies are in the cookie file).
	args, d.fsGen = siterules.AddSiteRulesToYTDLP(d.Video.URL, d.ChannelURL.Session.FlareSolverr, args)

	// Add target URL [ MUST GO LAST !! ].
	args = append(args, command.EndOfOptions, d.Video.URL)

	// Build command with context.
	//nolint:gosec // yt-dlp with the args Tubarr builds, the user's checked against the blocked options.
	cmd := exec.CommandContext(d.Context, sharedconsts.ProgramYTDLP, args...)
	useProcessGroup(cmd)
	return cmd, nil
}

// executeJSONDownload executes a JSON download command.
func (d *JSONDownload) executeJSONDownload(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("no command built for URL %s", d.Video.URL)
	}

	// Execute JSON download.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	d.Channel.Log().I("Running metadata download command for video %q (Channel: %q):\n\n%v\n", d.Video.URL, d.Channel.Name, cmd.String())
	err := cmd.Run()
	killProcessGroup(cmd)
	if err != nil {
		return fmt.Errorf("yt-dlp error for %s: %w\nStderr: %s", d.Video.URL, err, stderr.String())
	}

	// Find the line containing the JSON path.
	output := strings.TrimSpace(stdout.String())
	if output == "" {
		d.Channel.Log().D(3, "Full stdout: %s", stdout.String())
		d.Channel.Log().D(3, "Full stderr: %s", stderr.String())
		return fmt.Errorf("no output lines found for %s", d.Video.URL)
	}
	outputLines := strings.Split(output, "\n")

	var jsonPath string

	fileLine := outputLines[len(outputLines)-1]
	d.Channel.Log().D(1, "File line captured as: %v", fileLine)

	_, jsonPath, found := strings.Cut(fileLine, ": ")
	if !found || jsonPath == "" {
		d.Channel.Log().D(1, "Full stdout: %s", stdout.String())
		d.Channel.Log().D(1, "Full stderr: %s", stderr.String())
		return fmt.Errorf("could not find JSON file path in output for %s", d.Video.URL)
	}

	d.Video.Files.JSONFilePath = jsonPath

	// Verify the file exists.
	if err := verifyJSONDownload(d.Video.Files.JSONFilePath); err != nil {
		return err
	}

	d.Channel.Log().I("Successfully saved JSON file to %q", d.Video.Files.JSONFilePath)
	return nil
}
