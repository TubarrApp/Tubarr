package downloads

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/keys"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/spf13/viper"
)

// buildJSONCommand builds and returns the argument for downloading metadata files for the given URL.
func (d *JSONDownload) buildJSONCommand() *exec.Cmd {
	args := make([]string, 0, 32)

	// Restrict filenames
	args = append(args, command.RestrictFilenames)

	// Download JSON to directory
	args = append(args,
		command.SkipVideo,
		command.WriteInfoJSON,
		command.P, d.Video.ParsedJSONDir,
		command.Output, command.FilenameSyntax)

	// Cookie path
	if d.ChannelURL.CookiePath == "" {
		if d.ChannelURL.ChanURLSettings.CookieSource != "" {
			args = append(args, command.CookiesFromBrowser, d.ChannelURL.ChanURLSettings.CookieSource)
		}
	} else {
		args = append(args, command.CookiePath, d.ChannelURL.CookiePath)
	}

	// Cookie source
	if viper.IsSet(keys.CookieSource) {
		browserCookieSource := viper.GetString(keys.CookieSource)
		logging.I("Using cookies from browser %q", browserCookieSource)
		args = append(args, command.CookiesFromBrowser, browserCookieSource)
	} else {
		logging.D(1, "No browser cookies set for channel %q and URL %q, skipping cookies in JSON download", d.Channel.Name, d.Video.URL)
	}

	// Max filesize specified
	if d.ChannelURL.ChanURLSettings.MaxFilesize != "" {
		args = append(args, command.MaxFilesize, d.ChannelURL.ChanURLSettings.MaxFilesize)
	}

	// External downloaders & arguments
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

	// Retry download X times
	if d.ChannelURL.ChanURLSettings.Retries != 0 {
		args = append(args, command.Retries, strconv.Itoa(d.ChannelURL.ChanURLSettings.Retries))
	}

	// Randomize requests (avoid detection as bot)
	args = append(args, command.RandomizeRequests...)

	// Add target URL [ MUST GO LAST !! ]
	args = append(args, d.Video.URL)

	// Build command with context
	cmd := exec.CommandContext(d.Context, command.YTDLP, args...)
	return cmd
}

// executeJSONDownload executes a JSON download command.
func (d *JSONDownload) executeJSONDownload(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("no command built for URL %s", d.Video.URL)
	}

	// Ensure the directory exists
	if _, err := validation.ValidateDirectory(d.ChannelURL.ChanURLSettings.VideoDir, true); err != nil {
		return err
	}

	// Execute JSON download
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logging.I("Running metadata download command for URL %q:\n\n%v\n", d.Video.URL, cmd.String())
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("yt-dlp error for %s: %w\nStderr: %s", d.Video.URL, err, stderr.String())
	}

	// Find the line containing the JSON path
	outputLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(outputLines) == 0 {
		logging.D(1, "Full stdout: %s", stdout.String())
		logging.D(1, "Full stderr: %s", stderr.String())
		return fmt.Errorf("no output lines found for %s", d.Video.URL)
	}

	var jsonPath string

	fileLine := outputLines[len(outputLines)-1]
	logging.D(1, "File line captured as: %v", fileLine)

	_, jsonPath, found := strings.Cut(fileLine, ": ")
	if !found || jsonPath == "" {
		logging.D(1, "Full stdout: %s", stdout.String())
		logging.D(1, "Full stderr: %s", stderr.String())
		return fmt.Errorf("could not find JSON file path in output for %s", d.Video.URL)
	}

	d.Video.JSONPath = jsonPath

	// Verify the file exists
	if err := verifyJSONDownload(d.Video.JSONPath); err != nil {
		return err
	}

	logging.I("Successfully saved JSON file to %q", d.Video.JSONPath)
	return nil
}
