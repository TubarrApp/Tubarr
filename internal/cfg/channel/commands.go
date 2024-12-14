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
	channelCmd.AddCommand(addChannelCmd(cs))
	channelCmd.AddCommand(dlURLs(cs, s, ctx))
	channelCmd.AddCommand(crawlChannelCmd(cs, s, ctx))
	channelCmd.AddCommand(addCrawlToIgnore(cs, s, ctx))
	channelCmd.AddCommand(addURLToIgnore(cs))
	channelCmd.AddCommand(deleteChannelCmd(cs))
	channelCmd.AddCommand(listChannelCmd(cs))
	channelCmd.AddCommand(listAllChannelsCmd(cs))
	channelCmd.AddCommand(updateChannelRow(cs))
	channelCmd.AddCommand(updateChannelSettingsCmd(cs))
	channelCmd.AddCommand(addNotifyURL(cs))

	return channelCmd
}

// dlURLs downloads a list of URLs inputted by the user.
func dlURLs(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		cFile, channelURL, channelName string
		channelID                      int
		urls                           []string
	)

	dlURLFileCmd := &cobra.Command{
		Use:   "get-urls",
		Short: "Download inputted URLs (plaintext or file).",
		Long:  "If using a file, the file should contain one URL per line.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cFile == "" && len(urls) == 0 {
				return errors.New("must enter a URL source")
			}

			key, val, err := getChanKeyVal(channelID, channelName, channelURL)
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

	SetPrimaryChannelFlags(dlURLFileCmd, &channelName, &channelURL, &channelID)
	dlURLFileCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to this channel")
	dlURLFileCmd.Flags().StringSliceVar(&urls, keys.URLAdd, nil, "Enter a list of URLs to download")

	return dlURLFileCmd
}

// addNotifyURL adds a notification URL (can use to send requests to update Plex libraries on new video addition).
func addNotifyURL(cs interfaces.ChannelStore) *cobra.Command {
	var (
		channelName, channelURL string
		channelID               int
		notifyName, notifyURL   string
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
				key, val, err := getChanKeyVal(channelID, channelName, channelURL)
				if err != nil {
					return err
				}

				id, err = cs.GetID(key, val)
				if err != nil {
					return err
				}
			}

			if notifyName == "" {
				notifyName = channelName
			}

			if err := cs.AddNotifyURL(id, notifyName, notifyURL); err != nil {
				return err
			}

			return nil
		},
	}

	// Primary channel elements
	SetPrimaryChannelFlags(addNotifyCmd, &channelName, &channelURL, &channelID)
	addNotifyCmd.Flags().StringVar(&notifyURL, "notify-url", "", "Full notification URL including tokens")
	addNotifyCmd.Flags().StringVar(&notifyName, "notify-name", "", "Provide a custom name for this notification")

	return addNotifyCmd
}

// addURLToIgnore adds a user inputted URL to ignore from crawls.
func addURLToIgnore(cs interfaces.ChannelStore) *cobra.Command {
	var (
		url, name string
		ignoreURL string
		id        int
	)

	ignoreURLCmd := &cobra.Command{
		Use:   "ignore-url",
		Short: "Adds a video URL to ignore.",
		Long:  "URLs added to this list will not be grabbed from channel crawls.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ignoreURL == "" {
				return errors.New("cannot enter the target ignore URL blank")
			}

			key, val, err := getChanKeyVal(id, name, url)
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
	SetPrimaryChannelFlags(ignoreURLCmd, &name, &url, nil)
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// addCrawlToIgnore crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func addCrawlToIgnore(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		url, name string
		id        int
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:   "ignore-crawl",
		Short: "Crawl a channel for URLs to ignore.",
		Long:  "Crawls the current state of a channel page and adds all video URLs to ignore.",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name, url)
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
	SetPrimaryChannelFlags(ignoreCrawlCmd, &name, &url, nil)

	return ignoreCrawlCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		url, name, vDir, jDir, outDir, cookieSource,
		externalDownloader, externalDownloaderArgs, maxFilesize, filenameDateTag, renameStyle, minFreeMem, metarrExt string
		dlFilters, metaOps, fileSfxReplace                 []string
		crawlFreq, concurrency, metarrConcurrency, retries int
		maxCPU                                             float64
	)

	now := time.Now()
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, settings, etc.",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case vDir == "", url == "":
				return errors.New("must enter both a video directory and url")
			}

			// Infer empty fields
			if jDir == "" {
				jDir = vDir
			}

			if name == "" {
				name = url
			}

			// Verify filters
			dlFilters, err := verifyChannelOps(dlFilters)
			if err != nil {
				return err
			}

			if filenameDateTag != "" {
				if !cfgvalidate.DateFormat(filenameDateTag) {
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

			c := &models.Channel{
				URL:      url,
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
				},

				MetarrArgs: models.MetarrArgs{
					Ext:                metarrExt,
					MetaOps:            metaOps,
					FileDatePfx:        filenameDateTag,
					RenameStyle:        renameStyle,
					FilenameReplaceSfx: fileSfxReplace,
					MaxCPU:             maxCPU,
					MinFreeMem:         minFreeMem,
					OutputDir:          outDir,
					Concurrency:        metarrConcurrency,
				},

				LastScan:  now,
				CreatedAt: now,
				UpdatedAt: now,
			}

			if _, err := cs.AddChannel(c); err != nil {
				return err
			}
			return nil
		},
	}

	// Primary channel elements
	SetPrimaryChannelFlags(addCmd, &name, &url, nil)

	// Files/dirs
	cfgflags.SetFileDirFlags(addCmd, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(addCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader)

	// Download
	cfgflags.SetDownloadFlags(addCmd, &retries, &cookieSource, &maxFilesize, &dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(addCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	return addCmd
}

// deleteChannelCmd deletes a channel from the database.
func deleteChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		url, name string
		id        int
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete channels.",
		Long:  "Delete a channel by ID, name, or URL.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" && name == "" {
				return errors.New("must enter both a video directory and url")
			}

			key, val, err := getChanKeyVal(id, name, url)
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
	SetPrimaryChannelFlags(delCmd, &name, &url, &id)

	return delCmd
}

// listAllChannel returns details about a single channel in the database.
func listChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		url, name, key, val string
		err                 error
		channelID           int
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List a channel's details.",
		Long:  "Lists details of a channel in the database.",
		RunE: func(cmd *cobra.Command, args []string) error {

			id := int64(channelID)
			if id == 0 {
				key, val, err = getChanKeyVal(channelID, name, url)
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

			fmt.Printf("\n%sChannel ID: %d%s\nName: %s\nURL: %s\nVideo Directory: %s\nJSON Directory: %s\n", consts.ColorGreen, ch.ID, consts.ColorReset, ch.Name, ch.URL, ch.VideoDir, ch.JSONDir)
			fmt.Printf("Crawl Frequency: %d minutes\nFilters: %v\nConcurrency: %d\nCookie Source: %s\nRetries: %d\n", ch.Settings.CrawlFreq, ch.Settings.Filters, ch.Settings.Concurrency, ch.Settings.CookieSource, ch.Settings.Retries)
			fmt.Printf("External Downloader: %s\nExternal Downloader Args: %s\nMax Filesize: %s\n", ch.Settings.ExternalDownloader, ch.Settings.ExternalDownloaderArgs, ch.Settings.MaxFilesize)
			fmt.Printf("Max CPU: %.2f\nMetarr Concurrency: %d\nMin Free Mem: %s\nOutput Dir: %s\nOutput Filetype: %s\n", ch.MetarrArgs.MaxCPU, ch.MetarrArgs.Concurrency, ch.MetarrArgs.MinFreeMem, ch.MetarrArgs.OutputDir, ch.MetarrArgs.Ext)
			fmt.Printf("Rename Style: %s\nFilename Suffix Replace: %v\nMeta Ops: %v\nFilename Date Format: %s\n", ch.MetarrArgs.RenameStyle, ch.MetarrArgs.FilenameReplaceSfx, ch.MetarrArgs.MetaOps, ch.MetarrArgs.FileDatePfx)

			return nil
		},
	}
	// Primary channel elements
	SetPrimaryChannelFlags(listCmd, &name, &url, &channelID)
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
				fmt.Printf("\n%sChannel ID: %d%s\nName: %s\nURL: %s\nVideo Directory: %s\nJSON Directory: %s\n", consts.ColorGreen, ch.ID, consts.ColorReset, ch.Name, ch.URL, ch.VideoDir, ch.JSONDir)
				fmt.Printf("Crawl Frequency: %d minutes\nFilters: %v\nConcurrency: %d\nCookie Source: %s\nRetries: %d\n", ch.Settings.CrawlFreq, ch.Settings.Filters, ch.Settings.Concurrency, ch.Settings.CookieSource, ch.Settings.Retries)
				fmt.Printf("External Downloader: %s\nExternal Downloader Args: %s\nMax Filesize: %s\n", ch.Settings.ExternalDownloader, ch.Settings.ExternalDownloaderArgs, ch.Settings.MaxFilesize)
				fmt.Printf("Max CPU: %.2f\nMetarr Concurrency: %d\nMin Free Mem: %s\nOutput Dir: %s\nOutput Filetype: %s\n", ch.MetarrArgs.MaxCPU, ch.MetarrArgs.Concurrency, ch.MetarrArgs.MinFreeMem, ch.MetarrArgs.OutputDir, ch.MetarrArgs.Ext)
				fmt.Printf("Rename Style: %s\nFilename Suffix Replace: %v\nMeta Ops: %v\nFilename Date Format: %s\n", ch.MetarrArgs.RenameStyle, ch.MetarrArgs.FilenameReplaceSfx, ch.MetarrArgs.MetaOps, ch.MetarrArgs.FileDatePfx)
			}
			return nil
		},
	}
	return listAllCmd
}

// crawlChannelCmd initiates a crawl of a given channel.
func crawlChannelCmd(cs interfaces.ChannelStore, s interfaces.Store, ctx context.Context) *cobra.Command {
	var (
		url, name string
		id        int
	)

	crawlCmd := &cobra.Command{
		Use:   "crawl",
		Short: "Crawl a channel for new URLs.",
		Long:  "Initiate a crawl for new URLs of a channel that have not yet been downloaded.",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name, url)
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
	SetPrimaryChannelFlags(crawlCmd, &name, &url, &id)

	return crawlCmd
}

// updateChannelSettingsCmd updates channel settings.
func updateChannelSettingsCmd(cs interfaces.ChannelStore) *cobra.Command {
	var (
		id, concurrency, crawlFreq, metarrConcurrency, retries  int
		maxCPU                                                  float64
		vDir, jDir, outDir                                      string
		name, url, cookieSource                                 string
		minFreeMem, renameStyle, filenameDateTag, metarrExt     string
		maxFilesize, externalDownloader, externalDownloaderArgs string
		dlFilters, metaOps                                      []string
		fileSfxReplace                                          []string
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-settings",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name, url)
			if err != nil {
				return err
			}

			// Files/dirs:
			if vDir != "" { // Do not stat, due to templating
				if err := cs.UpdateChannelEntry(key, val, consts.QChanVideoDir, vDir); err != nil {
					return fmt.Errorf("failed to update video directory: %w", err)
				}
				logging.S(0, "Updated video directory to %q", vDir)
			}

			if jDir != "" { // Do not stat, due to templating
				if err := cs.UpdateChannelEntry(key, val, consts.QChanJSONDir, jDir); err != nil {
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
			})
			if err != nil {
				return err
			}

			if len(fnSettingsArgs) > 0 {
				for _, fn := range fnSettingsArgs {
					if _, err := cs.UpdateChannelSettingsJSON(key, val, fn); err != nil {
						return err
					}
				}
			}

			fnMetarrArray, err := getMetarrArgFns(cobraMetarrArgs{
				filenameReplaceSfx: fileSfxReplace,
				renameStyle:        renameStyle,
				fileDatePfx:        filenameDateTag,
				metarrExt:          metarrExt,
				metaOps:            metaOps,
				outputDir:          outDir,
				concurrency:        metarrConcurrency,
				maxCPU:             maxCPU,
				minFreeMem:         minFreeMem,
			})
			if err != nil {
				return err
			}

			if len(fnMetarrArray) > 0 {
				for _, fn := range fnMetarrArray {
					if _, err := cs.UpdateChannelMetarrArgsJSON(key, val, fn); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}

	// Primary channel elements
	SetPrimaryChannelFlags(updateSettingsCmd, &name, &url, &id)

	// Files/dirs
	cfgflags.SetFileDirFlags(updateSettingsCmd, &jDir, &vDir)

	// Program related
	cfgflags.SetProgramRelatedFlags(updateSettingsCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader)

	// Download
	cfgflags.SetDownloadFlags(updateSettingsCmd, &retries, &cookieSource, &maxFilesize, &dlFilters)

	// Metarr
	cfgflags.SetMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	return updateSettingsCmd
}

// updateChannelRow provides a command allowing the alteration of a channel row.
func updateChannelRow(cs interfaces.ChannelStore) *cobra.Command {
	var (
		col, newVal, url, name string
		id                     int
	)

	updateRowCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a channel column.",
		Long:  "Enter a column to update and a value to update that column to.",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name, url)
			if err != nil {
				return err
			}

			if err := verifyChanRowUpdateValid(col, newVal); err != nil {
				return err
			}
			if err := cs.UpdateChannelRow(key, val, col, newVal); err != nil {
				return err
			}
			logging.S(0, "Updated channel column: %q → %q", col, newVal)
			return nil
		},
	}

	// Primary channel elements
	SetPrimaryChannelFlags(updateRowCmd, &name, &url, &id)

	updateRowCmd.Flags().StringVarP(&col, "column-name", "c", "", "The name of the column in the table (e.g. video_directory)")
	updateRowCmd.Flags().StringVarP(&newVal, "value", "v", "", "The value to set in the column (e.g. /my-directory)")
	return updateRowCmd
}
