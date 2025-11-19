package cfg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/auth"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/state"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedvalidation"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// InitChannelCmds is the entrypoint for initializing channel commands.
func InitChannelCmds(ctx context.Context, s contracts.Store) *cobra.Command {
	channelCmd := &cobra.Command{
		Use:   "channel",
		Short: "Channel commands.",
		Long:  "Manage channels with various subcommands like add, delete, and list.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("please specify a subcommand. Use --help to see available subcommands")
		},
	}
	cs := s.ChannelStore()

	// Channel commands
	channelCmd.AddCommand(addAuth(cs))
	channelCmd.AddCommand(addChannelCmd(ctx, cs, s))
	channelCmd.AddCommand(addBatchChannelsCmd(ctx, cs, s))
	channelCmd.AddCommand(unblockChannelCmd(cs))
	channelCmd.AddCommand(downloadVideoURLs(ctx, cs, s))
	channelCmd.AddCommand(crawlChannelCmd(ctx, cs, s))
	channelCmd.AddCommand(ignoreCrawl(ctx, cs, s))
	channelCmd.AddCommand(addVideoURLToIgnore(cs))
	channelCmd.AddCommand(deleteChannelCmd(cs))
	channelCmd.AddCommand(deleteVideoURLs(cs))
	channelCmd.AddCommand(deleteNotifyURLs(cs))
	channelCmd.AddCommand(listChannelCmd(cs))
	channelCmd.AddCommand(listAllChannelsCmd(cs))
	channelCmd.AddCommand(pauseChannelCmd(cs))
	channelCmd.AddCommand(unpauseChannelCmd(cs))
	channelCmd.AddCommand(updateChannelValue(cs))
	channelCmd.AddCommand(updateChannelSettingsCmd(cs))
	channelCmd.AddCommand(addNotifyURLs(cs))

	// Channel URL commands
	channelCmd.AddCommand(updateChannelURLSettingsCmd(cs))

	return channelCmd
}

// addAuth adds authentication details to a channel.
func addAuth(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		channelID   int
		channelName string

		// Authentication details
		username    string
		password    string
		loginURL    string
		authDetails []string
	)

	addAuthCmd := &cobra.Command{
		Use:   "auth",
		Short: "Add authentication details to a channel.",
		Long:  "Add authentication details to a channel for use in crawls.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Valid items set?
			if len(authDetails) == 0 && (username == "" || loginURL == "") {
				return errors.New("must enter a username and login URL, or fully qualified authentication detail string ('channel URL|username|password|login URL')")
			}

			// Get and check key/val pair
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			// Get and check ID
			id, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel with %s %q", id, key, val)
			}

			// Fetch channel model (retrieve the URLs)
			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key/value %q:%q", key, val)
			}
			if err != nil {
				return err
			}

			// Parse and set authentication details
			cURLs := c.GetURLs()
			authDetails, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, cURLs, false)
			if err != nil {
				return err
			}

			if len(authDetails) > 0 {
				if err := cs.AddAuth(id, authDetails); err != nil {
					return err
				}
			}

			// Success
			logger.Pl.S("Channel with %s %q set authentication details", key, val)
			return nil
		},
	}
	cmd.SetPrimaryChannelFlags(addAuthCmd, &channelName, nil, &channelID)
	cmd.SetAuthFlags(addAuthCmd, &username, &password, &loginURL, &authDetails)
	return addAuthCmd
}

// deleteURLs deletes a list of URLs inputted by the user.
func deleteVideoURLs(cs contracts.ChannelStore) *cobra.Command {

	var (
		// Channel identifiers
		channelID   int
		channelName string

		// URLs and files
		cFile string
		urls  []string
	)

	deleteURLsCmd := &cobra.Command{
		Use:   "delete-videos",
		Short: "Remove video URLs from the database.",
		Long:  "If using a file, the file should contain one URL per line.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if cFile == "" && len(urls) == 0 {
				return errors.New("must enter a URL source")
			}

			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			chanID, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if err := cs.DeleteVideosByURLs(chanID, urls); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.SetPrimaryChannelFlags(deleteURLsCmd, &channelName, nil, &channelID)
	deleteURLsCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to delete from the database.")

	return deleteURLsCmd
}

// downloadVideoURLs downloads a list of URLs inputted by the user.
func downloadVideoURLs(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		// Channel identifiers
		channelID   int
		channelName string

		// URLs and files
		cFile string
		urls  []string
	)

	manualURLCmd := &cobra.Command{
		Use:           "download-video-urls",
		Short:         "Download inputted URLs (plaintext or file).",
		Long:          "If using a file, the file should contain one URL per line.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Valid items set?
			if cFile == "" && len(urls) == 0 {
				err := errors.New("must enter URLs into the source file, or set at least one URL directly")
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			var urlLines []string
			// Check URL file if existent
			if cFile != "" {
				cFileInfo, err := validation.ValidateFile(cFile, false)
				if err != nil {
					err = fmt.Errorf("file entered (%q) is not valid: %w", cFile, err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					return err
				}
				if cFile != "" && cFileInfo.Size() == 0 {
					err := fmt.Errorf("url file %q is blank", cFile)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					return err
				}
				if urlLines, err = file.ReadFileLines(cFile); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					return err
				}
			}

			// Combine with video URLs if any
			videoURLs := append(urls, urlLines...)

			// Check URLs have valid syntax
			for _, u := range videoURLs {
				splitLen := len(strings.Split(u, "|"))
				if splitLen > 2 {
					err := fmt.Errorf("url syntax entered incorrectly. Should be either just the URL or 'channel URL|video URL'")
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					return err
				}
			}

			// Get and check key/val pair
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			// Retrieve channel model
			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				err := fmt.Errorf("no channel model in database with %s %q", key, val)
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			cURLs := c.GetURLs()
			logger.Pl.D(1, "Retrieved channel %q with URLs: %v", c.Name, cURLs)

			// Download URLs - errors already logged, DON'T print here
			if err := app.DownloadVideosToChannel(ctx, s, cs, c, videoURLs); err != nil {
				return err
			}

			// Success
			logger.Pl.S("Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	cmd.SetPrimaryChannelFlags(manualURLCmd, &channelName, nil, &channelID)
	manualURLCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to your given channel")
	manualURLCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to download for a given channel")

	return manualURLCmd
}

// deleteNotifyURLs deletes notification URLs from a channel.
func deleteNotifyURLs(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		channelID   int
		channelName string

		// Notification details
		notifyURLs  []string
		notifyNames []string
	)

	deleteNotifyCmd := &cobra.Command{
		Use:   "notify-delete",
		Short: "Deletes a notify function from a channel.",
		Long:  "Enter a fully qualified notification URL here to delete from the database.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Valid items set?
			if len(notifyURLs) == 0 && len(notifyNames) == 0 {
				return errors.New("must enter at least one notify URL or name to delete")
			}

			// Get and check ID
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			id, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel name %q", id, channelName)
			}

			// Run command
			if err := cs.DeleteNotifyURLs(id, notifyURLs, notifyNames); err != nil {
				return err
			}

			// Success
			logger.Pl.S("Deleted notify URLs for channel with %s %q: %v", key, val, notifyURLs)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(deleteNotifyCmd, &channelName, nil, &channelID)
	deleteNotifyCmd.Flags().StringSliceVar(&notifyURLs, "notify-urls", nil, "Full notification URLs (e.g. 'http://YOUR_PLEX_SERVER_IP:32400/library/sections/LIBRARY_ID_NUMBER/refresh?X-Plex-Token=YOUR_PLEX_TOKEN_HERE')")
	deleteNotifyCmd.Flags().StringSliceVar(&notifyNames, "notify-names", nil, "Full notification names")

	return deleteNotifyCmd
}

// addNotifyURLs adds a notification URL (can use to send requests to update Plex libraries on new video addition).
func addNotifyURLs(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		channelID   int
		channelName string

		// Notification details
		notification []string
	)

	addNotifyCmd := &cobra.Command{
		Use:   "notify-add",
		Short: "Adds notify function to a channel.",
		Long:  "Enter fully qualified notification URLs here to send update requests to platforms like Plex etc. (notification pair format is 'URL|Friendly Name')",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Valid items set?
			if len(notification) == 0 {
				return errors.New("no notification URL|Name pairs entered")
			}

			// Get and check ID
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			id, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel name %q", id, channelName)
			}

			// Parse notification URLs
			validPairs, err := parsing.ParseNotifications(notification)
			if err != nil {
				return err
			}

			if err := cs.AddNotifyURLs(id, validPairs); err != nil {
				return err
			}

			// Success
			pairSlice := make([]string, 0, len(validPairs))
			for _, vp := range validPairs {
				pairSlice = append(pairSlice, (vp.ChannelURL + "|" + vp.NotifyURL + "|" + vp.Name))
			}
			logger.Pl.S("Added notify URLs for channel with %s %q: %v", key, val, pairSlice)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(addNotifyCmd, &channelName, nil, &channelID)
	cmd.SetNotifyFlags(addNotifyCmd, &notification)

	return addNotifyCmd
}

// addVideoURLToIgnore adds a user inputted URL to ignore from crawls.
func addVideoURLToIgnore(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string

		// Ignore details
		ignoreURL string
	)

	ignoreURLCmd := &cobra.Command{
		Use:   "ignore-video-url",
		Short: "Adds a video URL to ignore.",
		Long:  "URLs added to this list will not be grabbed from channel crawls.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Valid items set?
			if ignoreURL == "" {
				return errors.New("cannot enter the target ignore URL blank")
			}

			// Get and check ID
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			id, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel name %q", id, name)
			}

			// Add URLs to ignore
			if err := cs.AddURLToIgnore(id, ignoreURL); err != nil {
				return err
			}

			// Success
			logger.Pl.S("Ignoring URL %q for channel with %s %q", ignoreURL, key, val)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(ignoreURLCmd, &name, nil, nil)
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-video-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// ignoreCrawl crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func ignoreCrawl(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:          "ignore-crawl",
		Short:        "Crawl a channel for URLs to ignore.",
		Long:         "Crawls the current state of a channel page and adds all video URLs to ignore.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair.
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Get channel model.
			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				return fmt.Errorf("no rows in the database for channel with %s %q", key, val)
			}
			if err != nil {
				return err
			}

			// Fetch URL models.
			c.URLModels, err = cs.GetChannelURLModels(c, false)
			if err != nil {
				return fmt.Errorf("failed to fetch URL models for channel: %w", err)
			}

			// Log retrieved URLs.
			cURLs := c.GetURLs()
			logger.Pl.D(3, "Retrieved channel %q with URLs: %v", c.Name, cURLs)

			// Run ignore crawl.
			if !state.CrawlStateActive(c.Name) {
				state.LockCrawlState(c.Name)
				defer state.UnlockCrawlState(c.Name)

				if err := app.CrawlChannelIgnore(ctx, s, c); err != nil {
					return err
				}
			} else {
				logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
			}

			// Success
			logger.Pl.S("Completed ignore crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(ignoreCrawlCmd, &name, nil, nil)

	return ignoreCrawlCmd
}

// pauseChannelCmd pauses a channel from downloads in upcoming crawls.
func pauseChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string
	)

	pauseCmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause a channel.",
		Long:  "Paused channels won't download new videos when the main program runs a crawl.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Get channel model
			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key/value %q:%q", key, val)
			}
			if err != nil {
				return err
			}

			// Alter and save model settings
			c.ChanSettings.Paused = true

			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.Settings) error {
				s.Paused = c.ChanSettings.Paused
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to unpause channel: %w", err)
			}

			// Success
			logger.Pl.S("Paused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(pauseCmd, &name, nil, &id)

	return pauseCmd
}

// unpauseChannelCmd unpauses a channel to allow for downloads in upcoming crawls.
func unpauseChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string
	)

	unPauseCmd := &cobra.Command{
		Use:   "unpause",
		Short: "Unpause a channel.",
		Long:  "Unpauses a channel, allowing it to download new videos when the main program runs a crawl.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Get channel model
			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key %q and value %q", key, val)
			}
			if err != nil {
				return err
			}

			// Send update
			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.Settings) error {
				s.Paused = false
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to unpause channel: %w", err)
			}

			// Success
			logger.Pl.S("Unpaused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(unPauseCmd, &name, nil, &id)

	return unPauseCmd
}

// unblockChannelCmd unblocks channels which were locked (usually due to bot activity detection).
func unblockChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string
	)

	unblockCmd := &cobra.Command{
		Use:   "unblock",
		Short: "Unblock a channel.",
		Long:  "Unblocks a channel (usually blocked due to sites detecting Tubarr as a bot), allowing it to download new videos.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Get channel model
			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key %q and value %q", key, val)
			}
			if err != nil {
				return err
			}

			// Send update
			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.Settings) error {
				s.BotBlocked = false
				s.BotBlockedHostnames = nil
				s.BotBlockedTimestamps = make(map[string]time.Time)
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to unblock channel: %w", err)
			}

			// Success
			logger.Pl.S("Unblocked channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(unblockCmd, &name, nil, &id)

	return unblockCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		input       models.ChannelInputPtrs
		flags       models.ChannelFlagValues
		addFromFile string
		configFile  string
		fileToUse   string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, settings, etc.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if addFromFile != "" {
				fileToUse = addFromFile
			}
			if configFile != "" {
				fileToUse = configFile
			}
			if fileToUse == "" {
				mergeFlagsIntoInput(cmd, &flags, &input)
				return nil
			}

			v := viper.New()
			if err := file.LoadConfigFile(v, fileToUse); err != nil {
				return err
			}

			if err := parsing.LoadViperIntoStruct(v, &input); err != nil {
				return err
			}

			urlSettings, err := parsing.ParseURLSettingsFromViper(v)
			if err != nil {
				return err
			}
			input.URLSettings = urlSettings

			mergeFlagsIntoInput(cmd, &flags, &input)
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			c, authMap, err := parsing.BuildChannelFromInput(input)
			if err != nil {
				return err
			}

			channelID, err := cs.AddChannel(c)
			if err != nil {
				return err
			}
			c.ID = channelID

			if len(authMap) > 0 {
				if err := cs.AddAuth(channelID, authMap); err != nil {
					return err
				}
			}

			if input.Notification != nil && len(*input.Notification) != 0 {
				validPairs, err := parsing.ParseNotifications(*input.Notification)
				if err != nil {
					return err
				}

				if err := cs.AddNotifyURLs(channelID, validPairs); err != nil {
					return err
				}
			}

			// Ignore run if desired.
			if input.IgnoreRun != nil && *input.IgnoreRun {
				if !state.CrawlStateActive(c.Name) {
					state.LockCrawlState(c.Name)
					defer state.UnlockCrawlState(c.Name)

					logger.Pl.I("Running an 'ignore crawl'...")

					if err := app.CrawlChannelIgnore(ctx, s, c); err != nil {
						logger.Pl.E("Failed to complete ignore crawl run: %v", err)
					}
				} else {
					logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
				}
			}

			logger.Pl.S("Completed addition of channel %q to Tubarr", c.Name)
			return nil
		},
	}

	cmd.SetPrimaryChannelFlags(addCmd, &flags.Name, &flags.URLs, nil)

	cmd.SetFileDirFlags(addCmd, &configFile, &flags.JSONDir, &flags.VideoDir)

	cmd.SetProgramRelatedFlags(addCmd, &flags.Concurrency, &flags.CrawlFreq,
		&flags.ExternalDownloaderArgs, &flags.ExternalDownloader,
		&flags.MoveOpFile, &flags.MoveOps, &flags.Pause)

	cmd.SetDownloadFlags(addCmd, &flags.Retries, &flags.UseGlobalCookies,
		&flags.YTDLPOutputExt, &flags.FromDate, &flags.ToDate,
		&flags.CookiesFromBrowser, &flags.MaxFilesize, &flags.DLFilterFile,
		&flags.DLFilters)

	cmd.SetMetarrFlags(addCmd, &flags.MaxCPU, &flags.MetarrConcurrency,
		&flags.MetarrExt, &flags.ExtraFFmpegArgs, &flags.MinFreeMem,
		&flags.OutDir, &flags.RenameStyle, &flags.MetaOpsFile,
		&flags.FilteredMetaOpsFile, &flags.FilenameOpsFile, &flags.FilteredFilenameOpsFile,
		&flags.URLOutputDirs, &flags.FilenameOps, &flags.FilteredFilenameOps,
		&flags.MetaOps, &flags.FilteredMetaOps)

	cmd.SetAuthFlags(addCmd, &flags.Username, &flags.Password, &flags.LoginURL, &flags.AuthDetails)

	cmd.SetTranscodeFlags(addCmd, &flags.TranscodeGPU, &flags.TranscodeGPUDirectory,
		&flags.TranscodeVideoFilter, &flags.TranscodeQuality, &flags.TranscodeVideoCodec,
		&flags.TranscodeAudioCodec)

	cmd.SetCustomYDLPArgFlags(addCmd, &flags.ExtraYTDLPVideoArgs, &flags.ExtraYTDLPMetaArgs)

	cmd.SetNotifyFlags(addCmd, &flags.Notification)

	addCmd.Flags().BoolVar(&flags.Pause, "pause", false, "Paused channels won't crawl videos on a normal program run")
	addCmd.Flags().BoolVar(&flags.IgnoreRun, keys.IgnoreRun, false, "Run an 'ignore crawl' first so only new videos are downloaded (rather than the entire channel backlog)")
	addCmd.Flags().StringVar(&addFromFile, "add-channel-from-file", "", "Add a channel using a prewritten file (.toml, .yaml, etc.).\nFile contents example:\n\nchannel-name: 'Cool Channel'\nchannel-urls:\n  - 'https://www.coolchannel.com/'\n")

	return addCmd
}

// addBatchChannelsCmd adds multiple channels from config files in a directory.
func addBatchChannelsCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		configDirectory string
		input           models.ChannelInputPtrs
		flags           models.ChannelFlagValues
	)

	batchCmd := &cobra.Command{
		Use:   "add-batch",
		Short: "Add multiple channels from a directory.",
		Long:  "Add multiple channels by reading all Viper-compatible config files (.yaml, .yml, .toml, .json, etc.) from a directory.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configDirectory == "" {
				return errors.New("must specify a directory path containing config files")
			}

			batchConfigFiles, err := file.ScanDirectoryForConfigFiles(configDirectory)
			if err != nil {
				return fmt.Errorf("failed to scan directory: %w", err)
			}

			if len(batchConfigFiles) == 0 {
				logger.Pl.I("No config files found in directory %q", configDirectory)
				return nil
			}

			logger.Pl.I("Found %d config file(s) in directory %q", len(batchConfigFiles), configDirectory)

			var successes []string
			var failures []struct {
				file string
				err  error
			}

			for i, batchConfigFile := range batchConfigFiles {
				logger.Pl.I("Processing config file: %s", batchConfigFile)

				if i > 0 {
					input = models.ChannelInputPtrs{}
					flags = models.ChannelFlagValues{}
				}

				cmd.Flags().VisitAll(func(f *pflag.Flag) {
					f.Changed = false
				})

				v := viper.New()
				if err := file.LoadConfigFile(v, batchConfigFile); err != nil {
					return err
				}

				if err := resetCobraFlagsAndLoadViper(cmd, v); err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("failed to apply config: %w", err)})
					continue
				}

				mergeFlagsIntoInput(cmd, &flags, &input)

				urlSettings, err := parsing.ParseURLSettingsFromViper(v)
				if err == nil {
					input.URLSettings = urlSettings
				}

				c, authMap, err := parsing.BuildChannelFromInput(input)
				if err != nil || c == nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, err})
					continue
				}

				channelID, err := cs.AddChannel(c)
				if err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("failed to add channel to database: %w", err)})
					continue
				}

				c.ID = channelID

				if len(authMap) > 0 {
					if err := cs.AddAuth(channelID, authMap); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("failed to add auth: %w", err)})
						continue
					}
				}

				if input.Notification != nil && len(*input.Notification) != 0 {
					validPairs, err := parsing.ParseNotifications(*input.Notification)
					if err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid notification URLs: %w", err)})
						continue
					}

					if err := cs.AddNotifyURLs(channelID, validPairs); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("failed to add notification URLs: %w", err)})
						continue
					}
				}

				// Ignore run if desired.
				if input.IgnoreRun != nil && *input.IgnoreRun {
					if !state.CrawlStateActive(c.Name) {
						state.LockCrawlState(c.Name)
						defer state.UnlockCrawlState(c.Name)

						logger.Pl.I("Running an 'ignore crawl' for channel %q...", c.Name)
						if err := app.CrawlChannelIgnore(ctx, s, c); err != nil {
							logger.Pl.E("Failed to complete ignore crawl run for %q: %v", c.Name, err)
						}
					} else {
						logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
					}
				}

				successes = append(successes, batchConfigFile)
				logger.Pl.S("Successfully added channel %q from config file: %s", c.Name, batchConfigFile)
			}

			fmt.Println()
			logger.Pl.P("====== Batch Add Summary ======\n")
			if len(successes) > 0 {
				logger.Pl.S("Successfully added %d channel(s)", len(successes))
			}
			if len(failures) > 0 {
				logger.Pl.E("Failed to add %d channel(s):", len(failures))
				for _, f := range failures {
					logger.Pl.E("  - %s: %v", f.file, f.err)
				}
			}

			return nil
		},
	}

	batchCmd.Flags().StringVar(&configDirectory, "add-from-directory", "", "Directory containing channel config files (.yaml, .yml, .toml, .json, etc.)")

	cmd.SetPrimaryChannelFlags(batchCmd, &flags.Name, &flags.URLs, nil)
	cmd.SetFileDirFlags(batchCmd, &flags.ChannelConfigFile, &flags.JSONDir, &flags.VideoDir)

	cmd.SetProgramRelatedFlags(batchCmd, &flags.Concurrency, &flags.CrawlFreq, &flags.ExternalDownloaderArgs,
		&flags.ExternalDownloader, &flags.MoveOpFile, &flags.MoveOps, &flags.Pause)

	cmd.SetDownloadFlags(batchCmd, &flags.Retries, &flags.UseGlobalCookies, &flags.YTDLPOutputExt,
		&flags.FromDate, &flags.ToDate, &flags.CookiesFromBrowser, &flags.MaxFilesize,
		&flags.DLFilterFile, &flags.DLFilters)

	cmd.SetMetarrFlags(batchCmd, &flags.MaxCPU, &flags.MetarrConcurrency, &flags.MetarrExt,
		&flags.ExtraFFmpegArgs, &flags.MinFreeMem, &flags.OutDir, &flags.RenameStyle,
		&flags.MetaOpsFile, &flags.FilteredMetaOpsFile, &flags.FilenameOpsFile, &flags.FilteredFilenameOpsFile,
		&flags.URLOutputDirs, &flags.FilenameOps, &flags.FilteredFilenameOps, &flags.MetaOps, &flags.FilteredMetaOps)

	cmd.SetAuthFlags(batchCmd, &flags.Username, &flags.Password, &flags.LoginURL, &flags.AuthDetails)

	cmd.SetTranscodeFlags(batchCmd, &flags.TranscodeGPU, &flags.TranscodeGPUDirectory, &flags.TranscodeVideoFilter,
		&flags.TranscodeQuality, &flags.TranscodeVideoCodec, &flags.TranscodeAudioCodec)

	cmd.SetCustomYDLPArgFlags(batchCmd, &flags.ExtraYTDLPVideoArgs, &flags.ExtraYTDLPMetaArgs)
	cmd.SetNotifyFlags(batchCmd, &flags.Notification)

	batchCmd.Flags().BoolVar(&flags.Pause, "pause", false, "Paused channels won't crawl videos on a normal program run")
	batchCmd.Flags().BoolVar(&flags.IgnoreRun, keys.IgnoreRun, false, "Run an 'ignore crawl' for each channel so only new videos are downloaded")

	return batchCmd
}

// deleteChannelCmd deletes a channel from the database.
func deleteChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string

		// URLs
		urls []string
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete channels.",
		Long:  "Delete a channel by ID, name, or URL.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Delete channel
			if err := cs.DeleteChannel(key, val); err != nil {
				return err
			}

			// Success
			logger.Pl.S("Successfully deleted channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(delCmd, &name, &urls, &id)

	return delCmd
}

// listAllChannel returns details about a single channel in the database.
func listChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		channelID int
		name      string
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List a channel's details.",
		Long:  "Lists details of a channel in the database.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(channelID, name)
			if err != nil {
				return err
			}

			// Fetch channel model
			c, hasRows, err := cs.GetChannelModel(key, val, false)
			if !hasRows {
				logger.Pl.I("Entry for channel with %s %q does not exist in the database", key, val)
				return nil
			}
			if err != nil {
				return err
			}

			// Display settings and return
			cs.DisplaySettings(c)
			return nil
		},
	}
	// Primary channel elements
	cmd.SetPrimaryChannelFlags(listCmd, &name, nil, &channelID)
	return listCmd
}

// listAllChannelsCmd returns a list of channels in the database.
func listAllChannelsCmd(cs contracts.ChannelStore) *cobra.Command {
	listAllCmd := &cobra.Command{
		Use:   "list-all",
		Short: "List all channels.",
		Long:  "Lists all channels currently saved in the database.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Fetch channels from database
			chans, hasRows, err := cs.GetAllChannels(false)
			if !hasRows {
				logger.Pl.I("No entries in the database")
				return nil
			}
			if err != nil {
				return err
			}

			// Display settings and return
			for _, ch := range chans {
				cs.DisplaySettings(ch)
			}
			return nil
		},
	}
	return listAllCmd
}

// crawlChannelCmd initiates a crawl of a given channel.
func crawlChannelCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string
	)

	crawlCmd := &cobra.Command{
		Use:           "crawl",
		Short:         "Crawl a channel for new URLs.",
		Long:          "Initiate a crawl for new URLs of a channel that have not yet been downloaded.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				// Print flag/validation errors since they're user-facing
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			// Retrieve channel model
			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				err := fmt.Errorf("no channel model in database with %s %q", key, val)
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			// Crawl channel.
			if !state.CrawlStateActive(c.Name) {
				state.LockCrawlState(c.Name)
				defer state.UnlockCrawlState(c.Name)
				if err := app.CrawlChannel(ctx, s, c); err != nil {
					return err
				}
			} else {
				logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
			}

			// Success
			logger.Pl.S("Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(crawlCmd, &name, nil, &id)
	return crawlCmd
}

// updateChannelSettingsCmd updates channel settings.
func updateChannelSettingsCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		id      int
		name    string
		newName string

		// URLs
		urls       []string
		urlOutDirs []string

		// Directory paths
		vDir   string
		jDir   string
		outDir string
		gpuDir string

		// Configuration files
		configFile              string
		dlFilterFile            string
		moveOpsFile             string
		metaOpsFile             string
		filteredMetaOpsFile     string
		filenameOpsFile         string
		filteredFilenameOpsFile string

		// Authentication details
		username    string
		password    string
		loginURL    string
		authDetails []string

		// Download settings
		cookiesFromBrowser     string
		externalDownloader     string
		externalDownloaderArgs string
		maxFilesize            string
		ytdlpOutExt            string
		fromDate               string
		toDate                 string
		useGlobalCookies       bool

		// Filter and operation settings
		dlFilters           []string
		moveOps             []string
		metaOps             []string
		filenameOps         []string
		filteredMetaOps     []string
		filteredFilenameOps []string

		// Metarr settings
		metarrExt   string
		renameStyle string
		minFreeMem  string

		// Transcoding settings
		useGPU               string
		transcodeQuality     string
		transcodeVideoFilter string
		videoCodec           []string
		audioCodec           []string

		// Extra arguments
		extraYTDLPVideoArgs string
		extraYTDLPMetaArgs  string
		extraFFmpegArgs     string

		// Concurrency and performance settings
		concurrency       int
		crawlFreq         int
		metarrConcurrency int
		retries           int
		maxCPU            float64

		// Boolean flags
		pause      bool
		deleteAuth bool
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-channel",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			// Load in file if specified
			v := viper.New()
			if configFile != "" {
				if err := file.LoadConfigFile(v, configFile); err != nil {
					return err
				}
			}

			return resetCobraFlagsAndLoadViper(cmd, v)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Fetch model from database
			c, hasRows, err := cs.GetChannelModel(key, val, false)
			if !hasRows {
				return fmt.Errorf("no channel model in database with %s %q", key, val)
			}
			if err != nil {
				return err
			}

			// Load config file location if existent and one wasn't hardcoded into terminal
			//
			// Do not load again if hardcoded configFile is the same as the channel model's config file.
			if (!cmd.Flags().Changed(keys.ChannelConfigFile) || configFile == "") && c.ChannelConfigFile != "" && c.ChannelConfigFile != configFile {
				fileToUse := ""
				if configFile != "" {
					fileToUse = configFile
				}
				if c.ChannelConfigFile != "" {
					fileToUse = c.ChannelConfigFile
				}

				v := viper.New()
				if fileToUse != "" {
					if err := file.LoadConfigFile(v, fileToUse); err != nil {
						return err
					}
				}
				if err := resetCobraFlagsAndLoadViper(cmd, v); err != nil {
					return err
				}
			}

			// Change name
			if newName != "" && newName != c.Name {
				if err := cs.UpdateChannelValue(key, val, consts.QChanName, newName); err != nil {
					return err
				}
			}

			// Check if the user got auth details from flags
			gotAuthDetails := cmd.Flags().Changed(keys.AuthUsername) ||
				cmd.Flags().Changed(keys.AuthPassword) ||
				cmd.Flags().Changed(keys.AuthURL) ||
				cmd.Flags().Changed(keys.AuthDetails)

			// Parse and set authentication details if set by user, clear all if flag is set
			if gotAuthDetails || deleteAuth {
				// Get and check ID
				id, err := cs.GetChannelID(key, val)
				if err != nil {
					return err
				}

				if id == 0 {
					return fmt.Errorf("could not get valid ID (got %d) for channel with %s %q", id, key, val)
				}

				cURLs := c.GetURLs()
				authDetails, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, cURLs, deleteAuth)
				if err != nil {
					return err
				}

				if len(authDetails) > 0 {
					if err := cs.AddAuth(id, authDetails); err != nil {
						return err
					}
				}
			}

			// Gather channel settings
			fnSettingsArgs, err := getSettingsArgFns(cmd, chanSettings{
				concurrency:            concurrency,
				cookiesFromBrowser:     cookiesFromBrowser,
				crawlFreq:              crawlFreq,
				externalDownloader:     externalDownloader,
				externalDownloaderArgs: externalDownloaderArgs,
				filters:                dlFilters,
				filterFile:             dlFilterFile,
				fromDate:               fromDate,
				maxFilesize:            maxFilesize,
				metaFilterMoveOps:      moveOps,
				metaFilterMoveOpsFile:  moveOpsFile,
				paused:                 pause,
				retries:                retries,
				toDate:                 toDate,
				videoDir:               vDir,
				useGlobalCookies:       useGlobalCookies,
				ytdlpOutputExt:         ytdlpOutExt,
				extraYtdlpVideoArgs:    extraYTDLPVideoArgs,
				extraYtdlpMetaArgs:     extraYTDLPMetaArgs,
			})
			if err != nil {
				return err
			}

			if len(fnSettingsArgs) > 0 {
				finalUpdateFn := func(s *models.Settings) error {
					for _, fn := range fnSettingsArgs {
						if err := fn(s); err != nil {
							return err
						}
					}
					return nil
				}

				// Save channel settings to database
				if _, err := cs.UpdateChannelSettingsJSON(key, val, finalUpdateFn); err != nil {
					return err
				}
			}

			// Metarr arguments
			fnMetarrArray, err := getMetarrArgFns(cmd, cobraMetarrArgs{
				renameStyle:     renameStyle,
				extraFFmpegArgs: extraFFmpegArgs,
				metarrExt:       metarrExt,

				metaOps:             metaOps,
				metaOpsFile:         metaOpsFile,
				filteredMetaOps:     filteredMetaOps,
				filteredMetaOpsFile: filteredMetaOpsFile,

				filenameOps:             filenameOps,
				filenameOpsFile:         filenameOpsFile,
				filteredFilenameOps:     filteredFilenameOps,
				filteredFilenameOpsFile: filteredFilenameOpsFile,

				outputDir:            outDir,
				urlOutputDirs:        urlOutDirs,
				concurrency:          metarrConcurrency,
				maxCPU:               maxCPU,
				minFreeMem:           minFreeMem,
				useGPU:               useGPU,
				gpuDir:               gpuDir,
				transcodeVideoCodec:  videoCodec,
				transcodeAudioCodec:  audioCodec,
				transcodeQuality:     transcodeQuality,
				transcodeVideoFilter: transcodeVideoFilter,
			})
			if err != nil {
				return err
			}

			if len(fnMetarrArray) > 0 {
				finalUpdateFn := func(s *models.MetarrArgs) error {
					for _, fn := range fnMetarrArray {
						if err := fn(s); err != nil {
							return err
						}
					}
					return nil
				}

				// Save Metarr arguments to database
				if _, err := cs.UpdateChannelMetarrArgsJSON(key, val, finalUpdateFn); err != nil {
					return err
				}
			}

			// Success
			logger.Pl.S("Completed update for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(updateSettingsCmd, &name, &urls, &id)

	// Files/dirs
	cmd.SetFileDirFlags(updateSettingsCmd, &configFile, &jDir, &vDir)

	// Program related
	cmd.SetProgramRelatedFlags(updateSettingsCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader, &moveOpsFile,
		&moveOps, &pause)

	// Download
	cmd.SetDownloadFlags(updateSettingsCmd, &retries, &useGlobalCookies,
		&ytdlpOutExt, &fromDate, &toDate,
		&cookiesFromBrowser, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	cmd.SetMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &minFreeMem,
		&outDir, &renameStyle, &metaOpsFile,
		&filteredMetaOpsFile, &filenameOpsFile, &filteredFilenameOpsFile,
		&urlOutDirs, &filenameOps, &filteredFilenameOps,
		&metaOps, &filteredMetaOps)

	// Transcoding
	cmd.SetTranscodeFlags(updateSettingsCmd, &useGPU, &gpuDir,
		&transcodeVideoFilter, &transcodeQuality, &videoCodec,
		&audioCodec)

	// Auth
	cmd.SetAuthFlags(updateSettingsCmd, &username, &password,
		&loginURL, &authDetails)

	// Additional YTDLP args
	// YTDLPFlags
	cmd.SetCustomYDLPArgFlags(updateSettingsCmd, &extraYTDLPVideoArgs, &extraYTDLPMetaArgs)

	updateSettingsCmd.Flags().StringVar(&newName, "change-name-to", "", "Change the channel name to this value")
	updateSettingsCmd.Flags().BoolVar(&deleteAuth, "delete-auth", false, "Clear all authentication details for this channel and its URLs")

	return updateSettingsCmd
}

// updateChannelValue provides a command allowing the alteration of a channel row.
func updateChannelValue(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers
		id   int
		name string

		// Update details
		col    string
		newVal string
	)

	updateRowCmd := &cobra.Command{
		Use:   "update-value",
		Short: "Update a channel column value.",
		Long:  "Enter a column to update and a value to update that column to.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Check validity
			if err := verifyChanRowUpdateValid(col, newVal); err != nil {
				return err
			}

			// Update value in database
			if err := cs.UpdateChannelValue(key, val, col, newVal); err != nil {
				return err
			}

			// Success
			logger.Pl.S("Updated channel column: %q → %q", col, newVal)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(updateRowCmd, &name, nil, &id)

	updateRowCmd.Flags().StringVar(&col, "column-name", "", "The name of the column in the table (e.g. video_directory)")
	updateRowCmd.Flags().StringVar(&newVal, "set-value", "", "The value to set in the column (e.g. /my-directory)")
	return updateRowCmd
}

// ******************************** Private ********************************

type cobraMetarrArgs struct {
	renameStyle     string
	extraFFmpegArgs string
	metarrExt       string

	metaOps             []string
	metaOpsFile         string
	filteredMetaOps     []string
	filteredMetaOpsFile string

	filenameOps             []string
	filenameOpsFile         string
	filteredFilenameOps     []string
	filteredFilenameOpsFile string

	outputDir            string
	urlOutputDirs        []string
	concurrency          int
	maxCPU               float64
	minFreeMem           string
	useGPU               string
	gpuDir               string
	transcodeVideoCodec  []string
	transcodeAudioCodec  []string
	transcodeQuality     string
	transcodeVideoFilter string
}

// getMetarrArgFns gets and collects the Metarr argument functions for channel updates.
func getMetarrArgFns(cmd *cobra.Command, c cobraMetarrArgs) (fns []func(*models.MetarrArgs) error, err error) {
	f := cmd.Flags()

	// Min free memory
	if f.Changed(keys.MMinFreeMem) {
		if c.minFreeMem != "" {
			if _, err := sharedvalidation.ValidateMinFreeMem(c.minFreeMem); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MinFreeMem = c.minFreeMem
			return nil
		})
	}

	// Max CPU usage
	if f.Changed(keys.MMaxCPU) {
		c.maxCPU = sharedvalidation.ValidateMaxCPU(c.maxCPU, true)
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MaxCPU = c.maxCPU
			return nil
		})
	}

	// Metarr final video output extension (e.g. 'mp4')
	if f.Changed(keys.MOutputExt) {
		if c.metarrExt != "" {
			_, err := validation.ValidateMetarrOutputExt(c.metarrExt)
			if err != nil {
				return nil, err
			}
		}
		c.metarrExt = strings.ToLower(c.metarrExt)
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.OutputExt = c.metarrExt
			return nil
		})
	}

	// Rename style (e.g. 'spaces')
	if f.Changed(keys.MRenameStyle) {
		if c.renameStyle != "" {
			if err := validation.ValidateRenameFlag(c.renameStyle); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.RenameStyle = c.renameStyle
			return nil
		})
	}

	// Extra FFmpeg arguments
	if f.Changed(keys.MExtraFFmpegArgs) {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.ExtraFFmpegArgs = c.extraFFmpegArgs
			return nil
		})
	}

	// Filename operations (e.g. 'prefix:[DOG COLLECTION] ')
	if f.Changed(keys.MFilenameOps) {
		parsed := []models.FilenameOps{}

		if len(c.filenameOps) > 0 {
			parsed, err = parsing.ParseFilenameOps(c.filenameOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameOps = parsed
			return nil
		})
	}

	if f.Changed(keys.MFilenameOpsFile) {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameOpsFile = c.filenameOpsFile
			return nil
		})
	}

	// Filtered filename operations
	if f.Changed(keys.MFilteredFilenameOps) {
		parsed := []models.FilteredFilenameOps{}

		if len(c.filteredFilenameOps) > 0 {
			parsed, err = parsing.ParseFilteredFilenameOps(c.filteredFilenameOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilteredFilenameOps = parsed
			return nil
		})
	}

	if f.Changed(keys.MFilteredFilenameOpsFile) {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilteredFilenameOpsFile = c.filteredFilenameOpsFile
			return nil
		})
	}

	// Output directory
	if f.Changed(keys.MOutputDir) {
		if c.outputDir != "" {
			if _, err = validation.ValidateDirectory(c.outputDir, false); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.OutputDir = c.outputDir
			return nil
		})
	}

	// Output directory strings
	if f.Changed(keys.MURLOutputDirs) {
		validOutDirs := make([]string, 0, len(c.urlOutputDirs))
		if len(c.urlOutputDirs) != 0 {
			for _, u := range c.urlOutputDirs {
				if _, err = validation.ValidateDirectory(u, false); err != nil {
					return nil, err
				}
				validOutDirs = append(validOutDirs, u)
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.URLOutputDirs = validOutDirs
			return nil
		})
	}

	// Meta operations (e.g. 'all-credits:set:author')
	if f.Changed(keys.MMetaOps) {
		parsed := []models.MetaOps{}

		if len(c.metaOps) > 0 {
			parsed, err = parsing.ParseMetaOps(c.metaOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOps = parsed
			return nil
		})
	}

	if f.Changed(keys.MMetaOpsFile) {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOpsFile = c.metaOpsFile
			return nil
		})
	}

	// Filename meta operations (e.g. 'prefix:[COOL CAT VIDEOS]')
	if f.Changed(keys.MFilenameOps) {
		parsed := []models.FilenameOps{}

		if len(c.filenameOps) > 0 {
			parsed, err = parsing.ParseFilenameOps(c.filenameOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameOps = parsed
			return nil
		})
	}

	if f.Changed(keys.MFilenameOpsFile) {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameOpsFile = c.filenameOpsFile
			return nil
		})
	}

	// Filtered meta operations (e.g. 'title:contains:dog|title:prefix:[DOG VIDEOS]')
	if f.Changed(keys.MFilteredMetaOps) {
		parsed := []models.FilteredMetaOps{}

		if len(c.filteredMetaOps) > 0 {
			parsed, err = parsing.ParseFilteredMetaOps(c.filteredMetaOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilteredMetaOps = parsed
			return nil
		})
	}

	if f.Changed(keys.MFilteredMetaOpsFile) {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilteredMetaOpsFile = c.filteredMetaOpsFile
			return nil
		})
	}

	// Use GPU for transcoding
	if f.Changed(keys.TranscodeGPU) {
		validGPU := c.useGPU

		if c.useGPU != "" {
			validGPU, _, err = validation.ValidateGPU(c.useGPU, c.gpuDir)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeGPU = validGPU
			return nil
		})
	}

	// Transcoding GPU directory
	if f.Changed(keys.TranscodeGPUDir) {
		fns = append(fns, func(m *models.MetarrArgs) error {

			if c.gpuDir != "" {
				if _, err := os.Stat(c.gpuDir); err != nil {
					switch {
					case os.IsNotExist(err):
						return fmt.Errorf("gpu directory was entered as %v, but path does not exist", c.gpuDir)
					default:
						return fmt.Errorf("error checking GPU directory %v: %w", c.gpuDir, err)
					}
				}
			}
			m.TranscodeGPUDirectory = c.gpuDir
			return nil
		})
	}

	// Video codec
	if f.Changed(keys.TranscodeCodec) {
		validTranscodeCodec := c.transcodeVideoCodec

		if len(c.transcodeVideoCodec) != 0 {
			validTranscodeCodec, err = validation.ValidateVideoTranscodeCodecSlice(c.transcodeVideoCodec, c.useGPU)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeVideoCodecs = validTranscodeCodec
			return nil
		})
	}

	// Audio codec
	if f.Changed(keys.TranscodeAudioCodec) {
		validTranscodeAudioCodec := c.transcodeAudioCodec

		if len(c.transcodeAudioCodec) != 0 {
			validTranscodeAudioCodec, err = validation.ValidateAudioTranscodeCodecSlice(c.transcodeAudioCodec)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeAudioCodecs = validTranscodeAudioCodec
			return nil
		})
	}

	// Transcode quality
	if f.Changed(keys.TranscodeQuality) {
		validTranscodeQuality := c.transcodeQuality

		if c.transcodeQuality != "" {
			validTranscodeQuality, err = sharedvalidation.ValidateTranscodeQuality(c.transcodeQuality)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeQuality = validTranscodeQuality
			return nil
		})
	}

	// Transcode video filter
	if f.Changed(keys.TranscodeVideoFilter) {
		validTranscodeVideoFilter := c.transcodeVideoFilter

		if c.transcodeVideoFilter != "" {
			validTranscodeVideoFilter, err = validation.ValidateTranscodeVideoFilter(c.transcodeVideoFilter)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeVideoFilter = validTranscodeVideoFilter
			return nil
		})
	}
	return fns, nil
}

type chanSettings struct {
	concurrency            int
	cookiesFromBrowser     string
	crawlFreq              int
	externalDownloader     string
	externalDownloaderArgs string
	filters                []string
	filterFile             string
	fromDate               string
	jsonDir                string
	maxFilesize            string
	metaFilterMoveOps      []string
	metaFilterMoveOpsFile  string
	paused                 bool
	retries                int
	toDate                 string
	videoDir               string
	useGlobalCookies       bool
	ytdlpOutputExt         string
	extraYtdlpVideoArgs    string
	extraYtdlpMetaArgs     string
}

// getSettingsArgsFns creates the functions to send in to update the database with new values.
func getSettingsArgFns(cmd *cobra.Command, c chanSettings) (fns []func(m *models.Settings) error, err error) {
	f := cmd.Flags()

	// Concurrency
	if f.Changed(keys.ChanOrURLConcurrencyLimit) {
		fns = append(fns, func(s *models.Settings) error {
			s.Concurrency = max(c.concurrency, 1)
			return nil
		})
	}

	// Cookie source
	if f.Changed(keys.CookiesFromBrowser) {
		fns = append(fns, func(s *models.Settings) error {
			s.CookiesFromBrowser = c.cookiesFromBrowser
			return nil
		})
	}

	// Crawl frequency
	if f.Changed(keys.CrawlFreq) {
		fns = append(fns, func(s *models.Settings) error {
			s.CrawlFreq = max(c.crawlFreq, -1)
			return nil
		})
	}

	// Download retry amount
	if f.Changed(keys.DLRetries) {
		fns = append(fns, func(s *models.Settings) error {
			s.Retries = c.retries
			return nil
		})
	}

	// External downloader
	if f.Changed(keys.ExternalDownloader) {
		fns = append(fns, func(s *models.Settings) error {
			s.ExternalDownloader = c.externalDownloader
			return nil
		})
	}

	// External downloader arguments
	if f.Changed(keys.ExternalDownloaderArgs) {
		fns = append(fns, func(s *models.Settings) error {
			s.ExternalDownloaderArgs = c.externalDownloaderArgs
			return nil
		})
	}

	// Filter ops ('field:contains:frogs:must')
	if f.Changed(keys.FilterOpsInput) {
		dlFilters, err := parsing.ParseFilterOps(c.filters)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.Settings) error {
			s.Filters = dlFilters
			return nil
		})
	}

	// Move ops ('field:value:output directory')
	if f.Changed(keys.MoveOps) {
		moveOperations, err := parsing.ParseMetaFilterMoveOps(c.metaFilterMoveOps)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.Settings) error {
			s.MetaFilterMoveOps = moveOperations
			return nil
		})
	}

	// From date
	if f.Changed(keys.FromDate) {
		var validFromDate string

		if c.fromDate != "" {
			validFromDate, err = validation.ValidateToFromDate(c.fromDate)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(s *models.Settings) error {
			s.FromDate = validFromDate
			return nil
		})
	}

	// JSON directory
	if f.Changed(keys.JSONDir) {
		if c.jsonDir == "" {
			if c.videoDir == "" {
				return nil, fmt.Errorf("json directory cannot be empty. Attempted to default to video directory but video directory is also empty")
			}
			c.jsonDir = c.videoDir
		}

		if _, err = validation.ValidateDirectory(c.jsonDir, false); err != nil {
			return nil, err
		}

		fns = append(fns, func(s *models.Settings) error {
			s.JSONDir = c.jsonDir
			return nil
		})
	}

	// Max download filesize
	if f.Changed(keys.MaxFilesize) {
		if c.maxFilesize != "" {
			c.maxFilesize, err = validation.ValidateMaxFilesize(c.maxFilesize)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(s *models.Settings) error {
			s.MaxFilesize = c.maxFilesize
			return nil
		})
	}

	// Pause channel
	if f.Changed(keys.Pause) {
		fns = append(fns, func(s *models.Settings) error {
			s.Paused = c.paused
			return nil
		})
	}

	// To date
	if f.Changed(keys.ToDate) {
		var validToDate string

		if c.toDate != "" {
			validToDate, err = validation.ValidateToFromDate(c.toDate)
			if err != nil {
				return nil, err
			}
		}

		fns = append(fns, func(s *models.Settings) error {
			s.ToDate = validToDate
			return nil
		})
	}

	// Video directory
	if f.Changed(keys.VideoDir) {
		if c.videoDir == "" {
			return nil, fmt.Errorf("video directory cannot be empty")
		}

		if _, err = validation.ValidateDirectory(c.videoDir, false); err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.Settings) error {
			s.VideoDir = c.videoDir
			return nil
		})
	}

	// Use global cookies
	if f.Changed(keys.UseGlobalCookies) {
		fns = append(fns, func(s *models.Settings) error {
			s.UseGlobalCookies = c.useGlobalCookies
			return nil
		})
	}

	// YT-DLP output filetype for 'merge-output-format'
	if f.Changed(keys.YtdlpOutputExt) {
		if c.ytdlpOutputExt != "" {
			c.ytdlpOutputExt = strings.ToLower(c.ytdlpOutputExt)
			if err := validation.ValidateYtdlpOutputExtension(c.ytdlpOutputExt); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(s *models.Settings) error {
			s.YtdlpOutputExt = c.ytdlpOutputExt
			return nil
		})
	}

	// Additional yt-dlp video download arguments
	if f.Changed(keys.ExtraYTDLPVideoArgs) {
		fns = append(fns, func(s *models.Settings) error {
			s.ExtraYTDLPVideoArgs = c.extraYtdlpVideoArgs
			return nil
		})
	}

	// Additional yt-dlp metadata download arguments
	if f.Changed(keys.ExtraYTDLPMetaArgs) {
		fns = append(fns, func(s *models.Settings) error {
			s.ExtraYTDLPMetaArgs = c.extraYtdlpMetaArgs
			return nil
		})
	}
	return fns, nil
}

// getKeyVal returns a key and value for channel lookup.
func getChanKeyVal(id int, name string) (key, val string, err error) {
	switch {
	case id != 0:
		key = consts.QChanID
		val = strconv.Itoa(id)
	case name != "":
		key = consts.QChanName
		val = name
	default:
		return "", "", errors.New("please enter either a channel ID or channel name")
	}
	return key, val, nil
}

// verifyChanRowUpdateValid verifies that your update operation is valid
func verifyChanRowUpdateValid(col, val string) error {
	switch col {
	case "name", jsonkeys.SettingsVideoDirectory, jsonkeys.SettingsJSONDirectory:
		if val == "" {
			return fmt.Errorf("cannot set %s blank, please use the 'delete' function if you want to remove this channel entirely", col)
		}
	default:
		return errors.New("cannot set a custom value for internal DB elements")
	}
	return nil
}
