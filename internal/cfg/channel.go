package cfg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/auth"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/file"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	channelCmd.AddCommand(unblockChannelCmd(cs))
	channelCmd.AddCommand(downloadVideoURLs(cs, s, ctx))
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
			id, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel with %s %q", id, key, val)
			}

			// Fetch channel model (retrieve the URLs)
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key/value %q:%q", key, val)
			}
			if err != nil {
				return err
			}

			// Parse and set authentication details
			cURLs := c.GetURLs()
			authDetails, err := parseAuthDetails(username, password, loginURL, authDetails, cURLs, false)
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
	setPrimaryChannelFlags(addAuthCmd, &channelName, nil, &channelID)
	setAuthFlags(addAuthCmd, &username, &password, &loginURL, &authDetails)
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

			chanID, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if err := cs.DeleteVideoURLs(chanID, urls); err != nil {
				return err
			}
			return nil
		},
	}

	setPrimaryChannelFlags(deleteURLsCmd, &channelName, nil, &channelID)
	deleteURLsCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to delete from the database.")

	return deleteURLsCmd
}

// downloadVideoURLs downloads a list of URLs inputted by the user.
func downloadVideoURLs(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		cFile, channelName string
		channelID          int
		urls               []string
	)

	manualURLCmd := &cobra.Command{
		Use:           "download-video-urls",
		Short:         "Download inputted URLs (plaintext or file).",
		Long:          "If using a file, the file should contain one URL per line.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				err := fmt.Errorf("no channel model in database with %s %q", key, val)
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			// Download URLs - errors already logged, DON'T print here
			if err := cs.DownloadVideoURLs(key, val, c, s, videoURLs, ctx); err != nil {
				return err
			}

			// Success
			logging.S(0, "Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	setPrimaryChannelFlags(manualURLCmd, &channelName, nil, &channelID)
	manualURLCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to your given channel")
	manualURLCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to download for a given channel")

	return manualURLCmd
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
			logging.S(0, "Deleted notify URLs for channel with %s %q: %v", key, val, notifyURLs)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(deleteNotifyCmd, &channelName, nil, &channelID)
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

			id, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}

			if id == 0 {
				return fmt.Errorf("could not get valid ID (got %d) for channel name %q", id, channelName)
			}

			// Validate and add notification URLs
			validPairs, err := validation.ValidateNotificationStrings(notification)
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
	setPrimaryChannelFlags(addNotifyCmd, &channelName, nil, &channelID)
	setNotifyFlags(addNotifyCmd, &notification)

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
			logging.S(0, "Ignoring URL %q for channel with %s %q", ignoreURL, key, val)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(ignoreURLCmd, &name, nil, nil)
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
		Use:          "ignore-crawl",
		Short:        "Crawl a channel for URLs to ignore.",
		Long:         "Crawls the current state of a channel page and adds all video URLs to ignore.",
		SilenceUsage: true,
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
	setPrimaryChannelFlags(ignoreCrawlCmd, &name, nil, nil)

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
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key/value %q:%q", key, val)
			}
			if err != nil {
				return err
			}

			// Alter and save model settings
			c.ChanSettings.Paused = true

			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.ChannelSettings) error {
				s.Paused = c.ChanSettings.Paused
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
	setPrimaryChannelFlags(pauseCmd, &name, nil, &id)

	return pauseCmd
}

// unpauseChannelCmd unpauses a channel to allow for downloads in upcoming crawls.
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
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key %q and value %q", key, val)
			}
			if err != nil {
				return err
			}

			// Send update
			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.ChannelSettings) error {
				s.Paused = false
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
	setPrimaryChannelFlags(unPauseCmd, &name, nil, &id)

	return unPauseCmd
}

// unblockChannelCmd unblocks channels which were locked (usually due to bot activity detection).
func unblockChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		name string
		id   int
	)

	unblockCmd := &cobra.Command{
		Use:   "unblock",
		Short: "Unblock a channel.",
		Long:  "Unblocks a channel (usually blocked due to sites detecting Tubarr as a bot), allowing it to download new videos.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Get channel model
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("channel model does not exist in database for channel with key %q and value %q", key, val)
			}
			if err != nil {
				return err
			}

			// Send update
			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.ChannelSettings) error {
				s.BotBlocked = false
				s.BotBlockedHostnames = nil
				s.BotBlockedTimestamps = make(map[string]time.Time)
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to unblock channel: %w", err)
			}

			// Success
			logging.S(0, "Unblocked channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(unblockCmd, &name, nil, &id)

	return unblockCmd
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
				if err := loadConfigFile(configFile); err != nil {
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

			// Parse and validate authentication details
			authMap, err := parseAuthDetails(username, password, loginURL, authDetails, urls, false)
			if err != nil {
				return err
			}

			now := time.Now()
			var chanURLs []*models.ChannelURL
			for _, u := range urls {
				if u != "" {

					// Fill auth details if existent
					var parsedUsername, parsedPassword, parsedLoginURL string
					if _, exists := authMap[u]; exists {
						parsedUsername = authMap[u].Username
						parsedPassword = authMap[u].Password
						parsedLoginURL = authMap[u].LoginURL
					}

					// Create channel model
					newChanURL := &models.ChannelURL{
						URL:       u,
						Username:  parsedUsername,
						Password:  parsedPassword,
						LoginURL:  parsedLoginURL,
						LastScan:  now,
						CreatedAt: now,
						UpdatedAt: now,
					}
					chanURLs = append(chanURLs, newChanURL)
				}
			}

			// Initialize channel
			c := &models.Channel{
				URLModels: chanURLs,
				Name:      name,

				// Fill channel
				ChanSettings: &models.ChannelSettings{
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

				ChanMetarrArgs: &models.MetarrArgs{
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

			if len(authMap) > 0 {
				if err := cs.AddAuth(channelID, authMap); err != nil {
					return err
				}
			}

			// Validate notification URLs
			if len(notification) != 0 {
				validPairs, err := validation.ValidateNotificationStrings(notification)
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
					logging.E("Failed to complete ignore crawl run: %v", err)
				}
			}

			// Success
			logging.S(0, "Completed addition of channel %q to Tubarr", name)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(addCmd, &name, &urls, nil)

	// Files/dirs
	setFileDirFlags(addCmd, &configFile, &jDir, &vDir)

	// Program related
	setProgramRelatedFlags(addCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader,
		&moveOpFile, &moveOps, &pause,
		false)

	// Download
	setDownloadFlags(addCmd, &retries, &useGlobalCookies,
		&ytdlpOutputExt, &fromDate, &toDate,
		&cookieSource, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	setMetarrFlags(addCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &filenameDateTag,
		&minFreeMem, &outDir, &renameStyle,
		&urlOutDirs, &fileSfxReplace, &metaOps)

	// Login credentials
	setAuthFlags(addCmd, &username, &password, &loginURL, &authDetails)

	// Transcoding
	setTranscodeFlags(addCmd, &transcodeGPU, &gpuDir,
		&transcodeVideoFilter, &codec, &audioCodec,
		&transcodeQuality)

	// Notification URL
	setNotifyFlags(addCmd, &notification)

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
	setPrimaryChannelFlags(delCmd, &name, &urls, &id)

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
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				logging.I("Entry for channel with %s %q does not exist in the database", key, val)
				return nil
			}
			if err != nil {
				return err
			}

			// Display settings and return
			displaySettings(cs, c)
			return nil
		},
	}
	// Primary channel elements
	setPrimaryChannelFlags(listCmd, &name, nil, &channelID)
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
			chans, hasRows, err := cs.FetchAllChannels()
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
		Use:           "crawl",
		Short:         "Crawl a channel for new URLs.",
		Long:          "Initiate a crawl for new URLs of a channel that have not yet been downloaded.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				// Print flag/validation errors since they're user-facing
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			// Retrieve channel model
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				err := fmt.Errorf("no channel model in database with %s %q", key, val)
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return err
			}

			// Don't print errors from CrawlChannel, already handled in function
			if err := cs.CrawlChannel(key, val, c, s, ctx); err != nil {
				return err
			}

			// Success
			logging.S(0, "Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(crawlCmd, &name, nil, &id)
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
				if err := loadConfigFile(configFile); err != nil {
					return err
				}
			}

			// Check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Fetch model from database
			c, hasRows, err := cs.FetchChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("no channel model in database with %s %q", key, val)
			}
			if err != nil {
				return err
			}

			// Load config file location if existent and one wasn't hardcoded into terminal
			if configFile == "" && c.ChanSettings.ChannelConfigFile != "" {
				if err := loadConfigFile(configFile); err != nil {
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
				authDetails, err := parseAuthDetails(username, password, loginURL, authDetails, cURLs, deleteAuth)
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
	setPrimaryChannelFlags(updateSettingsCmd, &name, &urls, &id)

	// Files/dirs
	setFileDirFlags(updateSettingsCmd, &configFile, &jDir, &vDir)

	// Program related
	setProgramRelatedFlags(updateSettingsCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader, &moveOpsFile,
		&moveOps, &pause, true)

	// Download
	setDownloadFlags(updateSettingsCmd, &retries, &useGlobalCookies,
		&ytdlpOutExt, &fromDate, &toDate,
		&cookieSource, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	setMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &filenameDateTag,
		&minFreeMem, &outDir, &renameStyle,
		&urlOutDirs, &fileSfxReplace, &metaOps)

	// Transcoding
	setTranscodeFlags(updateSettingsCmd, &useGPU, &gpuDir,
		&transcodeVideoFilter, &codec, &audioCodec,
		&transcodeQuality)

	// Auth
	setAuthFlags(updateSettingsCmd, &username, &password,
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
	setPrimaryChannelFlags(updateRowCmd, &name, nil, &id)

	updateRowCmd.Flags().StringVarP(&col, "column-name", "c", "", "The name of the column in the table (e.g. video_directory)")
	updateRowCmd.Flags().StringVarP(&newVal, "value", "v", "", "The value to set in the column (e.g. /my-directory)")
	return updateRowCmd
}

// displaySettings displays fields relevant to a channel.
func displaySettings(cs interfaces.ChannelStore, c *models.Channel) {
	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		logging.E("Unable to fetch notification URLs for channel %q: %v", c.Name, err)
	}

	s := c.ChanSettings
	m := c.ChanMetarrArgs

	fmt.Printf("\n\n\n%s[ Channel: %q ]%s\n", consts.ColorGreen, c.Name, consts.ColorReset)

	cURLs := c.GetURLs()

	// Channel basic info
	fmt.Printf("\n%sBasic Info:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("ID: %d\n", c.ID)
	fmt.Printf("Name: %s\n", c.Name)
	fmt.Printf("URLs: %+v\n", cURLs)
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
	fmt.Printf("From Date: %q\n", parsing.HyphenateYyyyMmDd(s.FromDate))
	fmt.Printf("To Date: %q\n", parsing.HyphenateYyyyMmDd(s.ToDate))
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
	var nURLs []string
	for _, n := range notifyURLs {
		newNUrl := n.NotifyURL
		if n.ChannelURL != "" {
			newNUrl = n.ChannelURL + "|" + n.NotifyURL
		}
		nURLs = append(nURLs, newNUrl)
	}
	fmt.Printf("\n%sNotify URLs:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Notification URLs: %v\n", nURLs)

	fmt.Printf("\n%sAuthentication:%s\n", consts.ColorCyan, consts.ColorReset)

	// Auth details
	gotAuthModels := false
	for _, cu := range c.URLModels {
		if cu.Username != "" || cu.LoginURL != "" || cu.Password != "" {
			fmt.Printf("\nChannel URL: %s, Username: %s, Password: %s, Login URL: %s",
				cu.URL,
				cu.Username,
				auth.StarPassword(cu.Password),
				cu.LoginURL)

			if !gotAuthModels {
				gotAuthModels = true
			}
		}
	}
	if !gotAuthModels {
		fmt.Printf("[]\n")
	}

}

// UpdateFromConfig loads in config file data.
func UpdateFromConfig(cs interfaces.ChannelStore, c *models.Channel) {

	if c.ChanSettings.ChannelConfigFile != "" && !c.UpdatedFromConfig {
		if err := updateChannelFromConfig(cs, c); err != nil {
			logging.E("failed to update from config file %q: %v", c.ChanSettings.ChannelConfigFile, err)
		}

		c.UpdatedFromConfig = true
	}
}

// ******************************** Private ********************************

// updateChannelFromConfig updates the channel settings from a config file if it exists.
func updateChannelFromConfig(cs interfaces.ChannelStore, c *models.Channel) error {
	cfgFile := c.ChanSettings.ChannelConfigFile
	if cfgFile == "" {
		logging.D(2, "No config file path, nothing to apply")
		return nil
	}

	logging.I("Updating channel from config file %q...", cfgFile)
	if _, err := validation.ValidateFile(cfgFile, false); err != nil {
		return err
	}

	if err := loadConfigFile(cfgFile); err != nil {
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
		if c.ChanSettings == nil {
			return fmt.Errorf("c.ChanSettings is nil")
		}
		*s = *c.ChanSettings
		return nil
	})
	if err != nil {
		return err
	}

	_, err = cs.UpdateChannelMetarrArgsJSON(key, val, func(m *models.MetarrArgs) error {
		if c.ChanMetarrArgs == nil {
			return fmt.Errorf("c.ChanMetarrArgs is nil")
		}
		*m = *c.ChanMetarrArgs
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
	if c.ChanSettings == nil {
		c.ChanSettings = &models.ChannelSettings{}
	}

	// Channel config file location
	if v, ok := getConfigValue[string](keys.ChannelConfigFile); ok {
		if _, err = validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.ChanSettings.ChannelConfigFile = v
	}

	// Concurrency limit
	if v, ok := getConfigValue[int](keys.ConcurrencyLimitInput); ok {
		c.ChanSettings.Concurrency = validation.ValidateConcurrencyLimit(v)
	}

	// Cookie source
	if v, ok := getConfigValue[string](keys.CookieSource); ok {
		c.ChanSettings.CookieSource = v // No check for this currently! (cookies-from-browser)
	}

	// Crawl frequency
	if v, ok := getConfigValue[int](keys.CrawlFreq); ok {
		c.ChanSettings.CrawlFreq = v
	}

	// Download retries
	if v, ok := getConfigValue[int](keys.DLRetries); ok {
		c.ChanSettings.Retries = v
	}

	// External downloader
	if v, ok := getConfigValue[string](keys.ExternalDownloader); ok {
		c.ChanSettings.ExternalDownloader = v // No checks for this yet.
	}

	// External downloader arguments
	if v, ok := getConfigValue[string](keys.ExternalDownloaderArgs); ok {
		c.ChanSettings.ExternalDownloaderArgs = v // No checks for this yet.
	}

	// Filter ops file
	if v, ok := getConfigValue[string](keys.FilterOpsFile); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.ChanSettings.FilterFile = v
	}

	// From date
	if v, ok := getConfigValue[string](keys.FromDate); ok {
		if c.ChanSettings.FromDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// JSON directory
	if v, ok := getConfigValue[string](keys.JSONDir); ok {
		if _, err = validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.ChanSettings.JSONDir = v
	}

	// Max filesize to download
	if v, ok := getConfigValue[string](keys.MaxFilesize); ok {
		c.ChanSettings.MaxFilesize = v
	}

	// Move ops file
	if v, ok := getConfigValue[string](keys.MoveOpsFile); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.ChanSettings.MoveOpFile = v
	}

	// Pause channel
	if v, ok := getConfigValue[bool](keys.Pause); ok {
		c.ChanSettings.Paused = v
	}

	// To date
	if v, ok := getConfigValue[string](keys.ToDate); ok {
		if c.ChanSettings.ToDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}

	// Use global cookies?
	if v, ok := getConfigValue[bool](keys.UseGlobalCookies); ok {
		c.ChanSettings.UseGlobalCookies = v
	}

	// Video directory
	if v, ok := getConfigValue[string](keys.VideoDir); ok {
		if _, err = validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.ChanSettings.VideoDir = v
	}

	// YTDLP output format
	if v, ok := getConfigValue[string](keys.YtdlpOutputExt); ok {
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return err
		}
		c.ChanSettings.YtdlpOutputExt = v
	}
	return nil
}

// applyConfigMetarrSettings applies the Metarr settings to the model and saves to database.
func applyConfigMetarrSettings(c *models.Channel) (err error) {
	// Initialize MetarrArgs model if nil
	if c.ChanMetarrArgs == nil {
		c.ChanMetarrArgs = &models.MetarrArgs{}
	}

	var (
		gpuDirGot, gpuGot string
		videoCodecGot     string
	)

	// Metarr output extension
	if v, ok := getConfigValue[string](keys.MExt); ok {
		if _, err := validation.ValidateOutputFiletype(c.ChanSettings.ChannelConfigFile); err != nil {
			return fmt.Errorf("metarr output filetype %q in config file %q is invalid", v, c.ChanSettings.ChannelConfigFile)
		}
		c.ChanMetarrArgs.Ext = v
	}

	// Filename suffix replacements
	if v, ok := getConfigValue[[]string](keys.MFilenameReplaceSuffix); ok {
		c.ChanMetarrArgs.FilenameReplaceSfx, err = validation.ValidateFilenameSuffixReplace(v)
		if err != nil {
			return err
		}
	}

	// Rename style
	if v, ok := getConfigValue[string](keys.MRenameStyle); ok {
		if err := validation.ValidateRenameFlag(v); err != nil {
			return err
		}
		c.ChanMetarrArgs.RenameStyle = v
	}

	// Extra FFmpeg arguments
	if v, ok := getConfigValue[string](keys.MExtraFFmpegArgs); ok {
		c.ChanMetarrArgs.ExtraFFmpegArgs = v
	}

	// Filename date tag
	if v, ok := getConfigValue[string](keys.MFilenameDateTag); ok {
		if ok := validation.ValidateDateFormat(v); !ok {
			return fmt.Errorf("date format %q in config file %q is invalid", v, c.ChanSettings.ChannelConfigFile)
		}
		c.ChanMetarrArgs.FilenameDateTag = v
	}

	// Meta ops
	if v, ok := getConfigValue[[]string](keys.MMetaOps); ok {
		c.ChanMetarrArgs.MetaOps, err = validation.ValidateMetaOps(v)
		if err != nil {
			return err
		}
	}

	// Default output directory
	if v, ok := getConfigValue[string](keys.MOutputDir); ok {
		if _, err := validation.ValidateDirectory(v, false); err != nil {
			return err
		}
		c.ChanMetarrArgs.OutputDir = v
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
			c.ChanMetarrArgs.URLOutputDirs = nil
		} else {
			c.ChanMetarrArgs.URLOutputDirs = valid
		}
	}

	// Metarr concurrency
	if v, ok := getConfigValue[int](keys.MConcurrency); ok {
		c.ChanMetarrArgs.Concurrency = validation.ValidateConcurrencyLimit(v)
	}

	// Metarr max CPU
	if v, ok := getConfigValue[float64](keys.MMaxCPU); ok {
		c.ChanMetarrArgs.MaxCPU = v // Handled in Metarr
	}

	// Metarr minimum memory to reserve
	if v, ok := getConfigValue[string](keys.MMinFreeMem); ok {
		c.ChanMetarrArgs.MinFreeMem = v // Handled in Metarr
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
		c.ChanMetarrArgs.TranscodeVideoFilter, err = validation.ValidateTranscodeVideoFilter(v)
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
		if c.ChanMetarrArgs.TranscodeAudioCodec, err = validation.ValidateTranscodeAudioCodec(v); err != nil {
			return err
		}
	}

	// Metarr transcode quality
	if v, ok := getConfigValue[string](keys.MTranscodeQuality); ok {
		if c.ChanMetarrArgs.TranscodeQuality, err = validation.ValidateTranscodeQuality(v); err != nil {
			return err
		}
	}

	// Transcode GPU validation
	if gpuGot != "" || gpuDirGot != "" {
		c.ChanMetarrArgs.UseGPU, c.ChanMetarrArgs.GPUDir, err = validation.ValidateGPU(gpuGot, gpuDirGot)
		if err != nil {
			return err
		}
	}

	// Validate video codec against transcode GPU
	if c.ChanMetarrArgs.TranscodeCodec, err = validation.ValidateTranscodeCodec(videoCodecGot, c.ChanMetarrArgs.UseGPU); err != nil {
		return err
	}

	return nil
}

type cobraMetarrArgs struct {
	filenameReplaceSfx   []string
	renameStyle          string
	extraFFmpegArgs      string
	filenameDateTag      string
	metarrExt            string
	metaOps              []string
	outputDir            string
	urlOutputDirs        []string
	concurrency          int
	maxCPU               float64
	minFreeMem           string
	useGPU               string
	gpuDir               string
	transcodeCodec       string
	transcodeAudioCodec  string
	transcodeQuality     string
	transcodeVideoFilter string
}

// getMetarrArgFns gets and collects the Metarr argument functions for channel updates.
func getMetarrArgFns(cmd *cobra.Command, c cobraMetarrArgs) (fns []func(*models.MetarrArgs) error, err error) {

	f := cmd.Flags()

	// Min free memory
	if f.Changed(keys.MMinFreeMem) {
		if c.minFreeMem != "" {
			if err := validation.ValidateMinFreeMem(c.minFreeMem); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MinFreeMem = c.minFreeMem
			return nil
		})
	}

	// Metarr final video output extension (e.g. 'mp4')
	if f.Changed(keys.MExt) {
		if c.metarrExt != "" {
			_, err := validation.ValidateOutputFiletype(c.metarrExt)
			if err != nil {
				return nil, err
			}
		}
		c.metarrExt = strings.ToLower(c.metarrExt)
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.Ext = c.metarrExt
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

	// Filename date tag
	if f.Changed(keys.MFilenameDateTag) {
		if c.filenameDateTag != "" {
			if !validation.ValidateDateFormat(c.filenameDateTag) {
				return nil, errors.New("invalid Metarr filename date tag format")
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameDateTag = c.filenameDateTag
			return nil
		})
	}

	// Filename replace suffix (e.g. '_1' to '')
	if f.Changed(keys.MFilenameReplaceSuffix) {
		valid := c.filenameReplaceSfx

		if len(c.filenameReplaceSfx) > 0 {
			valid, err = validation.ValidateFilenameSuffixReplace(c.filenameReplaceSfx)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameReplaceSfx = valid
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
		valid := c.metaOps

		if len(c.metaOps) > 0 {
			valid, err = validation.ValidateMetaOps(c.metaOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOps = valid
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
			m.UseGPU = validGPU
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
			m.GPUDir = c.gpuDir
			return nil
		})
	}

	// Video codec
	if f.Changed(keys.TranscodeCodec) {
		validTranscodeCodec := c.transcodeCodec

		if c.transcodeCodec != "" {
			validTranscodeCodec, err = validation.ValidateTranscodeCodec(c.transcodeCodec, c.useGPU)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeCodec = validTranscodeCodec
			return nil
		})
	}

	// Audio codec
	if f.Changed(keys.TranscodeAudioCodec) {
		validTranscodeAudioCodec := c.transcodeAudioCodec

		if c.transcodeAudioCodec != "" {
			validTranscodeAudioCodec, err = validation.ValidateTranscodeAudioCodec(c.transcodeAudioCodec)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeAudioCodec = validTranscodeAudioCodec
			return nil
		})
	}

	// Transcode quality
	if f.Changed(keys.TranscodeQuality) {
		validTranscodeQuality := c.transcodeQuality

		if c.transcodeQuality != "" {
			validTranscodeQuality, err = validation.ValidateTranscodeQuality(c.transcodeQuality)
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
	channelConfigFile      string
	concurrency            int
	cookieSource           string
	crawlFreq              int
	externalDownloader     string
	externalDownloaderArgs string
	filters                []string
	filterFile             string
	fromDate               string
	jsonDir                string
	maxFilesize            string
	moveOps                []string
	moveOpsFile            string
	paused                 bool
	retries                int
	toDate                 string
	videoDir               string
	useGlobalCookies       bool
	ytdlpOutputExt         string
}

// getSettingsArgsFns creates the functions to send in to update the database with new values.
func getSettingsArgFns(cmd *cobra.Command, c chanSettings) (fns []func(m *models.ChannelSettings) error, err error) {

	f := cmd.Flags()

	// Concurrency
	if f.Changed(keys.Concurrency) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Concurrency = max(c.concurrency, 1)
			return nil
		})
	}

	// Channel config file location
	if f.Changed(keys.ChannelConfigFile) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ChannelConfigFile = c.channelConfigFile
			return nil
		})
	}

	// Cookie source
	if f.Changed(keys.CookieSource) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.CookieSource = c.cookieSource
			return nil
		})
	}

	// Crawl frequency
	if f.Changed(keys.CrawlFreq) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.CrawlFreq = max(c.crawlFreq, 0)
			return nil
		})
	}

	// Download retry amount
	if f.Changed(keys.DLRetries) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Retries = c.retries
			return nil
		})
	}

	// External downloader
	if f.Changed(keys.ExternalDownloader) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ExternalDownloader = c.externalDownloader
			return nil
		})
	}

	// External downloader arguments
	if f.Changed(keys.ExternalDownloaderArgs) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ExternalDownloaderArgs = c.externalDownloaderArgs
			return nil
		})
	}

	// Filter ops ('field:contains:frogs:must')
	if f.Changed(keys.FilterOpsInput) {
		dlFilters, err := validation.ValidateFilterOps(c.filters)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Filters = dlFilters
			return nil
		})
	}

	// Move ops ('field:value:output directory')
	if f.Changed(keys.MoveOps) {
		moveOperations, err := validation.ValidateMoveOps(c.moveOps)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.MoveOps = moveOperations
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
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.FromDate = validFromDate
			return nil
		})
	}

	// JSON directory
	if f.Changed(keys.JSONDir) {
		if c.jsonDir == "" {
			if c.videoDir != "" {
				c.jsonDir = c.videoDir
			} else {
				return nil, fmt.Errorf("json directory cannot be empty. Attempted to default to video directory but video directory is also empty")
			}
		}
		if _, err = validation.ValidateDirectory(c.jsonDir, false); err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
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
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.MaxFilesize = c.maxFilesize
			return nil
		})
	}

	// Pause channel
	if f.Changed(keys.Pause) {
		fns = append(fns, func(s *models.ChannelSettings) error {
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

		fns = append(fns, func(s *models.ChannelSettings) error {
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
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.VideoDir = c.videoDir
			return nil
		})
	}

	// Use global cookies
	if f.Changed(keys.UseGlobalCookies) {
		fns = append(fns, func(s *models.ChannelSettings) error {
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
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.YtdlpOutputExt = c.ytdlpOutputExt
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
	case "name", "video_directory", "json_directory":
		if val == "" {
			return fmt.Errorf("cannot set %s blank, please use the 'delete' function if you want to remove this channel entirely", col)
		}
	default:
		return errors.New("cannot set a custom value for internal DB elements")
	}
	return nil
}

// parseAuthDetails parses authorization details for channel URLs.
//
// Authentication details should be provided as JSON strings:
//   - Single channel: '{"username":"user","password":"pass","login_url":"https://example.com"}'
//   - Multiple channels: '{"channel_url":"https://ch1.com","username":"user","password":"pass","login_url":"https://example.com"}'
//
// Examples:
//
//	'{"username":"john","password":"p@ss,word!","login_url":"https://login.example.com"}'
//	'{"channel_url":"https://ch1.com","username":"user1","password":"pass1","login_url":"https://login1.com"}'
func parseAuthDetails(u, p, l string, a, cURLs []string, deleteAll bool) (map[string]*models.ChannelAccessDetails, error) {
	authMap := make(map[string]*models.ChannelAccessDetails, len(cURLs))

	// Handle delete all operation
	if deleteAll {
		for _, cURL := range cURLs {
			authMap[cURL] = &models.ChannelAccessDetails{
				Username: "",
				Password: "",
				LoginURL: "",
			}
		}
		logging.I("Deleted authentication details for channel URLs: %v", cURLs)
		return authMap, nil
	}

	// Check if there are any auth details to process
	if len(a) == 0 && (u == "" || l == "") {
		logging.D(3, "No authorization details to parse...")
		return nil, nil
	}

	// Parse JSON auth strings
	if len(a) > 0 {
		return parseJSONAuth(a, cURLs)
	}

	// Fallback: individual flags (u, p, l) for all channels
	for _, cURL := range cURLs {
		authMap[cURL] = &models.ChannelAccessDetails{
			Username: u,
			Password: p,
			LoginURL: l,
		}
	}
	return authMap, nil
}

// authDetails represents the JSON structure for authentication details.
type authDetails struct {
	ChannelURL string `json:"channel_url,omitempty"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	LoginURL   string `json:"login_url"`
}

// parseJSONAuth parses JSON-formatted authentication strings.
func parseJSONAuth(authStrings []string, cURLs []string) (map[string]*models.ChannelAccessDetails, error) {
	authMap := make(map[string]*models.ChannelAccessDetails, len(authStrings))

	for i, authStr := range authStrings {
		var auth authDetails

		// Parse JSON
		if err := json.Unmarshal([]byte(authStr), &auth); err != nil {
			return nil, fmt.Errorf("invalid JSON in authentication string %d: %w\nExpected format: '{\"username\":\"user\",\"password\":\"pass\",\"login_url\":\"https://example.com\"}'", i+1, err)
		}

		// Validate required fields
		if auth.Username == "" {
			return nil, fmt.Errorf("authentication string %d: username is required", i+1)
		}
		if auth.LoginURL == "" {
			return nil, fmt.Errorf("authentication string %d: login_url is required", i+1)
		}

		// Determine which channel URL to use
		var channelURL string
		if auth.ChannelURL != "" {
			// Explicit channel URL provided
			channelURL = auth.ChannelURL

			// Validate that this channel URL exists
			if !slices.Contains(cURLs, channelURL) {
				return nil, fmt.Errorf("authentication string %d: channel_url %q does not match any of the provided channel URLs: %v", i+1, channelURL, cURLs)
			}
		} else {
			// No explicit channel URL - use single channel if available
			if len(cURLs) != 1 {
				return nil, fmt.Errorf("authentication string %d: channel_url field is required when there are multiple channel URLs (%d provided)", i+1, len(cURLs))
			}
			channelURL = cURLs[0]
		}

		// Check for duplicate channel URL in auth strings
		if _, exists := authMap[channelURL]; exists {
			return nil, fmt.Errorf("duplicate authentication entry for channel URL: %q", channelURL)
		}

		authMap[channelURL] = &models.ChannelAccessDetails{
			Username: auth.Username,
			Password: auth.Password,
			LoginURL: auth.LoginURL,
		}
	}

	// For single channel case with explicit channel_url, verify it matches
	if len(cURLs) == 1 && len(authMap) == 1 {
		for providedURL := range authMap {
			if providedURL != cURLs[0] {
				return nil, fmt.Errorf("failsafe for user error: authentication specified for channel URL %q but actual channel URL is %q", providedURL, cURLs[0])
			}
		}
	}

	return authMap, nil
}

// getConfigValue normalizes and retrieves values from the config file.
// Supports both kebab-case and snake_case keys.
func getConfigValue[T any](key string) (T, bool) {
	var zero T

	// Try original key first
	if viper.IsSet(key) {
		if val, ok := convertConfigValue[T](viper.Get(key)); ok {
			return val, true
		}
	}

	// Try snake_case version
	snakeKey := strings.ReplaceAll(key, "-", "_")
	if snakeKey != key && viper.IsSet(snakeKey) {
		if val, ok := convertConfigValue[T](viper.Get(snakeKey)); ok {
			return val, true
		}
	}

	// Try kebab-case version
	kebabKey := strings.ReplaceAll(key, "_", "-")
	if kebabKey != key && viper.IsSet(kebabKey) {
		if val, ok := convertConfigValue[T](viper.Get(kebabKey)); ok {
			return val, true
		}
	}

	return zero, false
}

// convertConfigValue handles config entry conversions safely.
func convertConfigValue[T any](v any) (T, bool) {
	var zero T

	// Direct type match
	if val, ok := v.(T); ok {
		return val, true
	}

	// Let Viper handle the conversion - it's already good at this
	switch any(zero).(type) {
	case string:
		if s, ok := v.(string); ok {
			return any(s).(T), true
		}
		return any(fmt.Sprintf("%v", v)).(T), true

	case int:
		if i, ok := v.(int); ok {
			return any(i).(T), true
		}
		if i64, ok := v.(int64); ok {
			return any(int(i64)).(T), true
		}
		if f, ok := v.(float64); ok {
			return any(int(f)).(T), true
		}

	case float64:
		if f, ok := v.(float64); ok {
			return any(f).(T), true
		}
		if i, ok := v.(int); ok {
			return any(float64(i)).(T), true
		}

	case bool:
		if b, ok := v.(bool); ok {
			return any(b).(T), true
		}

	case []string:
		if slice, ok := v.([]string); ok {
			return any(slice).(T), true
		}
		if slice, ok := v.([]any); ok {
			strSlice := make([]string, len(slice))
			for i, item := range slice {
				strSlice[i] = fmt.Sprintf("%v", item)
			}
			return any(strSlice).(T), true
		}
	}

	return zero, false
}
