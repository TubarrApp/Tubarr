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
	"tubarr/internal/domain/keys"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

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
			logging.S("Channel with %s %q set authentication details", key, val)
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
			logging.S("Deleted notify URLs for channel with %s %q: %v", key, val, notifyURLs)
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
			logging.S("Ignoring URL %q for channel with %s %q", ignoreURL, key, val)
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

			// Get and check key/val pair
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			c, hasRows, err := cs.GetChannelModel(key, val, true)
			if !hasRows {
				return fmt.Errorf("no rows in the database for channel with %s %q", key, val)
			}
			if err != nil {
				return err
			}

			// Initialize URL list
			c.URLModels, err = cs.GetChannelURLModels(c, false)
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
			logging.S("Paused channel %q", c.Name)
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
			logging.S("Unpaused channel %q", c.Name)
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
			logging.S("Unblocked channel %q", c.Name)
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
		// Channel identifiers
		name string

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
		addFromFile             string
		dlFilterFile            string
		moveOpFile              string
		metaOpsFile             string
		filteredMetaOpsFile     string
		filenameOpsFile         string
		filteredFilenameOpsFile string

		// Authentication details
		username    string
		password    string
		loginURL    string
		authDetails []string

		// Notification details
		notification []string

		// Download settings
		cookiesFromBrowser     string
		externalDownloader     string
		externalDownloaderArgs string
		maxFilesize            string
		ytdlpOutputExt         string
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
		transcodeGPU         string
		transcodeQuality     string
		transcodeVideoFilter string
		videoCodec           []string
		audioCodec           []string

		// Extra arguments
		extraYTDLPVideoArgs string
		extraYTDLPMetaArgs  string
		extraFFmpegArgs     string

		// Concurrency and performance settings
		crawlFreq         int
		concurrency       int
		metarrConcurrency int
		retries           int
		maxCPU            float64

		// Boolean flags
		pause     bool
		ignoreRun bool
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, settings, etc.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			// Load in file if specified
			return parsing.LoadDefaultsFromConfig(cmd, addFromFile, configFile)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			var err error

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
			var dlFilterModels []models.Filters
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
			if len(videoCodec) != 0 {
				if videoCodec, err = validation.ValidateVideoTranscodeCodecSlice(videoCodec, transcodeGPU); err != nil {
					return err
				}
			}

			// Audio codec
			if len(audioCodec) != 0 {
				if audioCodec, err = validation.ValidateAudioTranscodeCodecSlice(audioCodec); err != nil {
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
			authMap, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, urls, false)
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
					CookiesFromBrowser:     cookiesFromBrowser,
					CrawlFreq:              crawlFreq,
					ExternalDownloader:     externalDownloader,
					ExternalDownloaderArgs: externalDownloaderArgs,
					Filters:                dlFilterModels,
					FilterFile:             dlFilterFile,
					FromDate:               fromDate,
					JSONDir:                jDir,
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
					TranscodeVideoCodecs: videoCodec,
					TranscodeAudioCodecs: audioCodec,
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
	cmd.SetPrimaryChannelFlags(addCmd, &name, &urls, nil)

	// Files/dirs
	cmd.SetFileDirFlags(addCmd, &configFile, &jDir, &vDir)

	// Program related
	cmd.SetProgramRelatedFlags(addCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader,
		&moveOpFile, &moveOps, &pause)

	// Download
	cmd.SetDownloadFlags(addCmd, &retries, &useGlobalCookies,
		&ytdlpOutputExt, &fromDate, &toDate,
		&cookiesFromBrowser, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	cmd.SetMetarrFlags(addCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &minFreeMem,
		&outDir, &renameStyle, &metaOpsFile,
		&filteredMetaOpsFile, &filenameOpsFile, &filteredFilenameOpsFile,
		&urlOutDirs, &filenameOps, &filteredFilenameOps,
		&metaOps, &filteredMetaOps)

	// Login credentials
	cmd.SetAuthFlags(addCmd, &username, &password, &loginURL, &authDetails)

	// Transcoding
	cmd.SetTranscodeFlags(addCmd, &transcodeGPU, &gpuDir,
		&transcodeVideoFilter, &transcodeQuality, &videoCodec,
		&audioCodec)

	// YTDLPFlags
	cmd.SetCustomYDLPArgFlags(addCmd, &extraYTDLPVideoArgs, &extraYTDLPMetaArgs)

	// Notification URL
	cmd.SetNotifyFlags(addCmd, &notification)

	// Pause or ignore run
	addCmd.Flags().BoolVar(&pause, "pause", false, "Paused channels won't crawl videos on a normal program run")
	addCmd.Flags().BoolVar(&ignoreRun, "ignore-run", false, "Run an 'ignore crawl' first so only new videos are downloaded (rather than the entire channel backlog)")
	addCmd.Flags().StringVar(&addFromFile, "add-channel-from-file", "", "Add a channel using a prewritten file (.toml, .yaml, etc.).\nFile contents example:\n\nchannel-name: 'Cool Channel'\nchannel-urls:\n  - 'https://www.coolchannel.com/'\n")

	return addCmd
}

// addBatchChannelsCmd adds multiple channels from config files in a directory.
func addBatchChannelsCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	type batchVars struct {
		// Directory containing config files
		configDirectory string

		// Channel identifiers
		name string

		// URLs
		urls       []string
		urlOutDirs []string

		// Directory paths
		vDir   string
		jDir   string
		outDir string
		gpuDir string

		// Configuration files
		channelConfigFile       string
		dlFilterFile            string
		moveOpFile              string
		metaOpsFile             string
		filteredMetaOpsFile     string
		filenameOpsFile         string
		filteredFilenameOpsFile string

		// Authentication details
		username    string
		password    string
		loginURL    string
		authDetails []string

		// Notification details
		notification []string

		// Download settings
		cookiesFromBrowser     string
		externalDownloader     string
		externalDownloaderArgs string
		maxFilesize            string
		ytdlpOutputExt         string
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
		transcodeGPU         string
		transcodeQuality     string
		transcodeVideoFilter string
		videoCodec           []string
		audioCodec           []string

		// Extra arguments
		extraYTDLPVideoArgs string
		extraYTDLPMetaArgs  string
		extraFFmpegArgs     string

		// Concurrency and performance settings
		crawlFreq         int
		concurrency       int
		metarrConcurrency int
		retries           int
		maxCPU            float64

		// Boolean flags
		pause     bool
		ignoreRun bool
	}

	var bv batchVars

	batchCmd := &cobra.Command{
		Use:   "add-batch",
		Short: "Add multiple channels from a directory.",
		Long:  "Add multiple channels by reading all Viper-compatible config files (.yaml, .yml, .toml, .json, etc.) from a directory.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if bv.configDirectory == "" {
				return errors.New("must specify a directory path with --from-directory")
			}

			// Scan directory for config files
			batchConfigFiles, err := file.ScanDirectoryForConfigFiles(bv.configDirectory)
			if err != nil {
				return fmt.Errorf("failed to scan directory: %w", err)
			}

			if len(batchConfigFiles) == 0 {
				logging.I("No config files found in directory %q", bv.configDirectory)
				return nil
			}

			logging.I("Found %d config file(s) in directory %q", len(batchConfigFiles), bv.configDirectory)

			// Track results
			var successes []string
			var failures []struct {
				file string
				err  error
			}

			// Process each config file
			for _, batchConfigFile := range batchConfigFiles {
				logging.I("Processing config file: %s", batchConfigFile)

				// Reset all flag variables to their zero values before processing each config file
				bv = batchVars{}

				// Reset flag Changed status so loadDefaultsFromConfig will apply new values
				cmd.Flags().VisitAll(func(f *pflag.Flag) {
					f.Changed = false
				})

				// Load config file into viper
				v := viper.New()
				if err := file.LoadConfigFileLocal(v, batchConfigFile); err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("failed to load config: %w", err)})
					continue
				}

				// Apply defaults from config to command flags
				if err := parsing.LoadDefaultsFromConfig(cmd, batchConfigFile, ""); err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("failed to apply config: %w", err)})
					continue
				}

				// Validate required fields
				if bv.vDir == "" || len(bv.urls) == 0 || bv.name == "" {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, errors.New("config must specify channel-name, channel-urls, and video-directory")})
					continue
				}

				if bv.jDir == "" {
					bv.jDir = bv.vDir
				}

				if _, err := validation.ValidateDirectory(bv.vDir, true); err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("invalid video directory: %w", err)})
					continue
				}

				// Validate filter operations
				var dlFilterModels []models.Filters
				if dlFilterModels, err = validation.ValidateFilterOps(bv.dlFilters); err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("invalid filter operations: %w", err)})
					continue
				}

				// Validate move operation filters
				moveOpsModels, err := validation.ValidateMoveOps(bv.moveOps)
				if err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("invalid move operations: %w", err)})
					continue
				}

				// Meta operations
				var metaOpsModels = []models.MetaOps{}
				if len(bv.metaOps) > 0 {
					if metaOpsModels, err = validation.ValidateMetaOps(bv.metaOps); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid meta operations: %w", err)})
						continue
					}
				}

				// Filtered meta operations
				var filteredMetaOpsModels = []models.FilteredMetaOps{}
				if len(bv.filteredMetaOps) > 0 {
					if filteredMetaOpsModels, err = validation.ValidateFilteredMetaOps(bv.filteredMetaOps); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid filtered meta operations: %w", err)})
						continue
					}
				}

				// Filename operations
				var filenameOpsModels = []models.FilenameOps{}
				if len(bv.filenameOps) > 0 {
					if filenameOpsModels, err = validation.ValidateFilenameOps(bv.filenameOps); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid filename operations: %w", err)})
						continue
					}
				}

				// Filtered filename operations
				var filteredFilenameOpsModels = []models.FilteredFilenameOps{}
				if len(bv.filteredFilenameOps) > 0 {
					if filteredFilenameOpsModels, err = validation.ValidateFilteredFilenameOps(bv.filteredFilenameOps); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid filtered filename operations: %w", err)})
						continue
					}
				}

				// Rename style
				if bv.renameStyle != "" {
					if err := validation.ValidateRenameFlag(bv.renameStyle); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid rename style: %w", err)})
						continue
					}
				}

				// Min free memory
				if bv.minFreeMem != "" {
					if err := validation.ValidateMinFreeMem(bv.minFreeMem); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid min free memory: %w", err)})
						continue
					}
				}

				// From date
				if bv.fromDate != "" {
					if bv.fromDate, err = validation.ValidateToFromDate(bv.fromDate); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid from-date: %w", err)})
						continue
					}
				}

				// To date
				if bv.toDate != "" {
					if bv.toDate, err = validation.ValidateToFromDate(bv.toDate); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid to-date: %w", err)})
						continue
					}
				}

				// HW Acceleration GPU
				if bv.transcodeGPU != "" {
					if bv.transcodeGPU, bv.gpuDir, err = validation.ValidateGPU(bv.transcodeGPU, bv.gpuDir); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid GPU settings: %w", err)})
						continue
					}
				}

				// Video codec
				if len(bv.videoCodec) != 0 {
					if bv.videoCodec, err = validation.ValidateVideoTranscodeCodecSlice(bv.videoCodec, bv.transcodeGPU); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid video codec: %w", err)})
						continue
					}
				}

				// Audio codec
				if len(bv.audioCodec) != 0 {
					if bv.audioCodec, err = validation.ValidateAudioTranscodeCodecSlice(bv.audioCodec); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid audio codec: %w", err)})
						continue
					}
				}

				// Transcode quality
				if bv.transcodeQuality != "" {
					if bv.transcodeQuality, err = validation.ValidateTranscodeQuality(bv.transcodeQuality); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid transcode quality: %w", err)})
						continue
					}
				}

				// YTDLP output extension
				if bv.ytdlpOutputExt != "" {
					bv.ytdlpOutputExt = strings.ToLower(bv.ytdlpOutputExt)
					if err = validation.ValidateYtdlpOutputExtension(bv.ytdlpOutputExt); err != nil {
						failures = append(failures, struct {
							file string
							err  error
						}{batchConfigFile, fmt.Errorf("invalid ytdlp output extension: %w", err)})
						continue
					}
				}

				// Parse and validate authentication details
				authMap, err := auth.ParseAuthDetails(bv.username, bv.password, bv.loginURL, bv.authDetails, bv.urls, false)
				if err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("invalid authentication details: %w", err)})
					continue
				}

				now := time.Now()
				var chanURLs []*models.ChannelURL
				for _, u := range bv.urls {
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
					Name:      bv.name,

					ChanSettings: &models.Settings{
						ChannelConfigFile:      bv.channelConfigFile,
						Concurrency:            bv.concurrency,
						CookiesFromBrowser:     bv.cookiesFromBrowser,
						CrawlFreq:              bv.crawlFreq,
						ExternalDownloader:     bv.externalDownloader,
						ExternalDownloaderArgs: bv.externalDownloaderArgs,
						Filters:                dlFilterModels,
						FilterFile:             bv.dlFilterFile,
						FromDate:               bv.fromDate,
						JSONDir:                bv.jDir,
						MaxFilesize:            bv.maxFilesize,
						MoveOps:                moveOpsModels,
						MoveOpFile:             bv.moveOpFile,
						Paused:                 bv.pause,
						Retries:                bv.retries,
						ToDate:                 bv.toDate,
						UseGlobalCookies:       bv.useGlobalCookies,
						VideoDir:               bv.vDir,
						YtdlpOutputExt:         bv.ytdlpOutputExt,
						ExtraYTDLPVideoArgs:    bv.extraYTDLPVideoArgs,
						ExtraYTDLPMetaArgs:     bv.extraYTDLPMetaArgs,
					},

					ChanMetarrArgs: &models.MetarrArgs{
						OutputExt:       bv.metarrExt,
						ExtraFFmpegArgs: bv.extraFFmpegArgs,

						MetaOps:             metaOpsModels,
						MetaOpsFile:         bv.metaOpsFile,
						FilteredMetaOps:     filteredMetaOpsModels,
						FilteredMetaOpsFile: bv.filteredMetaOpsFile,

						FilenameOps:             filenameOpsModels,
						FilenameOpsFile:         bv.filenameOpsFile,
						FilteredFilenameOps:     filteredFilenameOpsModels,
						FilteredFilenameOpsFile: bv.filteredFilenameOpsFile,

						RenameStyle:          bv.renameStyle,
						MaxCPU:               bv.maxCPU,
						MinFreeMem:           bv.minFreeMem,
						OutputDir:            bv.outDir,
						URLOutputDirs:        bv.urlOutDirs,
						Concurrency:          bv.metarrConcurrency,
						UseGPU:               bv.transcodeGPU,
						GPUDir:               bv.gpuDir,
						TranscodeVideoCodecs: bv.videoCodec,
						TranscodeAudioCodecs: bv.audioCodec,
						TranscodeQuality:     bv.transcodeQuality,
						TranscodeVideoFilter: bv.transcodeVideoFilter,
					},

					LastScan:  now,
					CreatedAt: now,
					UpdatedAt: now,
				}

				// Add channel to database and retrieve ID
				channelID, err := cs.AddChannel(c)
				if err != nil {
					failures = append(failures, struct {
						file string
						err  error
					}{batchConfigFile, fmt.Errorf("failed to add channel to database: %w", err)})
					continue
				}
				// Set the ID on the model
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

				// Validate notification URLs
				if len(bv.notification) != 0 {
					validPairs, err := validation.ValidateNotificationStrings(bv.notification)
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

				// Should perform an ignore run?
				if bv.ignoreRun {
					logging.I("Running an 'ignore crawl' for channel %q...", bv.name)
					if err := app.CrawlChannelIgnore(ctx, s, c); err != nil {
						logging.E("Failed to complete ignore crawl run for %q: %v", bv.name, err)
					}
				}

				// Success for this file
				successes = append(successes, batchConfigFile)
				logging.S("Successfully added channel %q from config file: %s", bv.name, batchConfigFile)
			}

			// Print summary
			logging.I("====== Batch Add Summary ======\n")
			logging.S("Successfully added %d channel(s)", len(successes))
			if len(failures) > 0 {
				logging.E("Failed to add %d channel(s):", len(failures))
				for _, f := range failures {
					logging.E("  - %s: %v", f.file, f.err)
				}
			}

			return nil
		},
	}

	// Add flags that are needed for the command to work
	batchCmd.Flags().StringVar(&bv.configDirectory, "add-from-directory", "", "Directory containing channel config files (.yaml, .yml, .toml, .json, etc.)")

	// Add all the same flags as the regular add command so they can be parsed from config files
	cmd.SetPrimaryChannelFlags(batchCmd, &bv.name, &bv.urls, nil)
	cmd.SetFileDirFlags(batchCmd, &bv.channelConfigFile, &bv.jDir, &bv.vDir)

	cmd.SetProgramRelatedFlags(batchCmd, &bv.concurrency, &bv.crawlFreq, &bv.externalDownloaderArgs,
		&bv.externalDownloader, &bv.moveOpFile, &bv.moveOps, &bv.pause)

	cmd.SetDownloadFlags(batchCmd, &bv.retries, &bv.useGlobalCookies, &bv.ytdlpOutputExt,
		&bv.fromDate, &bv.toDate, &bv.cookiesFromBrowser, &bv.maxFilesize,
		&bv.dlFilterFile, &bv.dlFilters)

	cmd.SetMetarrFlags(batchCmd, &bv.maxCPU, &bv.metarrConcurrency, &bv.metarrExt,
		&bv.extraFFmpegArgs, &bv.minFreeMem, &bv.outDir, &bv.renameStyle,
		&bv.metaOpsFile, &bv.filteredMetaOpsFile, &bv.filenameOpsFile, &bv.filteredFilenameOpsFile,
		&bv.urlOutDirs, &bv.filenameOps, &bv.filteredFilenameOps, &bv.metaOps, &bv.filteredMetaOps)

	cmd.SetAuthFlags(batchCmd, &bv.username, &bv.password, &bv.loginURL, &bv.authDetails)

	cmd.SetTranscodeFlags(batchCmd, &bv.transcodeGPU, &bv.gpuDir, &bv.transcodeVideoFilter,
		&bv.transcodeQuality, &bv.videoCodec, &bv.audioCodec)

	cmd.SetCustomYDLPArgFlags(batchCmd, &bv.extraYTDLPVideoArgs, &bv.extraYTDLPMetaArgs)
	cmd.SetNotifyFlags(batchCmd, &bv.notification)

	batchCmd.Flags().BoolVar(&bv.pause, "pause", false, "Paused channels won't crawl videos on a normal program run")
	batchCmd.Flags().BoolVar(&bv.ignoreRun, "ignore-run", false, "Run an 'ignore crawl' for each channel so only new videos are downloaded")

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
			logging.S("Successfully deleted channel with %s %q", key, val)
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

			// Don't print errors from CrawlChannel, already handled in function
			if err := app.CrawlChannel(ctx, s, c); err != nil {
				return err
			}

			// Success
			logging.S("Completed crawl for channel with %s %q", key, val)
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
		id   int
		name string

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
		Use:   "update-settings",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			// Load in file if specified
			return parsing.LoadDefaultsFromConfig(cmd, configFile, "")
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
			if !cmd.Flags().Changed(keys.ChannelConfigFile) && c.ChanSettings.ChannelConfigFile != "" {
				if err := file.LoadConfigFile(c.ChanSettings.ChannelConfigFile); err != nil {
					return err
				}
				if err := parsing.LoadDefaultsFromConfig(cmd, c.ChanSettings.ChannelConfigFile, ""); err != nil {
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
				channelConfigFile:      configFile,
				concurrency:            concurrency,
				cookiesFromBrowser:     cookiesFromBrowser,
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
			logging.S("Completed update for channel with %s %q", key, val)
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
			logging.S("Updated channel column: %q → %q", col, newVal)
			return nil
		},
	}

	// Primary channel elements
	cmd.SetPrimaryChannelFlags(updateRowCmd, &name, nil, &id)

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
			if err := validation.ValidateMinFreeMem(c.minFreeMem); err != nil {
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
		c.maxCPU = max(min(c.maxCPU, 100.0), 0)
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MaxCPU = c.maxCPU
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
	cookiesFromBrowser     string
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
	case "name", "video_directory", "json_directory":
		if val == "" {
			return fmt.Errorf("cannot set %s blank, please use the 'delete' function if you want to remove this channel entirely", col)
		}
	default:
		return errors.New("cannot set a custom value for internal DB elements")
	}
	return nil
}
