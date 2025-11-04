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
	"tubarr/internal/app"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/spf13/cobra"
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
		channelName                  string
		username, password, loginURL string
		authDetails                  []string
		channelID                    int
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
			c, hasRows, err := cs.GetChannelModel(key, val)
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
			logging.S("Channel with %s %q set authentication details", key, val)
			return nil
		},
	}
	setPrimaryChannelFlags(addAuthCmd, &channelName, nil, &channelID)
	setAuthFlags(addAuthCmd, &username, &password, &loginURL, &authDetails)
	return addAuthCmd
}

// deleteURLs deletes a list of URLs inputted by the user.
func deleteVideoURLs(cs contracts.ChannelStore) *cobra.Command {

	var (
		cFile, channelName string
		channelID          int
		urls               []string
	)

	deleteURLsCmd := &cobra.Command{
		Use:   "delete-video-urls",
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
func downloadVideoURLs(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
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
			c, hasRows, err := cs.GetChannelModel(key, val)
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
			logging.D(1, "Retrieved channel %q with URLs: %v", c.Name, cURLs)

			// Download URLs - errors already logged, DON'T print here
			if err := app.DownloadVideosToChannel(ctx, s, cs, c, videoURLs); err != nil {
				return err
			}

			// Success
			logging.S("Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	setPrimaryChannelFlags(manualURLCmd, &channelName, nil, &channelID)
	manualURLCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to your given channel")
	manualURLCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to download for a given channel")

	return manualURLCmd
}

// deleteNotifyURLs deletes notification URLs from a channel.
func deleteNotifyURLs(cs contracts.ChannelStore) *cobra.Command {
	var (
		channelName             string
		channelID               int
		notifyURLs, notifyNames []string
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
			logging.S("Deleted notify URLs for channel with %s %q: %v", key, val, notifyURLs)
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
func addNotifyURLs(cs contracts.ChannelStore) *cobra.Command {
	var (
		channelName  string
		channelID    int
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

			// Validate and add notification URLs
			validPairs, err := validation.ValidateNotificationStrings(notification)
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
			logging.S("Added notify URLs for channel with %s %q: %v", key, val, pairSlice)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(addNotifyCmd, &channelName, nil, &channelID)
	setNotifyFlags(addNotifyCmd, &notification)

	return addNotifyCmd
}

// addVideoURLToIgnore adds a user inputted URL to ignore from crawls.
func addVideoURLToIgnore(cs contracts.ChannelStore) *cobra.Command {
	var (
		name, ignoreURL string
		id              int
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
			logging.S("Ignoring URL %q for channel with %s %q", ignoreURL, key, val)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(ignoreURLCmd, &name, nil, nil)
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-video-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// ignoreCrawl crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func ignoreCrawl(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		name string
		id   int
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:          "ignore-crawl",
		Short:        "Crawl a channel for URLs to ignore.",
		Long:         "Crawls the current state of a channel page and adds all video URLs to ignore.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			c, hasRows, err := cs.GetChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("no rows in the database for channel with %s %q", key, val)
			}
			if err != nil {
				return err
			}

			// Initialize URL list
			c.URLModels, err = cs.GetChannelURLModels(c)
			if err != nil {
				return fmt.Errorf("failed to fetch URL models for channel: %w", err)
			}

			cURLs := c.GetURLs()
			logging.D(1, "Retrieved channel %q with URLs: %v", c.Name, cURLs)

			// Run an ignore crawl
			if err := app.CrawlChannelIgnore(ctx, s, c); err != nil {
				return err
			}

			// Success
			logging.S("Completed ignore crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(ignoreCrawlCmd, &name, nil, nil)

	return ignoreCrawlCmd
}

// pauseChannelCmd pauses a channel from downloads in upcoming crawls.
func pauseChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		name string
		id   int
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
			c, hasRows, err := cs.GetChannelModel(key, val)
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
			logging.S("Paused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(pauseCmd, &name, nil, &id)

	return pauseCmd
}

// unpauseChannelCmd unpauses a channel to allow for downloads in upcoming crawls.
func unpauseChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		name string
		id   int
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
			c, hasRows, err := cs.GetChannelModel(key, val)
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
			logging.S("Unpaused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(unPauseCmd, &name, nil, &id)

	return unPauseCmd
}

// unblockChannelCmd unblocks channels which were locked (usually due to bot activity detection).
func unblockChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		name string
		id   int
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
			c, hasRows, err := cs.GetChannelModel(key, val)
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
			logging.S("Unblocked channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(unblockCmd, &name, nil, &id)

	return unblockCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		urls []string
		name, vDir, mDir, outDir, cookieSource,
		externalDownloader, externalDownloaderArgs, maxFilesize, renameStyle, minFreeMem, metarrExt string
		urlOutDirs                   []string
		username, password, loginURL string
		authDetails                  []string
		notification                 []string
		fromDate, toDate             string

		dlFilters, moveOps, metaOps, filenameOps, filteredMetaOps, filteredFilenameOps []string

		configFile, dlFilterFile, moveOpFile, metaOpsFile, filteredMetaOpsFile, filenameOpsFile, filteredFilenameOpsFile string

		crawlFreq, concurrency, metarrConcurrency, retries                                              int
		maxCPU                                                                                          float64
		transcodeGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter, ytdlpOutputExt string
		pause, ignoreRun, useGlobalCookies                                                              bool
		extraYTDLPVideoArgs, extraYTDLPMetaArgs, extraFFmpegArgs                                        string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, settings, etc.",
		RunE: func(_ *cobra.Command, _ []string) error {
			var err error
			// Load in config file
			if configFile != "" {
				if err := file.LoadConfigFile(configFile); err != nil {
					return err
				}
			}

			// Got valid items?
			if vDir == "" || len(urls) == 0 || name == "" {
				return errors.New("new channels require a video directory, name, and at least one channel URL")
			}

			if mDir == "" {
				mDir = vDir
			}

			if _, err := validation.ValidateDirectory(vDir, true); err != nil {
				return err
			}

			// Validate filter operations
			var dlFilterModels []models.DLFilters
			if dlFilterModels, err = validation.ValidateFilterOps(dlFilters); err != nil {
				return err
			}

			// Validate move operation filters
			moveOpsModels, err := validation.ValidateMoveOps(moveOps)
			if err != nil {
				return err
			}

			// Meta operations
			var metaOpsModels = []models.MetaOps{}
			if len(metaOps) > 0 {
				if metaOpsModels, err = validation.ValidateMetaOps(metaOps); err != nil {
					return err
				}
			}

			var filenameOpsModels = []models.FilenameOps{}
			if len(filenameOps) > 0 {
				if filenameOpsModels, err = validation.ValidateFilenameOps(filenameOps); err != nil {
					return err
				}
			}

			// Filtered meta operations
			var filteredMetaOpsModels = []models.FilteredMetaOps{}
			if len(filteredMetaOps) > 0 {
				if filteredMetaOpsModels, err = validation.ValidateFilteredMetaOps(filteredMetaOps); err != nil {
					return err
				}
			}

			// Filtered filename operations
			var filteredFilenameOpsModels = []models.FilteredFilenameOps{}
			if len(filteredFilenameOps) > 0 {
				if filteredFilenameOpsModels, err = validation.ValidateFilteredFilenameOps(filteredFilenameOps); err != nil {
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
				ChanSettings: &models.Settings{
					ChannelConfigFile:      configFile,
					Concurrency:            concurrency,
					CookieSource:           cookieSource,
					CrawlFreq:              crawlFreq,
					ExternalDownloader:     externalDownloader,
					ExternalDownloaderArgs: externalDownloaderArgs,
					Filters:                dlFilterModels,
					FilterFile:             dlFilterFile,
					FromDate:               fromDate,
					MetaDir:                mDir,
					MaxFilesize:            maxFilesize,
					MoveOps:                moveOpsModels,
					MoveOpFile:             moveOpFile,
					Paused:                 pause,
					Retries:                retries,
					ToDate:                 toDate,
					UseGlobalCookies:       useGlobalCookies,
					VideoDir:               vDir,
					YtdlpOutputExt:         ytdlpOutputExt,
					ExtraYTDLPVideoArgs:    extraYTDLPVideoArgs,
					ExtraYTDLPMetaArgs:     extraYTDLPMetaArgs,
				},

				ChanMetarrArgs: &models.MetarrArgs{
					OutputExt:       metarrExt,
					ExtraFFmpegArgs: extraFFmpegArgs,

					MetaOps:             metaOpsModels,
					MetaOpsFile:         metaOpsFile,
					FilteredMetaOps:     filteredMetaOpsModels,
					FilteredMetaOpsFile: filteredMetaOpsFile,

					FilenameOps:             filenameOpsModels,
					FilenameOpsFile:         filenameOpsFile,
					FilteredFilenameOps:     filteredFilenameOpsModels,
					FilteredFilenameOpsFile: filteredFilenameOpsFile,

					RenameStyle:          renameStyle,
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

			// Add channel to database and retrieve ID
			channelID, err := cs.AddChannel(c)
			if err != nil {
				return err
			}
			// Set the ID on the model
			c.ID = channelID

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
				if err := app.CrawlChannelIgnore(ctx, s, c); err != nil {
					logging.E("Failed to complete ignore crawl run: %v", err)
				}
			}

			// Success
			logging.S("Completed addition of channel %q to Tubarr", name)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(addCmd, &name, &urls, nil)

	// Files/dirs
	setFileDirFlags(addCmd, &configFile, &mDir, &vDir)

	// Program related
	setProgramRelatedFlags(addCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader,
		&moveOpFile, &moveOps, &pause)

	// Download
	setDownloadFlags(addCmd, &retries, &useGlobalCookies,
		&ytdlpOutputExt, &fromDate, &toDate,
		&cookieSource, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	setMetarrFlags(addCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &minFreeMem,
		&outDir, &renameStyle, &metaOpsFile,
		&filteredMetaOpsFile, &filenameOpsFile, &filteredFilenameOpsFile,
		&urlOutDirs, &filenameOps, &filteredFilenameOps,
		&metaOps, &filteredMetaOps)

	// Login credentials
	setAuthFlags(addCmd, &username, &password, &loginURL, &authDetails)

	// Transcoding
	setTranscodeFlags(addCmd, &transcodeGPU, &gpuDir,
		&transcodeVideoFilter, &codec, &audioCodec,
		&transcodeQuality)

	// YTDLPFlags
	setCustomYDLPArgFlags(addCmd, &extraYTDLPVideoArgs, &extraYTDLPMetaArgs)

	// Notification URL
	setNotifyFlags(addCmd, &notification)

	// Pause or ignore run
	addCmd.Flags().BoolVar(&pause, "pause", false, "Paused channels won't crawl videos on a normal program run")
	addCmd.Flags().BoolVar(&ignoreRun, "ignore-run", false, "Run an 'ignore crawl' first so only new videos are downloaded (rather than the entire channel backlog)")

	return addCmd
}

// deleteChannelCmd deletes a channel from the database.
func deleteChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		urls []string
		name string
		id   int
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
			logging.S("Successfully deleted channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(delCmd, &name, &urls, &id)

	return delCmd
}

// listAllChannel returns details about a single channel in the database.
func listChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		name      string
		channelID int
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
			c, hasRows, err := cs.GetChannelModel(key, val)
			if !hasRows {
				logging.I("Entry for channel with %s %q does not exist in the database", key, val)
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
	setPrimaryChannelFlags(listCmd, &name, nil, &channelID)
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
			chans, hasRows, err := cs.GetAllChannels()
			if !hasRows {
				logging.I("No entries in the database")
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
		name string
		id   int
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
			c, hasRows, err := cs.GetChannelModel(key, val)
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
			if err := app.CrawlChannel(ctx, s, cs, c); err != nil {
				return err
			}

			// Success
			logging.S("Completed crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(crawlCmd, &name, nil, &id)
	return crawlCmd
}

// updateChannelSettingsCmd updates channel settings.
func updateChannelSettingsCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		urls                                                                                                              []string
		id, concurrency, crawlFreq, metarrConcurrency, retries                                                            int
		maxCPU                                                                                                            float64
		vDir, jDir, outDir                                                                                                string
		urlOutDirs                                                                                                        []string
		name, cookieSource                                                                                                string
		minFreeMem, renameStyle, metarrExt                                                                                string
		maxFilesize, externalDownloader, externalDownloaderArgs                                                           string
		username, password, loginURL                                                                                      string
		authDetails                                                                                                       []string
		dlFilters, moveOps, metaOps, filteredMetaOps, filenameOps, filteredFilenameOps                                    []string
		configFile, dlFilterFile, moveOpsFile, metaOpsFile, filteredMetaOpsFile, filenameOpsFile, filteredFilenameOpsFile string
		useGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter                                         string
		fromDate, toDate                                                                                                  string
		ytdlpOutExt                                                                                                       string
		useGlobalCookies, pause, deleteAuth                                                                               bool
		extraYTDLPVideoArgs, extraYTDLPMetaArgs, extraFFmpegArgs                                                          string
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-settings",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		RunE: func(cmd *cobra.Command, _ []string) error {

			// Load in config
			if configFile != "" {
				if err := file.LoadConfigFile(configFile); err != nil {
					return err
				}
			}

			// Check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Fetch model from database
			c, hasRows, err := cs.GetChannelModel(key, val)
			if !hasRows {
				return fmt.Errorf("no channel model in database with %s %q", key, val)
			}
			if err != nil {
				return err
			}

			// Load config file location if existent and one wasn't hardcoded into terminal
			if configFile == "" && c.ChanSettings.ChannelConfigFile != "" {
				if err := file.LoadConfigFile(c.ChanSettings.ChannelConfigFile); err != nil {
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
			logging.S("Completed update for channel with %s %q", key, val)
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
		&moveOps, &pause)

	// Download
	setDownloadFlags(updateSettingsCmd, &retries, &useGlobalCookies,
		&ytdlpOutExt, &fromDate, &toDate,
		&cookieSource, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	setMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &minFreeMem,
		&outDir, &renameStyle, &metaOpsFile,
		&filteredMetaOpsFile, &filenameOpsFile, &filteredFilenameOpsFile,
		&urlOutDirs, &filenameOps, &filteredFilenameOps,
		&metaOps, &filteredMetaOps)

	// Transcoding
	setTranscodeFlags(updateSettingsCmd, &useGPU, &gpuDir,
		&transcodeVideoFilter, &codec, &audioCodec,
		&transcodeQuality)

	// Auth
	setAuthFlags(updateSettingsCmd, &username, &password,
		&loginURL, &authDetails)

	// Additional YTDLP args
	// YTDLPFlags
	setCustomYDLPArgFlags(updateSettingsCmd, &extraYTDLPVideoArgs, &extraYTDLPMetaArgs)

	updateSettingsCmd.Flags().BoolVar(&deleteAuth, "delete-auth", false, "Clear all authentication details for this channel and its URLs")

	return updateSettingsCmd
}

// updateChannelValue provides a command allowing the alteration of a channel row.
func updateChannelValue(cs contracts.ChannelStore) *cobra.Command {
	var (
		col, newVal, name string
		id                int
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
			logging.S("Updated channel column: %q → %q", col, newVal)
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(updateRowCmd, &name, nil, &id)

	updateRowCmd.Flags().StringVarP(&col, "column-name", "c", "", "The name of the column in the table (e.g. video_directory)")
	updateRowCmd.Flags().StringVarP(&newVal, "value", "v", "", "The value to set in the column (e.g. /my-directory)")
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
	if f.Changed(keys.MOutputExt) {
		if c.metarrExt != "" {
			_, err := validation.ValidateOutputFiletype(c.metarrExt)
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
		valid := []models.FilenameOps{}

		if len(c.filenameOps) > 0 {
			valid, err = validation.ValidateFilenameOps(c.filenameOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameOps = valid
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
		valid := []models.FilteredFilenameOps{}

		if len(c.filteredFilenameOps) > 0 {
			valid, err = validation.ValidateFilteredFilenameOps(c.filteredFilenameOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilteredFilenameOps = valid
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
		valid := []models.MetaOps{}

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

	if f.Changed(keys.MMetaOpsFile) {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOpsFile = c.metaOpsFile
			return nil
		})
	}

	// Filename meta operations (e.g. 'prefix:[COOL CAT VIDEOS]')
	if f.Changed(keys.MFilenameOps) {
		valid := []models.FilenameOps{}

		if len(c.filenameOps) > 0 {
			valid, err = validation.ValidateFilenameOps(c.filenameOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameOps = valid
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
		valid := []models.FilteredMetaOps{}

		if len(c.filteredMetaOps) > 0 {
			valid, err = validation.ValidateFilteredMetaOps(c.filteredMetaOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilteredMetaOps = valid
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
	metaDir                string
	maxFilesize            string
	moveOps                []string
	moveOpsFile            string
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
	if f.Changed(keys.Concurrency) {
		fns = append(fns, func(s *models.Settings) error {
			s.Concurrency = max(c.concurrency, 1)
			return nil
		})
	}

	// Channel config file location
	if f.Changed(keys.ChannelConfigFile) {
		fns = append(fns, func(s *models.Settings) error {
			s.ChannelConfigFile = c.channelConfigFile
			return nil
		})
	}

	// Cookie source
	if f.Changed(keys.CookieSource) {
		fns = append(fns, func(s *models.Settings) error {
			s.CookieSource = c.cookieSource
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
		dlFilters, err := validation.ValidateFilterOps(c.filters)
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
		moveOperations, err := validation.ValidateMoveOps(c.moveOps)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.Settings) error {
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
		fns = append(fns, func(s *models.Settings) error {
			s.FromDate = validFromDate
			return nil
		})
	}

	// JSON directory
	if f.Changed(keys.MetaDir) {
		if c.metaDir == "" {
			if c.videoDir == "" {
				return nil, fmt.Errorf("json directory cannot be empty. Attempted to default to video directory but video directory is also empty")
			}
			c.metaDir = c.videoDir
		}

		if _, err = validation.ValidateDirectory(c.metaDir, false); err != nil {
			return nil, err
		}

		fns = append(fns, func(s *models.Settings) error {
			s.MetaDir = c.metaDir
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

	// Deduplicate
	a = validation.DeduplicateSliceEntries(a)

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
		return authMap, nil
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
