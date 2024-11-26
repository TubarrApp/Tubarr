package cfg

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// InitChannelCmds is the entrypoint for initializing channel commands.
func initChannelCmds(s models.Store) *cobra.Command {
	channelCmd := &cobra.Command{
		Use:   "channel",
		Short: "Channel commands",
		Long:  "Manage channels with various subcommands like add, delete, and list.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	cs := s.GetChannelStore()

	// Add subcommands with dependencies
	channelCmd.AddCommand(addChannelCmd(cs))
	channelCmd.AddCommand(dlURLs(cs, s))
	channelCmd.AddCommand(crawlChannelCmd(cs, s))
	channelCmd.AddCommand(addCrawlToIgnore(cs, s))
	channelCmd.AddCommand(addURLToIgnore(cs))
	channelCmd.AddCommand(deleteChannelCmd(cs))
	channelCmd.AddCommand(listChannelCmd(cs))
	channelCmd.AddCommand(updateChannelRow(cs))
	channelCmd.AddCommand(updateChannelSettingsCmd(cs))
	channelCmd.AddCommand(addNotifyURL(cs))

	return channelCmd
}

// dlURLs downloads a list of URLs inputted by the user.
func dlURLs(cs models.ChannelStore, s models.Store) *cobra.Command {
	var (
		cFile, channelURL, channelName string
		channelID                      int
		urls                           []string
	)

	dlURLFileCmd := &cobra.Command{
		Use:   "get-urls",
		Short: "Download input URLs (plaintext or file)",
		Long:  "Enter a file containing URLs, one per line, to download them to the channel",
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

			if err := cs.CrawlChannel(key, val, s); err != nil {
				return err
			}
			return nil
		},
	}

	setPrimaryChannelFlags(dlURLFileCmd, &channelName, &channelURL, &channelID)
	dlURLFileCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to this channel")
	dlURLFileCmd.Flags().StringSliceVar(&urls, keys.URLAdd, nil, "Enter a list of URLs to download")

	return dlURLFileCmd
}

// addNotifyURL adds a notification URL (can use to send requests to update Plex libraries on new video addition).
func addNotifyURL(cs models.ChannelStore) *cobra.Command {
	var (
		channelName, channelURL string
		channelID               int
		notifyName, notifyURL   string
	)

	addNotifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Adds notify function to a channel",
		Long:  "Enter a fully qualified notification URL here to send update requests to platforms like Plex etc.",
		RunE: func(cmd *cobra.Command, args []string) error {

			if notifyURL == "" {
				return errors.New("notification URL cannot be blank")
			}

			key, val, err := getChanKeyVal(channelID, channelName, channelURL)
			if err != nil {
				return err
			}

			if notifyName == "" {
				notifyName = channelName
			}

			id, err := cs.GetID(key, val)
			if err != nil {
				return err
			}

			if err := cs.AddNotifyURL(id, notifyName, notifyURL); err != nil {
				return err
			}

			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(addNotifyCmd, &channelName, &channelURL, &channelID)
	addNotifyCmd.Flags().StringVar(&notifyURL, "notify-url", "", "Full notification URL including tokens")
	addNotifyCmd.Flags().StringVar(&notifyName, "notify-name", "", "Provide a custom name for this notification")

	return addNotifyCmd
}

// addURLToIgnore adds a user inputted URL to ignore from crawls.
func addURLToIgnore(cs models.ChannelStore) *cobra.Command {
	var (
		url, name string
		ignoreURL string
		id        int
	)

	ignoreURLCmd := &cobra.Command{
		Use:   "ignore",
		Short: "Adds a channel video URL to ignore",
		Long:  "URLs added to this list will not be grabbed from channel crawls",
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
	setPrimaryChannelFlags(ignoreURLCmd, &name, &url, nil)
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// addCrawlToIgnore crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func addCrawlToIgnore(cs models.ChannelStore, s models.Store) *cobra.Command {
	var (
		url, name string
		id        int
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:   "ignore-current",
		Short: "Crawl a channel for URLs to ignore",
		Long:  "Crawls the current state of a channel page and adds all video URLs to ignore",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name, url)
			if err != nil {
				return err
			}

			if err := cs.CrawlChannelIgnore(key, val, s); err != nil {
				return err
			}
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(ignoreCrawlCmd, &name, &url, nil)

	return ignoreCrawlCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(cs models.ChannelStore) *cobra.Command {
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
		Short: "Add a channel",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, etc.",
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
				if !dateFormat(filenameDateTag) {
					return errors.New("invalid Metarr filename date tag format")
				}
			}

			if len(metaOps) > 0 {
				if metaOps, err = validateMetaOps(metaOps); err != nil {
					return err
				}
			}

			var fileSfxReplaceStr string
			if len(fileSfxReplace) > 0 {
				if fileSfxReplaceStr, err = validateFilenameSuffixReplace(fileSfxReplace); err != nil {
					return err
				}
			}

			if renameStyle != "" {
				if err := validateRenameFlag(renameStyle); err != nil {
					return err
				}
			}

			if minFreeMem != "" {
				if err := verifyMinFreeMem(minFreeMem); err != nil {
					return err
				}
			}

			c := &models.Channel{
				URL:  url,
				Name: name,
				VDir: vDir,
				JDir: jDir,

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
					FilenameReplaceSfx: fileSfxReplaceStr,
					MaxCPU:             maxCPU,
					MinFreeMem:         minFreeMem,
					OutputDir:          outDir,
					Concurrency:        metarrConcurrency,
				},

				LastScan:  now,
				CreatedAt: now,
				UpdatedAt: now,
			}

			id, err := cs.AddChannel(c)
			if err != nil {
				return err
			}

			fmt.Println()
			logging.S(0, "Successfully added channel (ID: %d)\n\nName: %s\nURL: %s\nCrawl Frequency: %d minutes\nFilters: %v\nSettings: %v\nMetarr Operations: %v",
				id, c.Name, c.URL, c.Settings.CrawlFreq, c.Settings.Filters, c.Settings, c.MetarrArgs)
			fmt.Println()
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(addCmd, &name, &url, nil)

	// Files/dirs
	setFileDirFlags(addCmd, &jDir, &vDir)

	// Program related
	setProgramRelatedFlags(addCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader)

	// Download
	setDownloadFlags(addCmd, &retries, &cookieSource, &maxFilesize, &dlFilters)

	// Metarr
	setMetarrFlags(addCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	return addCmd
}

// deleteChannelCmd deletes a channel from the database.
func deleteChannelCmd(cs models.ChannelStore) *cobra.Command {
	var (
		url, name string
		id        int
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete channels",
		Long:  "Delete a channel by name or URL",
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
	setPrimaryChannelFlags(delCmd, &name, &url, &id)

	return delCmd
}

// listChannelCmd returns a list of channels in the database.
func listChannelCmd(cs models.ChannelStore) *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all channels",
		Long:  "Lists all channels currently saved in the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			chans, err, hasRows := cs.ListChannels()
			if !hasRows {
				logging.I("No entries in the database")
				return nil
			}
			if err != nil {
				return err
			}

			for _, ch := range chans {
				fmt.Printf("\n%sChannel ID: %d%s\nName: %s\nURL: %s\nVideo Directory: %s\nJSON Directory: %s\n", consts.ColorGreen, ch.ID, consts.ColorReset, ch.Name, ch.URL, ch.VDir, ch.JDir)
				fmt.Printf("Crawl Frequency: %d minutes\nFilters: %v\nConcurrency: %d\nCookie Source: %s\nRetries: %d\n", ch.Settings.CrawlFreq, ch.Settings.Filters, ch.Settings.Concurrency, ch.Settings.CookieSource, ch.Settings.Retries)
				fmt.Printf("External Downloader: %s\nExternal Downloader Args: %s\nMax Filesize: %s\n", ch.Settings.ExternalDownloader, ch.Settings.ExternalDownloaderArgs, ch.Settings.MaxFilesize)
				fmt.Printf("Max CPU: %.2f\nMetarr Concurrency: %d\nMin Free Mem: %s\nOutput Dir: %s\n", ch.MetarrArgs.MaxCPU, ch.MetarrArgs.Concurrency, ch.MetarrArgs.MinFreeMem, ch.MetarrArgs.OutputDir)
				fmt.Printf("Rename Style: %s\nFilename Suffix Replace: %v\nMeta Ops: %v\nFilename Date Format: %s\n", ch.MetarrArgs.RenameStyle, ch.MetarrArgs.FilenameReplaceSfx, ch.MetarrArgs.MetaOps, ch.MetarrArgs.FileDatePfx)
			}
			return nil
		},
	}
	return listCmd
}

// crawlChannelCmd initiates a crawl of a given channel.
func crawlChannelCmd(cs models.ChannelStore, s models.Store) *cobra.Command {
	var (
		url, name string
		id        int
	)

	crawlCmd := &cobra.Command{
		Use:   "crawl",
		Short: "Crawl a channel for new URLs",
		Long:  "Initiate a crawl for new URLs of a channel that have not yet been downloaded",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name, url)
			if err != nil {
				return err
			}

			if err := cs.CrawlChannel(key, val, s); err != nil {
				return err
			}
			return nil
		},
	}

	// Primary channel elements
	setPrimaryChannelFlags(crawlCmd, &name, &url, &id)

	return crawlCmd
}

// updateChannelSettingsCmd updates channel settings.
func updateChannelSettingsCmd(cs models.ChannelStore) *cobra.Command {
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
		Short: "Update channel settings",
		RunE: func(cmd *cobra.Command, args []string) error {

			key, val, err := getChanKeyVal(id, name, url)
			if err != nil {
				return err
			}

			// Files/dirs:
			if vDir != "" { // Do not stat, due to templating
				if err := cs.UpdateChannelEntry(key, val, consts.QChanVDir, vDir); err != nil {
					return fmt.Errorf("failed to update video directory: %w", err)
				}
				logging.S(0, "Updated video directory to %q", vDir)
			}

			if jDir != "" { // Do not stat, due to templating
				if err := cs.UpdateChannelEntry(key, val, consts.QChanJDir, jDir); err != nil {
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
	setPrimaryChannelFlags(updateSettingsCmd, &name, &url, &id)

	// Files/dirs
	setFileDirFlags(updateSettingsCmd, &jDir, &vDir)

	// Program related
	setProgramRelatedFlags(updateSettingsCmd, &concurrency, &crawlFreq, &externalDownloaderArgs, &externalDownloader)

	// Download
	setDownloadFlags(updateSettingsCmd, &retries, &cookieSource, &maxFilesize, &dlFilters)

	// Metarr
	setMetarrFlags(updateSettingsCmd, &maxCPU, &metarrConcurrency, &metarrExt, &filenameDateTag, &minFreeMem, &outDir, &renameStyle, &fileSfxReplace, &metaOps)

	return updateSettingsCmd
}

// updateChannelRow provides a command allowing the alteration of a channel row.
func updateChannelRow(cs models.ChannelStore) *cobra.Command {
	var (
		col, newVal, url, name string
		id                     int
	)

	updateRowCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a channel column",
		Long:  "Enter a column to update and a value to update that column to",
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
	setPrimaryChannelFlags(updateRowCmd, &name, &url, &id)

	updateRowCmd.Flags().StringVarP(&col, "column-name", "c", "", "The name of the column in the table (e.g. video_directory)")
	updateRowCmd.Flags().StringVarP(&newVal, "value", "v", "", "The value to set in the column (e.g. /my-directory)")
	return updateRowCmd
}

// Private //////////////////////////////////////////////////////////////////////////////////////////

// verifyChanRowUpdateValid verifies that your update operation is valid
func verifyChanRowUpdateValid(col, val string) error {
	switch col {
	case "url", "name", "video_directory", "json_directory":
		if val == "" {
			return fmt.Errorf("cannot set %s blank, please use the 'delete' function if you want to remove this channel entirely", col)
		}
	default:
		return errors.New("cannot set a custom value for internal DB elements")
	}
	return nil
}

// verifyChannelOps verifies that the user inputted filters are valid
func verifyChannelOps(ops []string) ([]models.DLFilters, error) {

	var filters = make([]models.DLFilters, 0, len(ops))
	for _, op := range ops {
		split := strings.Split(op, ":")
		if len(split) < 3 {
			return nil, errors.New("please enter filters in the format 'field:filter_type:value' (e.g. 'title:omit:frogs' ignores videos with frogs in the metatitle)")
		}
		switch len(split) {
		case 3:
			switch split[1] {
			case "contains", "omit":
				filters = append(filters, models.DLFilters{
					Field: split[0],
					Type:  split[1],
					Value: split[2],
				})
			default:
				return nil, errors.New("please enter a filter type of either 'contains' or 'omit'")
			}
		case 2:
			switch split[1] {
			case "contains", "omit":
				filters = append(filters, models.DLFilters{
					Field: split[0],
					Type:  split[1],
				})
			default:
				return nil, errors.New("please enter a filter type of either 'contains' or 'omit'")
			}
		default:
			return nil, errors.New("invalid filter. Valid examples: 'title:contains:frogs','date:omit' (contains only metatitles with frogs, and omits downloads including a date metafield)")

		}
	}
	return filters, nil
}
