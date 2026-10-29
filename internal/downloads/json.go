package downloads

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/logger"
	"tubarr/internal/siterules"

	"github.com/TubarrApp/gocommon/sharedparsing"
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
		command.P, d.Video.JSONDir,
		command.Output, command.FilenameSyntax)

	// Cookie file and browser cookies, which yt-dlp combines (the file's winning where both set one).
	if d.ChannelURL.CookiePath != "" {
		args = append(args, command.CookiePath, d.ChannelURL.CookiePath)
	}
	if d.ChannelURL.ChanURLSettings.CookiesFromBrowser != "" {
		args = append(args, command.CookiesFromBrowser, d.ChannelURL.ChanURLSettings.CookiesFromBrowser)
	}
	if d.ChannelURL.CookiePath == "" && d.ChannelURL.ChanURLSettings.CookiesFromBrowser == "" {
		logger.Pl.D(1, "No cookies set for channel %q and URL %q, skipping cookies in JSON download", d.Channel.Name, d.Video.URL)
	}

	// Max filesize specified.
	if d.ChannelURL.ChanURLSettings.MaxFilesize != "" {
		args = append(args, command.MaxFilesize, d.ChannelURL.ChanURLSettings.MaxFilesize)
	}

	// External downloaders and arguments.
	if d.ChannelURL.ChanURLSettings.ExternalDownloader != "" {
		args = append(args, command.ExternalDLer, d.ChannelURL.ChanURLSettings.ExternalDownloader)
		if d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs != "" {
			switch d.ChannelURL.ChanURLSettings.ExternalDownloader {
			case command.DownloaderAria:
				ariaCmd := command.DownloaderAria + ":" +
					d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs +
					" " +
					command.AriaLog +
					" " +
					command.AriaNoRPC +
					" " +
					command.AriaNoColor +
					" " +
					command.AriaShowConsole +
					" " +
					command.AriaInterval
				args = append(args, command.ExternalDLArgs, ariaCmd)
			default:
				args = append(args, command.ExternalDLArgs, d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs)
			}
		}
	}

	// Randomize requests (avoid detection as bot).
	args = append(args, command.SleepRequests...)

	// Additional user arguments.
	if extra := d.ChannelURL.ChanURLSettings.ExtraYTDLPMetaArgs; extra != "" {
		splitArgs, err := sharedparsing.ShellSplit(extra)
		if err != nil {
			return nil, err
		}
		args = append(args, splitArgs...)
	}

	// Site rule impersonation, and FlareSolverr's user agent (its cookies are in the cookie file).
	args, d.fsGen = siterules.AddSiteRulesToYTDLP(d.Video.URL, d.ChannelURL.FlareSolverr, args)

	// Add target URL [ MUST GO LAST !! ].
	args = append(args, command.EndOfOptions, d.Video.URL)

	// Build command with context.
	cmd := exec.CommandContext(d.Context, command.YTDLP, args...)
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

	logger.Pl.I("Running metadata download command for video %q (Channel: %q):\n\n%v\n", d.Video.URL, d.Channel.Name, cmd.String())
	err := cmd.Run()
	killProcessGroup(cmd)
	if err != nil {
		return fmt.Errorf("yt-dlp error for %s: %w\nStderr: %s", d.Video.URL, err, stderr.String())
	}

	// Find the line containing the JSON path.
	outputLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(outputLines) == 0 {
		logger.Pl.D(3, "Full stdout: %s", stdout.String())
		logger.Pl.D(3, "Full stderr: %s", stderr.String())
		return fmt.Errorf("no output lines found for %s", d.Video.URL)
	}

	var jsonPath string

	fileLine := outputLines[len(outputLines)-1]
	logger.Pl.D(1, "File line captured as: %v", fileLine)

	_, jsonPath, found := strings.Cut(fileLine, ": ")
	if !found || jsonPath == "" {
		logger.Pl.D(1, "Full stdout: %s", stdout.String())
		logger.Pl.D(1, "Full stderr: %s", stderr.String())
		return fmt.Errorf("could not find JSON file path in output for %s", d.Video.URL)
	}

	d.Video.JSONFilePath = jsonPath

	// Verify the file exists.
	if err := verifyJSONDownload(d.Video.JSONFilePath); err != nil {
		return err
	}

	logger.Pl.I("Successfully saved JSON file to %q", d.Video.JSONFilePath)
	return nil
}
