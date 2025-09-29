// Package cfgchannel sets up Cobra channel commands.
package cfgchannel

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	cfgfiles "tubarr/internal/cfg/files"
	cfgflags "tubarr/internal/cfg/flags"
	"tubarr/internal/cfg/validation"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
)

// InitChannelCmds is the entrypoint for initializing channel commands.
func InitChannelCmds(s interfaces.Store, ctx context.Context) *cobra.Command {
	channelCmd := &cobra.Command{
		Use:   "channel",
		Short: "Channel commands.",
		Long:  "Manage channels with various subcommands like add, delete, and list.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	cs := s.ChannelStore()

	// Add subcommands with dependencies
	channelCmd.AddCommand(addAuth(cs))
	channelCmd.AddCommand(addChannelCmd(cs, s, ctx))
	channelCmd.AddCommand(dlURLs(cs, s, ctx))
	channelCmd.AddCommand(crawlChannelCmd(cs, s, ctx))
	channelCmd.AddCommand(ignoreCrawl(cs, s, ctx))
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

	return channelCmd
}

// addAuth adds authentication details to a channel.
func addAuth(cs interfaces.ChannelStore) *cobra.Command {
	var (
		channelName                  string
		username, password, loginURL string
		authDetails                  []string
		channelID                    int
	)

	addAuthCmd := &cobra.Command{
		Use:   "auth",
		Short: "Add authentication details to a channel.",
		Long:  "Add authentication details to a channel for use in crawls.",
		RunE: func(cmd *cobra.Command, args []string) error {

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
			id, err := cs.GetID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel with %s %q", id, key, val)
			}

			// Fetch channel model (retrieve the URLs)
			c, err, hasRows := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key/value %q:%q", key, val)
			}
			if err != nil {
				return err
			}

			// Parse and set authentication details
			authDetails, err := parseAuthDetails(username, password, loginURL, authDetails, c.URLs, false)
			if err != nil {
				return err
			}

			if len(authDetails) > 0 {
				if err := cs.AddAuth(id, authDetails); err != nil {
					return err
				}
			}

			// Success
			logging.S(0, "Channel with %s %q set authentication details", key, val)
			return nil
		},
	}
	cfgflags.SetPrimaryChannelFlags(addAuthCmd, &channelName, nil, &channelID)
	cfgflags.SetAuthFlags(addAuthCmd, &username, &password, &loginURL, &authDetails)
	return addAuthCmd
}

// deleteURLs deletes a list of URLs inputted by the user.
func deleteVideoURLs(cs interfaces.ChannelStore) *cobra.Command {

	var (
		cFile, channelName string
		channelID          int
		urls               []string
	)

	deleteURLsCmd := &cobra.Command{
		Use:   "delete-video-urls",
		Short: "Remove video URLs from the database.",
		Long:  "If using a file, the file should contain one URL per line.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cFile == "" && len(urls) == 0 {
				return errors.New("must enter a URL source")
			}

			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			chanID, err := cs.GetID(key, val)
			if err != nil {
				return err
			}

			if err := cs.DeleteVideoURLs(chanID, urls); err != nil {
				return err
			}
			return nil
		},
	}

	cfgflags.SetPrimaryChannelFlags(deleteURLsCmd, &channelName, nil, &channelID)
	deleteURLsCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to delete from the database.")

	return deleteURLsCmd
}

// dlURLs downloads a list of URLs inputted by the user.
func dlURLs(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		cFile, channelName string
		channelID          int
		urls               []string
	)

	dlURLFileCmd := &cobra.Command{
		Use:   "download-urls",
		Short: "Download inputted URLs (plaintext or file).",
		Long:  "If using a file, the file should contain one URL per line.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Valid items set?
			if cFile == "" && len(urls) == 0 {
				return errors.New("must enter URLs into the source file, or set at least one URL directly")
			}

			// Check URL file if existent
			cFileInfo, err := validation.ValidateFile(cFile, false)
			if err != nil {
				return fmt.Errorf("file entered (%q) is not valid: %w", cFile, err)
			}
			if cFile != "" && cFileInfo.Size() == 0 {
				return fmt.Errorf("url file %q is blank", cFile)
			}

			// URL length already determined to be > 0 earlier.

			// Get and check key/val pair
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			// Set Viper flags
			cfgflags.SetChangedFlag(keys.URLAdd, urls, cmd.Flags())
			cfgflags.SetChangedFlag(keys.URLFile, cFile, cmd.Flags())

			// Crawl channel
			if err := cs.CrawlChannel(key, val, s, ctx); err != nil {
				return err
			}

			// Success
			logging.S(0, "Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	cfgflags.SetPrimaryChannelFlags(dlURLFileCmd, &channelName, nil, &channelID)
	dlURLFileCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to this channel")
	dlURLFileCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to download")

	return dlURLFileCmd
}

// deleteNotifyURLs deletes notification URLs from a channel.
func deleteNotifyURLs(cs interfaces.ChannelStore) *cobra.Command {
	var (
		channelName             string
		channelID               int
		notifyURLs, notifyNames []string
	)

	deleteNotifyCmd := &cobra.Command{
		Use:   "notify-delete",
		Short: "Deletes a notify function from a channel.",
		Long:  "Enter a fully qualified notification URL here to delete from the database.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Valid items set?
			if len(notifyURLs) == 0 && len(notifyNames) == 0 {
				return errors.New("must enter at least one notify URL or name to delete")
			}

			// Get and check ID
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			id, err := cs.GetID(key, val)
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
			logging.S(0, "Deleted notify URLs for channel with %s %q: %v", key, val, notifyURLs)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(deleteNotifyCmd, &channelName, nil, &channelID)
	deleteNotifyCmd.Flags().StringSliceVar(&notifyURLs, "notify-urls", nil, "Full notification URLs (e.g. 'http://YOUR_PLEX_SERVER_IP:32400/library/sections/LIBRARY_ID_NUMBER/refresh?X-Plex-Token=YOUR_PLEX_TOKEN_HERE')")
	deleteNotifyCmd.Flags().StringSliceVar(&notifyNames, "notify-names", nil, "Full notification names")

	return deleteNotifyCmd
}

// addNotifyURLs adds a notification URL (can use to send requests to update Plex libraries on new video addition).
func addNotifyURLs(cs interfaces.ChannelStore) *cobra.Command {
	var (
		channelName  string
		channelID    int
		notification []string
	)

	addNotifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Adds notify function to a channel.",
		Long:  "Enter fully qualified notification URLs here to send update requests to platforms like Plex etc. (notification pair format is 'URL|Friendly Name')",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Valid items set?
			if len(notification) == 0 {
				return errors.New("no notification URL|Name pairs entered")
			}

			// Get and check ID
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			id, err := cs.GetID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel name %q", id, channelName)
			}

			// Validate and add notification URLs
			validPairs, err := validation.ValidateNotificationPairs(notification)
			if err != nil {
				return err
			}

			if err := cs.AddNotifyURLs(id, validPairs); err != nil {
				return err
			}

			// Success
			logging.S(0, "Added notify URLs for channel with %s %q: %v", key, val, validPairs)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(addNotifyCmd, &channelName, nil, &channelID)
	cfgflags.SetNotifyFlags(addNotifyCmd, &notification)

	return addNotifyCmd
}

// addVideoURLToIgnore adds a user inputted URL to ignore from crawls.
func addVideoURLToIgnore(cs interfaces.ChannelStore) *cobra.Command {
	var (
		name, ignoreURL string
		id              int
	)

	ignoreURLCmd := &cobra.Command{
		Use:   "ignore-video-url",
		Short: "Adds a video URL to ignore.",
		Long:  "URLs added to this list will not be grabbed from channel crawls.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Valid items set?
			if ignoreURL == "" {
				return errors.New("cannot enter the target ignore URL blank")
			}

			// Get and check ID
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			id, err := cs.GetID(key, val)
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
			logging.S(0, "Ignoring URL %q for channel with %s %q", ignoreURL, key, val)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(ignoreURLCmd, &name, nil, nil)
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-video-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// ignoreCrawl crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func ignoreCrawl(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		name string
		id   int
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:   "ignore-crawl",
		Short: "Crawl a channel for URLs to ignore.",
		Long:  "Crawls the current state of a channel page and adds all video URLs to ignore.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Run an ignore crawl
			if err := cs.CrawlChannelIgnore(key, val, s, ctx); err != nil {
				return err
			}

			// Success
			logging.S(0, "Completed ignore crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(ignoreCrawlCmd, &name, nil, nil)

	return ignoreCrawlCmd
}

// pauseChannelCmd pauses a channel from downloads in upcoming crawls.
func pauseChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		name string
		id   int
	)

	pauseCmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause a channel.",
		Long:  "Paused channels won't download new videos when the main program runs a crawl.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Get channel model
			c, err, hasRows := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key/value %q:%q", key, val)
			}
			if err != nil {
				return err
			}

			// Alter and save model settings
			c.Settings.Paused = true

			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.ChannelSettings) error {
				s.Paused = c.Settings.Paused
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to unpause channel: %w", err)
			}

			// Success
			logging.S(0, "Paused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(pauseCmd, &name, nil, &id)

	return pauseCmd
}

// pauseChannelCmd pauses a channel from downloads in upcoming crawls.
func unpauseChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		name string
		id   int
	)

	unPauseCmd := &cobra.Command{
		Use:   "unpause",
		Short: "Unpause a channel.",
		Long:  "Unpauses a channel, allowing it to download new videos when the main program runs a crawl.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Get channel model
			c, err, hasRows := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key %q and value %q", key, val)
			}
			if err != nil {
				return err
			}

			// Alter and save model settings
			c.Settings.Paused = false

			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.ChannelSettings) error {
				s.Paused = c.Settings.Paused
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to unpause channel: %w", err)
			}

			// Success
			logging.S(0, "Unpaused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(unPauseCmd, &name, nil, &id)

	return unPauseCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		urls []string
		name, vDir, jDir, outDir, cookieSource,
		externalDownloader, externalDownloaderArgs, maxFilesize, filenameDateTag, renameStyle, minFreeMem, metarrExt string
		urlOutDirs                                                                                      []string
		username, password, loginURL                                                                    string
		authDetails                                                                                     []string
		notification                                                                                    []string
		fromDate, toDate                                                                                string
		dlFilters, metaOps, moveOps, fileSfxReplace                                                     []string
		configFile, dlFilterFile, moveOpFile                                                            string
		crawlFreq, concurrency, metarrConcurrency, retries                                              int
		maxCPU                                                                                          float64
		transcodeGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter, ytdlpOutputExt string
		pause, ignoreRun, useGlobalCookies                                                              bool
		extraFFmpegArgs                                                                                 string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, settings, etc.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Load in config file
			if configFile != "" {
				if err := cfgfiles.LoadConfigFile(configFile); err != nil {
					return err
				}
			}

			// Got valid items?
			if vDir == "" || len(urls) == 0 || name == "" {
				return errors.New("new channels require a video directory, name, and at least one channel URL")
			}

			if jDir == "" {
				jDir = vDir
			}

			if _, err := validation.ValidateDirectory(vDir, true); err != nil {
				return err
			}

			// Validate filter operations
			dlFilters, err := validation.ValidateFilterOps(dlFilters)
			if err != nil {
				return err
			}

			// Validate move operation filters
			moveOps, err := validation.ValidateMoveOps(moveOps)
			if err != nil {
				return err
			}

			// Filename date tag
			if filenameDateTag != "" {
				if !validation.ValidateDateFormat(filenameDateTag) {
					return errors.New("invalid Metarr filename date tag format")
				}
			}

			// Meta operations
			if len(metaOps) > 0 {
				if metaOps, err = validation.ValidateMetaOps(metaOps); err != nil {
					return err
				}
			}

			// Filename suffix replace (e.g. '_1' to '')
			if len(fileSfxReplace) > 0 {
				if fileSfxReplace, err = validation.ValidateFilenameSuffixReplace(fileSfxReplace); err != nil {
					return err
				}
			}

			// Rename style
			if renameStyle != "" {
				if err := validation.ValidateRenameFlag(renameStyle); err != nil {
					return err
				}
			}

			// Min free memory
			if minFreeMem != "" {
				if err := validation.ValidateMinFreeMem(minFreeMem); err != nil {
					return err
				}
			}

			// From date
			if fromDate != "" {
				if fromDate, err = validation.ValidateToFromDate(fromDate); err != nil {
					return err
				}
			}

			// To date
			if toDate != "" {
				if toDate, err = validation.ValidateToFromDate(toDate); err != nil {
					return err
				}
			}

			// HW Acceleration GPU
			if transcodeGPU != "" {
				if transcodeGPU, gpuDir, err = validation.ValidateGPU(transcodeGPU, gpuDir); err != nil {
					return err
				}
			}

			// Video codec
			if codec != "" {
				if codec, err = validation.ValidateTranscodeCodec(codec, transcodeGPU); err != nil {
					return err
				}
			}

			// Audio codec
			if audioCodec != "" {
				if audioCodec, err = validation.ValidateTranscodeAudioCodec(audioCodec); err != nil {
					return err
				}
			}

			// Transcode quality
			if transcodeQuality != "" {
				if transcodeQuality, err = validation.ValidateTranscodeQuality(transcodeQuality); err != nil {
					return err
				}
			}

			// YTDLP output extension ('merge-output-format')
			if ytdlpOutputExt != "" {
				ytdlpOutputExt = strings.ToLower(ytdlpOutputExt)
				if err = validation.ValidateYtdlpOutputExtension(ytdlpOutputExt); err != nil {
					return err
				}
			}

			// Initialize channel
			now := time.Now()
			c := &models.Channel{
				URLs: urls,
				Name: name,

				// Fill channel
				Settings: &models.ChannelSettings{
					ChannelConfigFile:      configFile,
					Concurrency:            concurrency,
					CookieSource:           cookieSource,
					CrawlFreq:              crawlFreq,
					ExternalDownloader:     externalDownloader,
					ExternalDownloaderArgs: externalDownloaderArgs,
					Filters:                dlFilters,
					FilterFile:             dlFilterFile,
					FromDate:               fromDate,
					JSONDir:                jDir,
					MaxFilesize:            maxFilesize,
					MoveOps:                moveOps,
					MoveOpFile:             moveOpFile,
					Paused:                 pause,
					Retries:                retries,
					ToDate:                 toDate,
					UseGlobalCookies:       useGlobalCookies,
					VideoDir:               vDir,
					YtdlpOutputExt:         ytdlpOutputExt,
				},

				MetarrArgs: &models.MetarrArgs{
					Ext:                  metarrExt,
					ExtraFFmpegArgs:      extraFFmpegArgs,
					MetaOps:              metaOps,
					FilenameDateTag:      filenameDateTag,
					RenameStyle:          renameStyle,
					FilenameReplaceSfx:   fileSfxReplace,
					MaxCPU:               maxCPU,
					MinFreeMem:           minFreeMem,
					OutputDir:            outDir,
					URLOutputDirs:        urlOutDirs,
					Concurrency:          metarrConcurrency,
					UseGPU:               transcodeGPU,
					GPUDir:               gpuDir,
					TranscodeCodec:       codec,
					TranscodeAudioCodec:  audioCodec,
					TranscodeQuality:     transcodeQuality,
					TranscodeVideoFilter: transcodeVideoFilter,
				},

				LastScan:  now,
				CreatedAt: now,
				UpdatedAt: now,
			}

			// Add channel to database
			channelID, err := cs.AddChannel(c)
			if err != nil {
				return err
			}

			// Parse and validate authentication details
			authMap, err := parseAuthDetails(username, password, loginURL, authDetails, urls, false)
			if err != nil {
				return err
			}

			if len(authMap) > 0 {
				if err := cs.AddAuth(channelID, authMap); err != nil {
					return err
				}
			}

			// Validate notification URLs
			if len(notification) != 0 {
				validPairs, err := validation.ValidateNotificationPairs(notification)
				if err != nil {
					return err
				}

				if err := cs.AddNotifyURLs(channelID, validPairs); err != nil {
					return err
				}
			}

			// Should perform an ignore run?
			if ignoreRun {
				logging.I("Running an 'ignore crawl'...")
				cID := strconv.FormatInt(channelID, 10)
				if err := cs.CrawlChannelIgnore("id", cID, s, ctx); err != nil {
					logging.E(0, "Failed to complete ignore crawl run: %v", err)
				}
			}

			// Success
			logging.S(0, "Completed addition of channel %q to Tubarr", name)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(addCmd, &name, &urls, nil)

	// Files/dirs
	cfgflags.SetFileDirFlags(addCmd, &configFile, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(addCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader,
		&moveOpFile, &moveOps, &pause,
		false)

	// Download
	cfgflags.SetDownloadFlags(addCmd, &retries, &useGlobalCookies,
		&ytdlpOutputExt, &fromDate, &toDate,
		&cookieSource, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(addCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &filenameDateTag,
		&minFreeMem, &outDir, &renameStyle,
		&urlOutDirs, &fileSfxReplace, &metaOps)

	// Login credentials
	cfgflags.SetAuthFlags(addCmd, &username, &password, &loginURL, &authDetails)

	// Transcoding
	cfgflags.SetTranscodeFlags(addCmd, &transcodeGPU, &gpuDir,
		&transcodeVideoFilter, &codec, &audioCodec,
		&transcodeQuality)

	// Notification URL
	cfgflags.SetNotifyFlags(addCmd, &notification)

	// Pause or ignore run
	addCmd.Flags().BoolVar(&pause, "pause", false, "Paused channels won't crawl videos on a normal program run")
	addCmd.Flags().BoolVar(&ignoreRun, "ignore-run", false, "Run an 'ignore crawl' first so only new videos are downloaded (rather than the entire channel backlog)")

	return addCmd
}

// deleteChannelCmd deletes a channel from the database.
func deleteChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		urls []string
		name string
		id   int
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete channels.",
		Long:  "Delete a channel by ID, name, or URL.",
		RunE: func(cmd *cobra.Command, args []string) error {

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
			logging.S(0, "Successfully deleted channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(delCmd, &name, &urls, &id)

	return delCmd
}

// listAllChannel returns details about a single channel in the database.
func listChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		name      string
		channelID int
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List a channel's details.",
		Long:  "Lists details of a channel in the database.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(channelID, name)
			if err != nil {
				return err
			}

			// Fetch channel model
			ch, err, hasRows := cs.FetchChannelModel(key, val)
			if !hasRows {
				logging.I("Entry for channel with %s %q does not exist in the database", key, val)
				return nil
			}
			if err != nil {
				return err
			}

			// Display settings and return
			displaySettings(cs, ch)
			return nil
		},
	}
	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(listCmd, &name, nil, &channelID)
	return listCmd
}

// listAllChannelsCmd returns a list of channels in the database.
func listAllChannelsCmd(cs interfaces.ChannelStore) *cobra.Command {
	listAllCmd := &cobra.Command{
		Use:   "list-all",
		Short: "List all channels.",
		Long:  "Lists all channels currently saved in the database.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Fetch channels from database
			chans, err, hasRows := cs.FetchAllChannels()
			if !hasRows {
				logging.I("No entries in the database")
				return nil
			}
			if err != nil {
				return err
			}

			// Display settings and return
			for _, ch := range chans {
				displaySettings(cs, ch)
			}
			return nil
		},
	}
	return listAllCmd
}

// crawlChannelCmd initiates a crawl of a given channel.
func crawlChannelCmd(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		name string
		id   int
	)

	crawlCmd := &cobra.Command{
		Use:   "crawl",
		Short: "Crawl a channel for new URLs.",
		Long:  "Initiate a crawl for new URLs of a channel that have not yet been downloaded.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Crawl channel
			if err := cs.CrawlChannel(key, val, s, ctx); err != nil {
				return err
			}

			// Success
			logging.S(0, "Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(crawlCmd, &name, nil, &id)

	return crawlCmd
}

// updateChannelSettingsCmd updates channel settings.
func updateChannelSettingsCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		urls                                                                      []string
		id, concurrency, crawlFreq, metarrConcurrency, retries                    int
		maxCPU                                                                    float64
		vDir, jDir, outDir                                                        string
		urlOutDirs                                                                []string
		name, cookieSource                                                        string
		minFreeMem, renameStyle, filenameDateTag, metarrExt                       string
		maxFilesize, externalDownloader, externalDownloaderArgs                   string
		username, password, loginURL                                              string
		authDetails                                                               []string
		dlFilters, metaOps, moveOps                                               []string
		configFile, dlFilterFile, moveOpsFile                                     string
		fileSfxReplace                                                            []string
		useGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter string
		fromDate, toDate                                                          string
		ytdlpOutExt                                                               string
		useGlobalCookies, pause, deleteAuth                                       bool
		extraFFmpegArgs                                                           string
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-settings",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Load in config
			if configFile != "" {
				if err := cfgfiles.LoadConfigFile(configFile); err != nil {
					return err
				}
			}

			// Check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Fetch model from database
			c, err, _ := cs.FetchChannelModel(key, val)
			if err != nil {
				return err
			}

			// Load config file location if existent and one wasn't hardcoded into terminal
			if configFile == "" && c.Settings.ChannelConfigFile != "" {
				if err := cfgfiles.LoadConfigFile(configFile); err != nil {
					return err
				}
			}

			// Parse and set authentication details if set by user, clear all if flag is set
			if cmd.Flags().Changed(keys.AuthUsername) ||
				cmd.Flags().Changed(keys.AuthPassword) ||
				cmd.Flags().Changed(keys.AuthURL) ||
				cmd.Flags().Changed(keys.AuthDetails) ||
				deleteAuth {

				// Get and check ID
				id, err := cs.GetID(key, val)
				if err != nil {
					return err
				}

				if id == 0 {
					return fmt.Errorf("could not get valid ID (got %d) for channel with %s %q", id, key, val)
				}

				authDetails, err := parseAuthDetails(username, password, loginURL, authDetails, c.URLs, deleteAuth)
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
				channelConfigFile:      configFile,
				concurrency:            concurrency,
				cookieSource:           cookieSource,
				crawlFreq:              crawlFreq,
				externalDownloader:     externalDownloader,
				externalDownloaderArgs: externalDownloaderArgs,
				filters:                dlFilters,
				filterFile:             dlFilterFile,
				fromDate:               fromDate,
				maxFilesize:            maxFilesize,
				moveOps:                moveOps,
				moveOpsFile:            moveOpsFile,
				paused:                 pause,
				retries:                retries,
				toDate:                 toDate,
				videoDir:               vDir,
				useGlobalCookies:       useGlobalCookies,
				ytdlpOutputExt:         ytdlpOutExt,
			})
			if err != nil {
				return err
			}

			if len(fnSettingsArgs) > 0 {
				finalUpdateFn := func(s *models.ChannelSettings) error {
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
				filenameReplaceSfx:   fileSfxReplace,
				renameStyle:          renameStyle,
				extraFFmpegArgs:      extraFFmpegArgs,
				filenameDateTag:      filenameDateTag,
				metarrExt:            metarrExt,
				metaOps:              metaOps,
				outputDir:            outDir,
				urlOutputDirs:        urlOutDirs,
				concurrency:          metarrConcurrency,
				maxCPU:               maxCPU,
				minFreeMem:           minFreeMem,
				useGPU:               useGPU,
				gpuDir:               gpuDir,
				transcodeCodec:       codec,
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
			logging.S(0, "Completed update for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(updateSettingsCmd, &name, &urls, &id)

	// Files/dirs
	cfgflags.SetFileDirFlags(updateSettingsCmd, &configFile, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(updateSettingsCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader, &moveOpsFile,
		&moveOps, &pause, true)

	// Download
	cfgflags.SetDownloadFlags(updateSettingsCmd, &retries, &useGlobalCookies,
		&ytdlpOutExt, &fromDate, &toDate,
		&cookieSource, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &filenameDateTag,
		&minFreeMem, &outDir, &renameStyle,
		&urlOutDirs, &fileSfxReplace, &metaOps)

	// Transcoding
	cfgflags.SetTranscodeFlags(updateSettingsCmd, &useGPU, &gpuDir,
		&transcodeVideoFilter, &codec, &audioCodec,
		&transcodeQuality)

	// Auth
	cfgflags.SetAuthFlags(updateSettingsCmd, &username, &password,
		&loginURL, &authDetails)

	updateSettingsCmd.Flags().BoolVar(&deleteAuth, "delete-auth", false, "Clear all authentication details for this channel and its URLs")

	return updateSettingsCmd
}

// updateChannelValue provides a command allowing the alteration of a channel row.
func updateChannelValue(cs interfaces.ChannelStore) *cobra.Command {
	var (
		col, newVal, name string
		id                int
	)

	updateRowCmd := &cobra.Command{
		Use:   "update-value",
		Short: "Update a channel column value.",
		Long:  "Enter a column to update and a value to update that column to.",
		RunE: func(cmd *cobra.Command, args []string) error {

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
			logging.S(0, "Updated channel column: %q → %q", col, newVal)
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(updateRowCmd, &name, nil, &id)

	updateRowCmd.Flags().StringVarP(&col, "column-name", "c", "", "The name of the column in the table (e.g. video_directory)")
	updateRowCmd.Flags().StringVarP(&newVal, "value", "v", "", "The value to set in the column (e.g. /my-directory)")
	return updateRowCmd
}

// displaySettings displays fields relevant to a channel.
func displaySettings(cs interfaces.ChannelStore, c *models.Channel) {
	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		logging.E(0, "Unable to fetch notification URLs for channel %q: %v", c.Name, err)
	}

	s := c.Settings
	m := c.MetarrArgs

	fmt.Printf("\n\n\n%s[ Channel: %q ]%s\n", consts.ColorGreen, c.Name, consts.ColorReset)

	// Channel basic info
	fmt.Printf("\n%sBasic Info:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("ID: %d\n", c.ID)
	fmt.Printf("Name: %s\n", c.Name)
	fmt.Printf("URL: %+v\n", c.URLs)
	fmt.Printf("Paused: %v\n", s.Paused)

	// Channel settings
	fmt.Printf("\n%sChannel Settings:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Video Directory: %s\n", s.VideoDir)
	fmt.Printf("JSON Directory: %s\n", s.JSONDir)
	fmt.Printf("Config File: %s\n", s.ChannelConfigFile)
	fmt.Printf("Crawl Frequency: %d minutes\n", s.CrawlFreq)
	fmt.Printf("Concurrency: %d\n", s.Concurrency)
	fmt.Printf("Cookie Source: %s\n", s.CookieSource)
	fmt.Printf("Retries: %d\n", s.Retries)
	fmt.Printf("External Downloader: %s\n", s.ExternalDownloader)
	fmt.Printf("External Downloader Args: %s\n", s.ExternalDownloaderArgs)
	fmt.Printf("Filter Ops: %v\n", s.Filters)
	fmt.Printf("Filter File: %s\n", s.FilterFile)
	fmt.Printf("From Date: %q\n", hyphenateYyyyMmDd(s.FromDate))
	fmt.Printf("To Date: %q\n", hyphenateYyyyMmDd(s.ToDate))
	fmt.Printf("Max Filesize: %s\n", s.MaxFilesize)
	fmt.Printf("Move Ops: %v\n", s.MoveOps)
	fmt.Printf("Move Ops File: %s\n", s.MoveOpFile)
	fmt.Printf("Use Global Cookies: %v\n", s.UseGlobalCookies)
	fmt.Printf("Yt-dlp Output Extension: %s\n", s.YtdlpOutputExt)

	// Metarr settings
	fmt.Printf("\n%sMetarr Settings:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Default Output Directory: %s\n", m.OutputDir)
	fmt.Printf("URL-Specific Output Directories: %v\n", m.URLOutputDirs)
	fmt.Printf("Output Filetype: %s\n", m.Ext)
	fmt.Printf("Metarr Concurrency: %d\n", m.Concurrency)
	fmt.Printf("Max CPU: %.2f\n", m.MaxCPU)
	fmt.Printf("Min Free Memory: %s\n", m.MinFreeMem)
	fmt.Printf("HW Acceleration: %s\n", m.UseGPU)
	fmt.Printf("HW Acceleration Directory: %s\n", m.GPUDir)
	fmt.Printf("Video Codec: %s\n", m.TranscodeCodec)
	fmt.Printf("Audio Codec: %s\n", m.TranscodeAudioCodec)
	fmt.Printf("Transcode Quality: %s\n", m.TranscodeQuality)
	fmt.Printf("Rename Style: %s\n", m.RenameStyle)
	fmt.Printf("Filename Suffix Replace: %v\n", m.FilenameReplaceSfx)
	fmt.Printf("Meta Operations: %v\n", m.MetaOps)
	fmt.Printf("Filename Date Format: %s\n", m.FilenameDateTag)

	// Extra arguments
	fmt.Printf("Extra FFmpeg Arguments: %s\n", m.ExtraFFmpegArgs)

	// Notification URLs
	fmt.Printf("\n%sNotify URLs:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Notification URLs: %v\n", notifyURLs)
}

// UpdateChannelFromConfig updates the channel settings from a config file if it exists.
func UpdateChannelFromConfig(cs interfaces.ChannelStore, c *models.Channel) error {
	cfgFile := c.Settings.ChannelConfigFile
	if cfgFile == "" {
		logging.D(2, "No config file path, nothing to apply")
		return nil
	}

	logging.I("Updating channel from config file %q...", cfgFile)
	if _, err := validation.ValidateFile(cfgFile, false); err != nil {
		return err
	}

	if err := cfgfiles.LoadConfigFile(cfgFile); err != nil {
		return err
	}

	if err := applyConfigChannelSettings(c); err != nil {
		return err
	}

	if err := applyConfigMetarrSettings(c); err != nil {
		return err
	}

	key, val, err := getChanKeyVal(int(c.ID), c.Name)
	if err != nil {
		return err
	}

	_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.ChannelSettings) error {
		if c.Settings == nil {
			return fmt.Errorf("c.Settings is nil")
		}
		*s = *c.Settings
		return nil
	})
	if err != nil {
		return err
	}

	_, err = cs.UpdateChannelMetarrArgsJSON(key, val, func(m *models.MetarrArgs) error {
		if c.MetarrArgs == nil {
			return fmt.Errorf("c.MetarrArgs is nil")
		}
		*m = *c.MetarrArgs
		return nil
	})
	if err != nil {
		return err
	}

	logging.S(0, "Updated channel %q from config file", c.Name)
	return nil
}

// applyConfigChannelSettings applies the channel settings to the model and saves to database.
func applyConfigChannelSettings(c *models.Channel) (err error) {
	// Initialize settings model if nil
	if c.Settings == nil {
		c.Settings = &models.ChannelSettings{}
	}

	// Channel config file location
	if v, ok := getConfigValue[string](keys.ChannelConfigFile); ok {
		if _, err = validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.Settings.ChannelConfigFile = v
	}

	// Concurrency limit
	if v, ok := getConfigValue[int](keys.ConcurrencyLimitInput); ok {
		c.Settings.Concurrency = validation.ValidateConcurrencyLimit(v)
	}

	// Cookie source
	if v, ok := getConfigValue[string](keys.CookieSource); ok {
		c.Settings.CookieSource = v // No check for this currently! (cookies-from-browser)
	}

	// Crawl frequency
	if v, ok := getConfigValue[int](keys.CrawlFreq); ok {
		c.Settings.CrawlFreq = v
	}

	// Download retries
	if v, ok := getConfigValue[int](keys.DLRetries); ok {
		c.Settings.Retries = v
	}

	// External downloader
	if v, ok := getConfigValue[string](keys.ExternalDownloader); ok {
		c.Settings.ExternalDownloader = v // No checks for this yet.
	}

	// External downloader arguments
	if v, ok := getConfigValue[string](keys.ExternalDownloaderArgs); ok {
		c.Settings.ExternalDownloaderArgs = v // No checks for this yet.
	}

	// Filter ops file
	if v, ok := getConfigValue[string](keys.FilterOpsFile); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.Settings.FilterFile = v
	}

	// From date
	if v, ok := getConfigValue[string](keys.FromDate); ok {
		if c.Settings.FromDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// JSON directory
	if v, ok := getConfigValue[string](keys.JSONDir); ok {
		if _, err = validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.Settings.JSONDir = v
	}

	// Max filesize to download
	if v, ok := getConfigValue[string](keys.MaxFilesize); ok {
		c.Settings.MaxFilesize = v
	}

	// Pause channel
	if v, ok := getConfigValue[bool](keys.Pause); ok {
		c.Settings.Paused = v
	}

	// To date
	if v, ok := getConfigValue[string](keys.ToDate); ok {
		if c.Settings.ToDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// Use global cookies?
	if v, ok := getConfigValue[bool](keys.UseGlobalCookies); ok {
		c.Settings.UseGlobalCookies = v
	}

	// Video directory
	if v, ok := getConfigValue[string](keys.VideoDir); ok {
		if _, err = validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.Settings.VideoDir = v
	}

	// YTDLP output format
	if v, ok := getConfigValue[string](keys.YtdlpOutputExt); ok {
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return err
		}
		c.Settings.YtdlpOutputExt = v
	}
	return nil
}

// applyConfigMetarrSettings applies the Metarr settings to the model and saves to database.
func applyConfigMetarrSettings(c *models.Channel) (err error) {
	// Initialize MetarrArgs model if nil
	if c.MetarrArgs == nil {
		c.MetarrArgs = &models.MetarrArgs{}
	}

	var (
		gpuDirGot, gpuGot string
		videoCodecGot     string
	)

	// Metarr output extension
	if v, ok := getConfigValue[string](keys.MExt); ok {
		if _, err := validation.ValidateOutputFiletype(c.Settings.ChannelConfigFile); err != nil {
			return fmt.Errorf("metarr output filetype %q in config file %q is invalid", v, c.Settings.ChannelConfigFile)
		}
		c.MetarrArgs.Ext = v
	}

	// Filename suffix replacements
	if v, ok := getConfigValue[[]string](keys.MFilenameReplaceSuffix); ok {
		c.MetarrArgs.FilenameReplaceSfx, err = validation.ValidateFilenameSuffixReplace(v)
		if err != nil {
			return err
		}
	}

	// Rename style
	if v, ok := getConfigValue[string](keys.MRenameStyle); ok {
		if err := validation.ValidateRenameFlag(v); err != nil {
			return err
		}
		c.MetarrArgs.RenameStyle = v
	}

	// Extra FFmpeg arguments
	if v, ok := getConfigValue[string](keys.MExtraFFmpegArgs); ok {
		c.MetarrArgs.ExtraFFmpegArgs = v
	}

	// Filename date tag
	if v, ok := getConfigValue[string](keys.MFilenameDateTag); ok {
		if ok := validation.ValidateDateFormat(v); !ok {
			return fmt.Errorf("date format %q in config file %q is invalid", v, c.Settings.ChannelConfigFile)
		}
		c.MetarrArgs.FilenameDateTag = v
	}

	// Meta ops
	if v, ok := getConfigValue[[]string](keys.MMetaOps); ok {
		c.MetarrArgs.MetaOps, err = validation.ValidateMetaOps(v)
		if err != nil {
			return err
		}
	}

	// Default output directory
	if v, ok := getConfigValue[string](keys.MOutputDir); ok {
		if _, err := validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.MetarrArgs.OutputDir = v
	}

	// Per-URL output directory
	if v, ok := getConfigValue[[]string](keys.MURLOutputDirs); ok && len(v) != 0 {

		valid := make([]string, 0, len(v))

		for _, d := range v {
			split := strings.Split(d, "|")
			if len(split) == 2 && split[1] != "" {
				valid = append(valid, d)
			} else {
				logging.W("Removed invalid per-URL output directory pair %q", d)
			}
		}

		if len(valid) == 0 {
			c.MetarrArgs.URLOutputDirs = nil
		} else {
			c.MetarrArgs.URLOutputDirs = valid
		}
	}

	// Metarr concurrency
	if v, ok := getConfigValue[int](keys.MConcurrency); ok {
		c.MetarrArgs.Concurrency = validation.ValidateConcurrencyLimit(v)
	}

	// Metarr max CPU
	if v, ok := getConfigValue[float64](keys.MMaxCPU); ok {
		c.MetarrArgs.MaxCPU = v // Handled in Metarr
	}

	// Metarr minimum memory to reserve
	if v, ok := getConfigValue[string](keys.MMinFreeMem); ok {
		c.MetarrArgs.MinFreeMem = v // Handled in Metarr
	}

	// Metarr GPU
	if v, ok := getConfigValue[string](keys.TranscodeGPU); ok {
		gpuGot = v
	}
	if v, ok := getConfigValue[string](keys.TranscodeGPUDir); ok {
		gpuDirGot = v
	}

	// Metarr video filter
	if v, ok := getConfigValue[string](keys.TranscodeVideoFilter); ok {
		c.MetarrArgs.TranscodeVideoFilter, err = validation.ValidateTranscodeVideoFilter(v)
		if err != nil {
			return err
		}
	}

	// Metarr video codec
	if v, ok := getConfigValue[string](keys.TranscodeCodec); ok {
		videoCodecGot = v
	}

	// Metarr audio codec
	if v, ok := getConfigValue[string](keys.TranscodeAudioCodec); ok {
		if c.MetarrArgs.TranscodeAudioCodec, err = validation.ValidateTranscodeAudioCodec(v); err != nil {
			return err
		}
	}

	// Metarr transcode quality
	if v, ok := getConfigValue[string](keys.MTranscodeQuality); ok {
		if c.MetarrArgs.TranscodeQuality, err = validation.ValidateTranscodeQuality(v); err != nil {
			return err
		}
	}

	// Transcode GPU validation
	if gpuGot != "" || gpuDirGot != "" {
		c.MetarrArgs.UseGPU, c.MetarrArgs.GPUDir, err = validation.ValidateGPU(gpuGot, gpuDirGot)
		if err != nil {
			return err
		}
	}

	// Validate video codec against transcode GPU
	if c.MetarrArgs.TranscodeCodec, err = validation.ValidateTranscodeCodec(videoCodecGot, c.MetarrArgs.UseGPU); err != nil {
		return err
	}

	return nil
}
