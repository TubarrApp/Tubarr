// Package cfgchannel sets up Cobra channel commands.
package cfgchannel

import (
	"context"
	"errors"
	"fmt"
	"time"
	cfgflags "tubarr/internal/cfg/flags"
	cfgvalidate "tubarr/internal/cfg/validation"
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
	channelCmd.AddCommand(addChannelCmd(cs))
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
	channelCmd.AddCommand(addNotifyURL(cs))

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

			if err := cs.AddAuth(chanID, authDetails); err != nil {
				return err
			}
			return nil
		},
	}
	SetPrimaryChannelFlags(addAuthCmd, &channelName, nil, &channelID)
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

	SetPrimaryChannelFlags(deleteURLsCmd, &channelName, nil, &channelID)
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
			if cFile == "" && len(urls) == 0 {
				return errors.New("must enter a URL source")
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

	SetPrimaryChannelFlags(dlURLFileCmd, &channelName, nil, &channelID)
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

			if len(notifyURLs) == 0 {
				return errors.New("must enter at least one notify URL to delete")
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
	SetPrimaryChannelFlags(deleteNotifyCmd, &channelName, nil, &channelID)
	deleteNotifyCmd.Flags().StringSliceVar(&notifyURLs, keys.URLs, nil, "Full notification URL including tokens")
	deleteNotifyCmd.Flags().StringSliceVar(&notifyNames, "names", nil, "Full notification names")

	return deleteNotifyCmd
}

// addNotifyURL adds a notification URL (can use to send requests to update Plex libraries on new video addition).
func addNotifyURL(cs interfaces.ChannelStore) *cobra.Command {
	var (
		channelName, notifyName, notifyURL string
		channelID                          int
	)

	addNotifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Adds notify function to a channel.",
		Long:  "Enter a fully qualified notification URL here to send update requests to platforms like Plex etc.",
		RunE: func(cmd *cobra.Command, args []string) error {

			if notifyURL == "" {
				return errors.New("notification URL cannot be blank")
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

			if notifyName == "" {
				notifyName = notifyURL
			}

			if err := cs.AddNotifyURL(id, notifyName, notifyURL); err != nil {
				return err
			}

			return nil
		},
	}

	// Primary channel elements
	SetPrimaryChannelFlags(addNotifyCmd, &channelName, nil, &channelID)
	addNotifyCmd.Flags().StringVar(&notifyURL, "notify-url", "", "Full notification URL including tokens")
	addNotifyCmd.Flags().StringVar(&notifyName, "notify-name", "", "Provide a custom name for this notification")

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
	SetPrimaryChannelFlags(ignoreURLCmd, &name, nil, nil)
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
	SetPrimaryChannelFlags(ignoreCrawlCmd, &name, nil, nil)

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
	SetPrimaryChannelFlags(pauseCmd, &name, nil, &id)

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
	SetPrimaryChannelFlags(pauseCmd, &name, nil, &id)

	return pauseCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		urls []string
		name, vDir, jDir, outDir, cookieSource,
		externalDownloader, externalDownloaderArgs, maxFilesize, filenameDateTag, renameStyle, minFreeMem, metarrExt string
		usernames, passwords, loginURLs                                           []string
		notifyName, notifyURL                                                     string
		fromDate, toDate                                                          string
		dlFilters, metaOps, fileSfxReplace                                        []string
		crawlFreq, concurrency, metarrConcurrency, retries                        int
		maxCPU                                                                    float64
		useGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter string
		pause                                                                     bool
	)

	now := time.Now()
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, settings, etc.",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case vDir == "", len(urls) == 0:
				return errors.New("must enter both a video directory and at least one channel URL source")
			}

			// Infer empty fields
			if jDir == "" {
				jDir = vDir
			}

			if name == "" {
				return fmt.Errorf("must input a name for this channel")
			}

			// Verify filters
			dlFilters, err := verifyChannelOps(dlFilters)
			if err != nil {
				return err
			}

			if filenameDateTag != "" {
				if !cfgvalidate.ValidateDateFormat(filenameDateTag) {
					return errors.New("invalid Metarr filename date tag format")
				}
			}

			if len(metaOps) > 0 {
				if metaOps, err = cfgvalidate.ValidateMetaOps(metaOps); err != nil {
					return err
				}
			}

			if len(fileSfxReplace) > 0 {
				if fileSfxReplace, err = cfgvalidate.ValidateFilenameSuffixReplace(fileSfxReplace); err != nil {
					return err
				}
			}

			if renameStyle != "" {
				if err := cfgvalidate.ValidateRenameFlag(renameStyle); err != nil {
					return err
				}
			}

			if minFreeMem != "" {
				if err := cfgvalidate.ValidateMinFreeMem(minFreeMem); err != nil {
					return err
				}
			}

			if fromDate != "" {
				if fromDate, err = validateToFromDate(fromDate); err != nil {
					return err
				}
			}

			if toDate != "" {
				if toDate, err = validateToFromDate(toDate); err != nil {
					return err
				}
			}

			if useGPU != "" {
				if useGPU, err = validateGPU(useGPU, gpuDir); err != nil {
					return err
				}
			}

			if codec != "" {
				if codec, err = validateTranscodeCodec(codec); err != nil {
					return err
				}
			}

			if audioCodec != "" {
				if audioCodec, err = validateTranscodeAudioCodec(audioCodec); err != nil {
					return err
				}
			}

			if transcodeQuality != "" {
				if transcodeQuality, err = validateTranscodeQuality(transcodeQuality); err != nil {
					return err
				}
			}

			c := &models.Channel{
				URLs:     urls,
				Name:     name,
				VideoDir: vDir,
				JSONDir:  jDir,

				Settings: models.ChannelSettings{
					CrawlFreq:              crawlFreq,
					Filters:                dlFilters,
					Retries:                retries,
					CookieSource:           cookieSource,
					ExternalDownloader:     externalDownloader,
					ExternalDownloaderArgs: externalDownloaderArgs,
					Concurrency:            concurrency,
					MaxFilesize:            maxFilesize,
					FromDate:               fromDate,
					ToDate:                 toDate,
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

			if err := cs.AddAuth(channelID, authDetails); err != nil {
				return err
			}

			if notifyURL != "" && channelID > 0 {
				if notifyName == "" {
					notifyName = notifyURL
				}

				if err := cs.AddNotifyURL(channelID, notifyName, notifyURL); err != nil {
					return err
				}
			}
			return nil
		},
	}

	// Primary channel elements
	SetPrimaryChannelFlags(addCmd, &name, &urls, nil)

	// Files/dirs
	cfgflags.SetFileDirFlags(addCmd, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(addCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader)

	// Download
	cfgflags.SetDownloadFlags(addCmd, &retries, &cookieSource, &maxFilesize, &dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(addCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	// Login credentials
	cfgflags.SetAuthFlags(addCmd, &usernames, &passwords, &loginURLs)

	// Transcoding
	cfgflags.SetTranscodeFlags(addCmd, &useGPU, &gpuDir, &transcodeVideoFilter, &codec, &audioCodec, &transcodeQuality)

	// Notification URL
	addCmd.Flags().StringVar(&notifyURL, "notify-url", "", "Full notification URL including tokens")
	addCmd.Flags().StringVar(&notifyName, "notify-name", "", "Provide a custom name for this notification")

	addCmd.Flags().StringVar(&fromDate, "from-date", "", "Only grab videos uploaded on or after this date")
	addCmd.Flags().StringVar(&toDate, "to-date", "", "Only grab videos uploaded up to this date")

	addCmd.Flags().BoolVar(&pause, "pause", false, "Paused channels won't crawl videos on a normal program run")

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
	SetPrimaryChannelFlags(delCmd, &name, &urls, &id)

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

			ch, err, hasRows := cs.FetchChannel(id)
			if !hasRows {
				logging.I("Entry for channel with ID %d does not exist in the database", id)
				return nil
			}
			if err != nil {
				return err
			}

			fmt.Printf("\n%sChannel ID: %d%s\nName: %s\nURLs: %+v\nVideo Directory: %s\nJSON Directory: %s\n", consts.ColorGreen, ch.ID, consts.ColorReset, ch.Name, ch.URLs, ch.VideoDir, ch.JSONDir)
			fmt.Printf("Crawl Frequency: %d minutes\nFilters: %v\nConcurrency: %d\nCookie Source: %s\nRetries: %d\n", ch.Settings.CrawlFreq, ch.Settings.Filters, ch.Settings.Concurrency, ch.Settings.CookieSource, ch.Settings.Retries)
			fmt.Printf("External Downloader: %s\nExternal Downloader Args: %s\nMax Filesize: %s\n", ch.Settings.ExternalDownloader, ch.Settings.ExternalDownloaderArgs, ch.Settings.MaxFilesize)
			fmt.Printf("Max CPU: %.2f\nMetarr Concurrency: %d\nMin Free Mem: %s\nOutput Dir: %s\nOutput Filetype: %s\nHW accel type: %s\n", ch.MetarrArgs.MaxCPU, ch.MetarrArgs.Concurrency, ch.MetarrArgs.MinFreeMem, ch.MetarrArgs.OutputDir, ch.MetarrArgs.Ext, ch.MetarrArgs.UseGPU)
			fmt.Printf("Rename Style: %s\nFilename Suffix Replace: %v\nMeta Ops: %v\nFilename Date Format: %s\n", ch.MetarrArgs.RenameStyle, ch.MetarrArgs.FilenameReplaceSfx, ch.MetarrArgs.MetaOps, ch.MetarrArgs.FileDatePfx)
			fmt.Printf("From Date (yyyy-mm-dd): %q\nTo Date (yyyy-mm-dd): %q\n", hyphenateYyyyMmDd(ch.Settings.FromDate), hyphenateYyyyMmDd(ch.Settings.ToDate))
			fmt.Printf("Paused?: %v\n", ch.Paused)

			return nil
		},
	}
	// Primary channel elements
	SetPrimaryChannelFlags(listCmd, &name, nil, &channelID)
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
				fmt.Printf("\n%sChannel ID: %d%s\nName: %s\nURL: %+v\nVideo Directory: %s\nJSON Directory: %s\n", consts.ColorGreen, ch.ID, consts.ColorReset, ch.Name, ch.URLs, ch.VideoDir, ch.JSONDir)
				fmt.Printf("Crawl Frequency: %d minutes\nFilters: %v\nConcurrency: %d\nCookie Source: %s\nRetries: %d\n", ch.Settings.CrawlFreq, ch.Settings.Filters, ch.Settings.Concurrency, ch.Settings.CookieSource, ch.Settings.Retries)
				fmt.Printf("External Downloader: %s\nExternal Downloader Args: %s\nMax Filesize: %s\n", ch.Settings.ExternalDownloader, ch.Settings.ExternalDownloaderArgs, ch.Settings.MaxFilesize)
				fmt.Printf("Max CPU: %.2f\nMetarr Concurrency: %d\nMin Free Mem: %s\nOutput Dir: %s\nOutput Filetype: %s\nHW accel type: %s\n", ch.MetarrArgs.MaxCPU, ch.MetarrArgs.Concurrency, ch.MetarrArgs.MinFreeMem, ch.MetarrArgs.OutputDir, ch.MetarrArgs.Ext, ch.MetarrArgs.UseGPU)
				fmt.Printf("Rename Style: %s\nFilename Suffix Replace: %v\nMeta Ops: %v\nFilename Date Format: %s\n", ch.MetarrArgs.RenameStyle, ch.MetarrArgs.FilenameReplaceSfx, ch.MetarrArgs.MetaOps, ch.MetarrArgs.FileDatePfx)
				fmt.Printf("From Date (yyyy-mm-dd): %q\nTo Date (yyyy-mm-dd): %q\n", hyphenateYyyyMmDd(ch.Settings.FromDate), hyphenateYyyyMmDd(ch.Settings.ToDate))
				fmt.Printf("Paused?: %v\n", ch.Paused)
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
	SetPrimaryChannelFlags(crawlCmd, &name, nil, &id)

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
		fileSfxReplace                                                            []string
		useGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter string
		fromDate, toDate                                                          string
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-settings",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		RunE: func(cmd *cobra.Command, args []string) error {

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

			// Settings
			fnSettingsArgs, err := getSettingsArgFns(chanSettings{
				cookieSource:           cookieSource,
				crawlFreq:              crawlFreq,
				retries:                retries,
				filters:                dlFilters,
				externalDownloader:     externalDownloader,
				externalDownloaderArgs: externalDownloaderArgs,
				concurrency:            concurrency,
				maxFilesize:            maxFilesize,
				fromDate:               fromDate,
				toDate:                 toDate,
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

			if useGPU != "" {
				if useGPU, err = validateGPU(useGPU, gpuDir); err != nil {
					return err
				}
			}

			if codec != "" {
				if codec, err = validateTranscodeCodec(codec); err != nil {
					return err
				}
			}

			if audioCodec != "" {
				if audioCodec, err = validateTranscodeAudioCodec(audioCodec); err != nil {
					return err
				}
			}

			if transcodeQuality != "" {
				if transcodeQuality, err = validateTranscodeQuality(transcodeQuality); err != nil {
					return err
				}
			}

			fnMetarrArray, err := getMetarrArgFns(cobraMetarrArgs{
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
	SetPrimaryChannelFlags(updateSettingsCmd, &name, &urls, &id)

	// Files/dirs
	cfgflags.SetFileDirFlags(updateSettingsCmd, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(updateSettingsCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader)

	// Download
	cfgflags.SetDownloadFlags(updateSettingsCmd, &retries, &cookieSource, &maxFilesize, &dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	// Transcoding
	cfgflags.SetTranscodeFlags(updateSettingsCmd, &useGPU, &gpuDir, &transcodeVideoFilter, &codec, &audioCodec, &transcodeQuality)

	// Auth
	cfgflags.SetAuthFlags(updateSettingsCmd, &username, &password, &loginURL)

	updateSettingsCmd.Flags().StringVar(&fromDate, "from-date", "", "Only grab videos uploaded on or after this date")
	updateSettingsCmd.Flags().StringVar(&toDate, "to-date", "", "Only grab videos uploaded up to this date")

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
	SetPrimaryChannelFlags(updateRowCmd, &name, nil, &id)

	updateRowCmd.Flags().StringVarP(&col, "column-name", "c", "", "The name of the column in the table (e.g. video_directory)")
	updateRowCmd.Flags().StringVarP(&newVal, "value", "v", "", "The value to set in the column (e.g. /my-directory)")
	return updateRowCmd
}
