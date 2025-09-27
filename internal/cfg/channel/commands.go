// Package cfgchannel sets up Cobra channel commands.
package cfgchannel

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	cfgflags "tubarr/internal/cfg/flags"
	"tubarr/internal/cfg/validation"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

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
	channelCmd.AddCommand(dlURLs(cs, s, ctx))
	channelCmd.AddCommand(crawlChannelCmd(cs, s, ctx))
	channelCmd.AddCommand(addCrawlToIgnore(cs, s, ctx))
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
		channelName                     string
		usernames, passwords, loginURLs []string
		channelID                       int
	)

	addAuthCmd := &cobra.Command{
		Use:   "auth",
		Short: "Add authentication details to a channel.",
		Long:  "Add authentication details to a channel for use in crawls.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if usernames == nil || passwords == nil || loginURLs == nil {
				return errors.New("must enter a username, password, and login URL")
			}

			chanID := int64(channelID)

			if channelID == 0 {
				key, val, err := getChanKeyVal(channelID, channelName)
				if err != nil {
					return err
				}

				if chanID, err = cs.GetID(key, val); err != nil {
					return err
				}
			}

			authDetails, err := parseAuthDetails(usernames, passwords, loginURLs)
			if err != nil {
				return err
			}

			if len(authDetails) > 0 {
				if err := cs.AddAuth(chanID, authDetails); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cfgflags.SetPrimaryChannelFlags(addAuthCmd, &channelName, nil, &channelID)
	cfgflags.SetAuthFlags(addAuthCmd, &usernames, &passwords, &loginURLs)
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
		Use:   "get-urls",
		Short: "Download inputted URLs (plaintext or file).",
		Long:  "If using a file, the file should contain one URL per line.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cFileInfo, err := validation.ValidateFile(cFile, false)
			if err != nil {
				return fmt.Errorf("file entered (%q) is not valid: %v", cFile, err)
			}
			if cFile != "" && cFileInfo.Size() == 0 {
				return fmt.Errorf("url file %q is blank", cFile)
			}

			if cFile == "" && len(urls) == 0 {
				return errors.New("must enter URLs into the source file, or set at least one URL directly")
			}

			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			if len(urls) > 0 {
				viper.Set(keys.URLAdd, urls)
			}

			if cFile != "" {
				viper.Set(keys.URLFile, cFile)
			}

			if err := cs.CrawlChannel(key, val, s, ctx); err != nil {
				return err
			}
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

			if len(notifyURLs) == 0 && len(notifyNames) == 0 {
				return errors.New("must enter at least one notify URL or name to delete")
			}

			var (
				id = int64(channelID)
			)
			if id == 0 {
				key, val, err := getChanKeyVal(channelID, channelName)
				if err != nil {
					return err
				}

				id, err = cs.GetID(key, val)
				if err != nil {
					return err
				}
			}

			if err := cs.DeleteNotifyURLs(id, notifyURLs, notifyNames); err != nil {
				return err
			}

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

			if len(notification) == 0 {
				return errors.New("no notification URL|Name pairs entered")
			}

			var (
				id = int64(channelID)
			)
			if id == 0 {
				key, val, err := getChanKeyVal(channelID, channelName)
				if err != nil {
					return err
				}

				id, err = cs.GetID(key, val)
				if err != nil {
					return err
				}
			}

			validPairs, err := validation.ValidateNotificationPairs(notification)
			if err != nil {
				return err
			}

			if err := cs.AddNotifyURLs(id, validPairs); err != nil {
				return err
			}

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
			if ignoreURL == "" {
				return errors.New("cannot enter the target ignore URL blank")
			}

			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			id, err := cs.GetID(key, val)
			if err != nil {
				return err
			}

			if err := cs.AddURLToIgnore(id, ignoreURL); err != nil {
				return err
			}
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(ignoreURLCmd, &name, nil, nil)
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-video-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// addCrawlToIgnore crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func addCrawlToIgnore(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		name string
		id   int
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:   "ignore-crawl",
		Short: "Crawl a channel for URLs to ignore.",
		Long:  "Crawls the current state of a channel page and adds all video URLs to ignore.",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			if err := cs.CrawlChannelIgnore(key, val, s, ctx); err != nil {
				return err
			}
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
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			if err := cs.UpdateChannelValue(key, val, consts.QChanPaused, true); err != nil {
				return err
			}
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

	pauseCmd := &cobra.Command{
		Use:   "unpause",
		Short: "Unpause a channel.",
		Long:  "Unpauses a channel, allowing it to download new videos when the main program runs a crawl.",
		RunE: func(cmd *cobra.Command, args []string) error {
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			if err := cs.UpdateChannelValue(key, val, consts.QChanPaused, false); err != nil {
				return err
			}
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(pauseCmd, &name, nil, &id)

	return pauseCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		urls []string
		name, vDir, jDir, outDir, cookieSource,
		externalDownloader, externalDownloaderArgs, maxFilesize, filenameDateTag, renameStyle, minFreeMem, metarrExt string
		usernames, passwords, loginURLs                                                           []string
		notification                                                                              []string
		fromDate, toDate                                                                          string
		dlFilters, metaOps, fileSfxReplace                                                        []string
		configFile, dlFilterFile                                                                  string
		crawlFreq, concurrency, metarrConcurrency, retries                                        int
		maxCPU                                                                                    float64
		useGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter, ytdlpOutputExt string
		pause, ignoreRun, useGlobalCookies                                                        bool
	)

	now := time.Now()
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, settings, etc.",
		RunE: func(cmd *cobra.Command, args []string) error {

			// Files and directories
			if vDir == "" || len(urls) == 0 || name == "" {
				return errors.New("new channels require a video directory, name, and at least one channel URL")
			}

			if jDir == "" { // Do not stat, due to templating
				jDir = vDir
			}

			if configFile != "" {
				if err := loadConfigFile(configFile); err != nil {
					return err
				}
			}

			// Verify filters
			dlFilters, err := validation.ValidateChannelOps(dlFilters)
			if err != nil {
				return err
			}

			if filenameDateTag != "" {
				if !validation.ValidateDateFormat(filenameDateTag) {
					return errors.New("invalid Metarr filename date tag format")
				}
			}

			if len(metaOps) > 0 {
				if metaOps, err = validation.ValidateMetaOps(metaOps); err != nil {
					return err
				}
			}

			if len(fileSfxReplace) > 0 {
				if fileSfxReplace, err = validation.ValidateFilenameSuffixReplace(fileSfxReplace); err != nil {
					return err
				}
			}

			if renameStyle != "" {
				if err := validation.ValidateRenameFlag(renameStyle); err != nil {
					return err
				}
			}

			if minFreeMem != "" {
				if err := validation.ValidateMinFreeMem(minFreeMem); err != nil {
					return err
				}
			}

			if fromDate != "" {
				if fromDate, err = validation.ValidateToFromDate(fromDate); err != nil {
					return err
				}
			}

			if toDate != "" {
				if toDate, err = validation.ValidateToFromDate(toDate); err != nil {
					return err
				}
			}

			if useGPU != "" {
				if useGPU, gpuDir, err = validation.ValidateGPU(useGPU, gpuDir); err != nil {
					return err
				}
			}

			if codec != "" {
				if codec, err = validation.ValidateTranscodeCodec(codec, useGPU); err != nil {
					return err
				}
			}

			if audioCodec != "" {
				if audioCodec, err = validation.ValidateTranscodeAudioCodec(audioCodec); err != nil {
					return err
				}
			}

			if transcodeQuality != "" {
				if transcodeQuality, err = validation.ValidateTranscodeQuality(transcodeQuality); err != nil {
					return err
				}
			}

			if ytdlpOutputExt != "" {
				ytdlpOutputExt = strings.ToLower(ytdlpOutputExt)
				if err = validation.ValidateYtdlpOutputExtension(ytdlpOutputExt); err != nil {
					return err
				}
			}

			c := &models.Channel{
				URLs:     urls,
				Name:     name,
				VideoDir: vDir,
				JSONDir:  jDir,

				Settings: models.ChannelSettings{
					ChannelConfigFile:      configFile,
					Concurrency:            concurrency,
					CookieSource:           cookieSource,
					CrawlFreq:              crawlFreq,
					ExternalDownloader:     externalDownloader,
					ExternalDownloaderArgs: externalDownloaderArgs,
					Filters:                dlFilters,
					FilterFile:             dlFilterFile,
					FromDate:               fromDate,
					MaxFilesize:            maxFilesize,
					Retries:                retries,
					ToDate:                 toDate,
					UseGlobalCookies:       useGlobalCookies,
					YtdlpOutputExt:         ytdlpOutputExt,
				},

				MetarrArgs: models.MetarrArgs{
					Ext:                  metarrExt,
					MetaOps:              metaOps,
					FileDatePfx:          filenameDateTag,
					RenameStyle:          renameStyle,
					FilenameReplaceSfx:   fileSfxReplace,
					MaxCPU:               maxCPU,
					MinFreeMem:           minFreeMem,
					OutputDir:            outDir,
					Concurrency:          metarrConcurrency,
					UseGPU:               useGPU,
					GPUDir:               gpuDir,
					TranscodeCodec:       codec,
					TranscodeAudioCodec:  audioCodec,
					TranscodeQuality:     transcodeQuality,
					TranscodeVideoFilter: transcodeVideoFilter,
				},

				LastScan:  now,
				Paused:    pause,
				CreatedAt: now,
				UpdatedAt: now,
			}

			channelID, err := cs.AddChannel(c)
			if err != nil {
				return err
			}

			authDetails, err := parseAuthDetails(usernames, passwords, loginURLs)
			if err != nil {
				return err
			}

			if len(authDetails) > 0 {
				if err := cs.AddAuth(channelID, authDetails); err != nil {
					return err
				}
			}

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

			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(addCmd, &name, &urls, nil)

	// Files/dirs
	cfgflags.SetFileDirFlags(addCmd, &configFile, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(addCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader, false)

	// Download
	cfgflags.SetDownloadFlags(addCmd, &retries, &useGlobalCookies, &ytdlpOutputExt, &fromDate, &toDate, &cookieSource, &maxFilesize, &dlFilterFile, &dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(addCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	// Login credentials
	cfgflags.SetAuthFlags(addCmd, &usernames, &passwords, &loginURLs)

	// Transcoding
	cfgflags.SetTranscodeFlags(addCmd, &useGPU, &gpuDir, &transcodeVideoFilter, &codec, &audioCodec, &transcodeQuality)

	// Notification URL
	cfgflags.SetNotifyFlags(addCmd, &notification)

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
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			if err := cs.DeleteChannel(key, val); err != nil {
				return err
			}
			logging.S(0, "Successfully deleted channel with key %q and value %q", key, val)
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
		name, key, val string
		err            error
		channelID      int
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List a channel's details.",
		Long:  "Lists details of a channel in the database.",
		RunE: func(cmd *cobra.Command, args []string) error {

			id := int64(channelID)
			if id == 0 {
				key, val, err = getChanKeyVal(channelID, name)
				if err != nil {
					return err
				}

				if id, err = cs.GetID(key, val); err != nil {
					return err
				}
			}

			ch, err, hasRows := cs.FetchChannelModel("id", strconv.FormatInt(id, 10))
			if !hasRows {
				logging.I("Entry for channel with ID %d does not exist in the database", id)
				return nil
			}
			if err != nil {
				return err
			}

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
			chans, err, hasRows := cs.FetchAllChannels()
			if !hasRows {
				logging.I("No entries in the database")
				return nil
			}
			if err != nil {
				return err
			}

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
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			if err := cs.CrawlChannel(key, val, s, ctx); err != nil {
				return err
			}
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
		name, cookieSource                                                        string
		minFreeMem, renameStyle, filenameDateTag, metarrExt                       string
		maxFilesize, externalDownloader, externalDownloaderArgs                   string
		username, password, loginURL                                              []string
		dlFilters, metaOps                                                        []string
		configFile, dlFilterFile                                                  string
		fileSfxReplace                                                            []string
		useGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter string
		fromDate, toDate                                                          string
		ytdlpOutExt                                                               string
		useGlobalCookies                                                          bool
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-settings",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		RunE: func(cmd *cobra.Command, args []string) error {

			logging.I("Updating channel with name %v", name)

			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Files/dirs:
			if vDir != "" { // Do not stat, due to templating
				if err := cs.UpdateChannelValue(key, val, consts.QChanVideoDir, vDir); err != nil {
					return fmt.Errorf("failed to update video directory: %w", err)
				}
				logging.S(0, "Updated video directory to %q", vDir)
			}

			if jDir != "" { // Do not stat, due to templating
				if err := cs.UpdateChannelValue(key, val, consts.QChanJSONDir, jDir); err != nil {
					return fmt.Errorf("failed to update JSON directory: %w", err)
				}
				logging.S(0, "Updated JSON directory to %q", jDir)
			}

			// Update from config file
			if configFile != "" {
				if err := loadConfigFile(configFile); err != nil {
					return err
				}
			} else {
				c, err, _ := cs.FetchChannelModel(key, val)
				if err != nil {
					return err
				}

				if c.Settings.ChannelConfigFile != "" {

					if err := loadConfigFile(configFile); err != nil {
						return err
					}
				}
			}

			// Settings
			fnSettingsArgs, err := getSettingsArgFns(chanSettings{
				channelConfigFile:      configFile,
				cookieSource:           cookieSource,
				crawlFreq:              crawlFreq,
				retries:                retries,
				filters:                dlFilters,
				filterFile:             dlFilterFile,
				externalDownloader:     externalDownloader,
				externalDownloaderArgs: externalDownloaderArgs,
				concurrency:            concurrency,
				maxFilesize:            maxFilesize,
				fromDate:               fromDate,
				toDate:                 toDate,
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
				if _, err := cs.UpdateChannelSettingsJSON(key, val, finalUpdateFn); err != nil {
					return err
				}
			}

			fnMetarrArray, err := getMetarrArgFns(cmd, cobraMetarrArgs{
				filenameReplaceSfx:   fileSfxReplace,
				renameStyle:          renameStyle,
				fileDatePfx:          filenameDateTag,
				metarrExt:            metarrExt,
				metaOps:              metaOps,
				outputDir:            outDir,
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
				if _, err := cs.UpdateChannelMetarrArgsJSON(key, val, finalUpdateFn); err != nil {
					return err
				}
			}
			return nil
		},
	}

	// Primary channel elements
	cfgflags.SetPrimaryChannelFlags(updateSettingsCmd, &name, &urls, &id)

	// Files/dirs
	cfgflags.SetFileDirFlags(updateSettingsCmd, &configFile, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(updateSettingsCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader, true)

	// Download
	cfgflags.SetDownloadFlags(updateSettingsCmd, &retries, &useGlobalCookies, &ytdlpOutExt, &fromDate, &toDate, &cookieSource, &maxFilesize, &dlFilterFile, &dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	// Transcoding
	cfgflags.SetTranscodeFlags(updateSettingsCmd, &useGPU, &gpuDir, &transcodeVideoFilter, &codec, &audioCodec, &transcodeQuality)

	// Auth
	cfgflags.SetAuthFlags(updateSettingsCmd, &username, &password, &loginURL)

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

			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			if err := verifyChanRowUpdateValid(col, newVal); err != nil {
				return err
			}
			if err := cs.UpdateChannelValue(key, val, col, newVal); err != nil {
				return err
			}
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
// displaySettings displays fields relevant to a channel.
func displaySettings(cs interfaces.ChannelStore, ch *models.Channel) {
	notifyURLs, err := cs.GetNotifyURLs(ch.ID)
	if err != nil {
		logging.E(0, "Unable to fetch notification URLs for channel %q: %v", ch.Name, err)
	}

	fmt.Printf("\n%sChannel%s %q\n", consts.ColorGreen, consts.ColorReset, ch.Name)

	// Channel basic info
	fmt.Printf("\n%sBasic Info:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("ID: %d\n", ch.ID)
	fmt.Printf("Name: %s\n", ch.Name)
	fmt.Printf("URL: %+v\n", ch.URLs)
	fmt.Printf("Video Directory: %s\n", ch.VideoDir)
	fmt.Printf("JSON Directory: %s\n", ch.JSONDir)
	fmt.Printf("Paused: %v\n", ch.Paused)

	// Channel settings
	fmt.Printf("\n%sChannel Settings:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Config File: %s\n", ch.Settings.ChannelConfigFile)
	fmt.Printf("Auto Download: %v\n", ch.Settings.AutoDownload)
	fmt.Printf("Crawl Frequency: %d minutes\n", ch.Settings.CrawlFreq)
	fmt.Printf("Concurrency: %d\n", ch.Settings.Concurrency)
	fmt.Printf("Cookie Source: %s\n", ch.Settings.CookieSource)
	fmt.Printf("Retries: %d\n", ch.Settings.Retries)
	fmt.Printf("External Downloader: %s\n", ch.Settings.ExternalDownloader)
	fmt.Printf("External Downloader Args: %s\n", ch.Settings.ExternalDownloaderArgs)
	fmt.Printf("Filter File: %s\n", ch.Settings.FilterFile)
	fmt.Printf("From Date: %q\n", hyphenateYyyyMmDd(ch.Settings.FromDate))
	fmt.Printf("To Date: %q\n", hyphenateYyyyMmDd(ch.Settings.ToDate))
	fmt.Printf("Max Filesize: %s\n", ch.Settings.MaxFilesize)
	fmt.Printf("Use Global Cookies: %v\n", ch.Settings.UseGlobalCookies)
	fmt.Printf("Yt-dlp Output Extension: %s\n", ch.Settings.YtdlpOutputExt)

	// Metarr settings
	fmt.Printf("\n%sMetarr Settings:%s\n", consts.ColorCyan, consts.ColorReset)
	fmt.Printf("Output Directory: %s\n", ch.MetarrArgs.OutputDir)
	fmt.Printf("Output Filetype: %s\n", ch.MetarrArgs.Ext)
	fmt.Printf("Metarr Concurrency: %d\n", ch.MetarrArgs.Concurrency)
	fmt.Printf("Max CPU: %.2f\n", ch.MetarrArgs.MaxCPU)
	fmt.Printf("Min Free Memory: %s\n", ch.MetarrArgs.MinFreeMem)
	fmt.Printf("HW Acceleration: %s\n", ch.MetarrArgs.UseGPU)
	fmt.Printf("HW Acceleration Directory: %s\n", ch.MetarrArgs.GPUDir)
	fmt.Printf("Video Codec: %s\n", ch.MetarrArgs.TranscodeCodec)
	fmt.Printf("Audio Codec: %s\n", ch.MetarrArgs.TranscodeAudioCodec)
	fmt.Printf("Transcode Quality: %s\n", ch.MetarrArgs.TranscodeQuality)
	fmt.Printf("Rename Style: %s\n", ch.MetarrArgs.RenameStyle)
	fmt.Printf("Filename Suffix Replace: %v\n", ch.MetarrArgs.FilenameReplaceSfx)
	fmt.Printf("Meta Operations: %v\n", ch.MetarrArgs.MetaOps)
	fmt.Printf("Filename Date Format: %s\n", ch.MetarrArgs.FileDatePfx)

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
		*s = c.Settings
		return nil
	})
	if err != nil {
		return err
	}

	_, err = cs.UpdateChannelMetarrArgsJSON(key, val, func(m *models.MetarrArgs) error {
		*m = c.MetarrArgs
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
	if v, ok := getConfigValue[bool](keys.AutoDownload); ok {
		c.Settings.AutoDownload = v
	}
	if v, ok := getConfigValue[string](keys.ChannelConfigFile); ok {
		if _, err = validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.Settings.ChannelConfigFile = v
	}
	if v, ok := getConfigValue[int](keys.ConcurrencyLimitInput); ok {
		c.Settings.Concurrency = validation.ValidateConcurrencyLimit(v)
	}
	if v, ok := getConfigValue[string](keys.CookieSource); ok {
		c.Settings.CookieSource = v // No check for this currently! (cookies-from-browser)
	}
	if v, ok := getConfigValue[int](keys.CrawlFreq); ok {
		c.Settings.CrawlFreq = v
	}
	if v, ok := getConfigValue[string](keys.ExternalDownloader); ok {
		c.Settings.ExternalDownloader = v // No checks for this yet.
	}
	if v, ok := getConfigValue[string](keys.ExternalDownloaderArgs); ok {
		c.Settings.ExternalDownloaderArgs = v // No checks for this yet.
	}
	if v, ok := getConfigValue[string](keys.FilterOpsFile); ok {
		if _, err := validation.ValidateFile(v, false); err != nil {
			return err
		}
		c.Settings.FilterFile = v
	}
	if v, ok := getConfigValue[string](keys.FromDate); ok {
		if c.Settings.FromDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}
	if v, ok := getConfigValue[string](keys.MaxFilesize); ok {
		c.Settings.MaxFilesize = v
	}
	if v, ok := getConfigValue[int](keys.DLRetries); ok {
		c.Settings.Retries = v
	}
	if v, ok := getConfigValue[string](keys.ToDate); ok {
		if c.Settings.ToDate, err = validation.ValidateToFromDate(v); err != nil {
			return err
		}
	}
	if v, ok := getConfigValue[bool](keys.UseGlobalCookies); ok {
		c.Settings.UseGlobalCookies = v
	}
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

	var (
		gpuDirGot, gpuGot string
		videoCodecGot     string
	)

	if v, ok := getConfigValue[string](keys.MExt); ok {
		if _, err := validation.ValidateOutputFiletype(c.Settings.ChannelConfigFile); err != nil {
			return fmt.Errorf("metarr output filetype %q in config file %q is invalid", v, c.Settings.ChannelConfigFile)
		}
		c.MetarrArgs.Ext = v
	}
	if v, ok := getConfigValue[[]string](keys.MFilenameReplaceSuffix); ok {
		c.MetarrArgs.FilenameReplaceSfx, err = validation.ValidateFilenameSuffixReplace(v)
		if err != nil {
			return err
		}
	}
	if v, ok := getConfigValue[string](keys.MRenameStyle); ok {
		if err := validation.ValidateRenameFlag(v); err != nil {
			return err
		}
		c.MetarrArgs.RenameStyle = v
	}
	if v, ok := getConfigValue[string](keys.MInputFileDatePfx); ok {
		if ok := validation.ValidateDateFormat(v); !ok {
			return fmt.Errorf("date format %q in config file %q is invalid", v, c.Settings.ChannelConfigFile)
		}
		c.MetarrArgs.FileDatePfx = v
	}
	if v, ok := getConfigValue[[]string](keys.MMetaOps); ok {
		c.MetarrArgs.MetaOps, err = validation.ValidateMetaOps(v)
		if err != nil {
			return err
		}
	}
	if v, ok := getConfigValue[string](keys.MOutputDir); ok {
		c.MetarrArgs.OutputDir = v // No check due to templating.
	}
	if v, ok := getConfigValue[int](keys.MConcurrency); ok {
		c.MetarrArgs.Concurrency = validation.ValidateConcurrencyLimit(v)
	}
	if v, ok := getConfigValue[float64](keys.MMaxCPU); ok {
		c.MetarrArgs.MaxCPU = v // Handled in Metarr
	}
	if v, ok := getConfigValue[string](keys.MMinFreeMem); ok {
		c.MetarrArgs.MinFreeMem = v // Handled in Metarr
	}
	if v, ok := getConfigValue[string](keys.TranscodeGPU); ok {
		gpuGot = v
	}
	if v, ok := getConfigValue[string](keys.TranscodeGPUDir); ok {
		gpuDirGot = v
	}
	if v, ok := getConfigValue[string](keys.TranscodeVideoFilter); ok {
		c.MetarrArgs.TranscodeVideoFilter, err = validation.ValidateTranscodeVideoFilter(v)
		if err != nil {
			return err
		}
	}
	if v, ok := getConfigValue[string](keys.TranscodeCodec); ok {
		videoCodecGot = v
	}
	if v, ok := getConfigValue[string](keys.TranscodeAudioCodec); ok {
		if c.MetarrArgs.TranscodeAudioCodec, err = validation.ValidateTranscodeAudioCodec(v); err != nil {
			return err
		}
	}
	if v, ok := getConfigValue[string](keys.MTranscodeQuality); ok {
		if c.MetarrArgs.TranscodeQuality, err = validation.ValidateTranscodeQuality(v); err != nil {
			return err
		}
	}

	if gpuGot != "" || gpuDirGot != "" {
		c.MetarrArgs.UseGPU, c.MetarrArgs.GPUDir, err = validation.ValidateGPU(gpuGot, gpuDirGot)
		if err != nil {
			return err
		}
	}

	if c.MetarrArgs.TranscodeCodec, err = validation.ValidateTranscodeCodec(videoCodecGot, c.MetarrArgs.UseGPU); err != nil {
		return err
	}

	return nil
}
