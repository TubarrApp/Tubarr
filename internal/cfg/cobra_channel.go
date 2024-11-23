package cfg

import (
	"fmt"
	"strconv"
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
			return fmt.Errorf("please specify a subcommand. Use --help to see available subcommands")
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
		cFile, channelURL, channelName, key, val string
		urls                                     []string
	)

	dlURLFileCmd := &cobra.Command{
		Use:   "get-urls",
		Short: "Download input URLs (plaintext or file)",
		Long:  "Enter a file containing URLs, one per line, to download them to the channel",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cFile == "" && len(urls) == 0 {
				return fmt.Errorf("must enter a URL source")
			}

			switch {
			case channelURL != "":
				key = consts.QChanURL
				val = channelURL
			case channelName != "":
				key = consts.QChanName
				val = channelName
			default:
				return fmt.Errorf("must enter a channel name or channel URL")
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

	dlURLFileCmd.Flags().StringVarP(&channelURL, "channel-url", "u", "", "Channel URL")
	dlURLFileCmd.Flags().StringVarP(&channelName, "channel-name", "n", "", "Channel name")
	dlURLFileCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to this channel")
	dlURLFileCmd.Flags().StringSliceVar(&urls, keys.URLAdd, nil, "Enter a list of URLs to download")

	return dlURLFileCmd
}

// addNotifyURL adds a notification URL (can use to send requests to update Plex libraries on new video addition).
func addNotifyURL(cs models.ChannelStore) *cobra.Command {
	var (
		channelName, channelURL string
		notifyURL               string
	)

	addNotifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Adds notify function to a channel",
		Long:  "Enter a fully qualified notification URL here to send update requests to platforms like Plex etc.",
		RunE: func(cmd *cobra.Command, args []string) error {

			if notifyURL == "" {
				return fmt.Errorf("notification URL cannot be blank")
			}

			var key, val, notifyName string

			switch {
			case channelURL != "":
				key = consts.QChanURL
				val = channelURL
				if notifyName == "" {
					notifyName = channelURL
				}
			case channelName != "":
				key = consts.QChanName
				val = channelName
				if notifyName == "" {
					notifyName = channelName
				}
			default:
				return fmt.Errorf("must enter a channel name or channel URL")
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
	addNotifyCmd.Flags().StringVarP(&channelURL, "channel-url", "u", "", "Channel URL")
	addNotifyCmd.Flags().StringVarP(&channelName, "channel-name", "n", "", "Channel name")
	addNotifyCmd.Flags().StringVar(&notifyURL, "notify-url", "", "Full notification URL including tokens")

	return addNotifyCmd
}

// addURLToIgnore adds a user inputted URL to ignore from crawls.
func addURLToIgnore(cs models.ChannelStore) *cobra.Command {
	var (
		url, name, key, val string
		ignoreURL           string
	)

	ignoreURLCmd := &cobra.Command{
		Use:   "ignore",
		Short: "Adds a channel video URL to ignore",
		Long:  "URLs added to this list will not be grabbed from channel crawls",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ignoreURL == "" {
				return fmt.Errorf("cannot enter the target ignore URL blank")
			}
			switch {
			case url != "":
				key = consts.QChanURL
				val = url
			case name != "":
				key = consts.QChanName
				val = name
			default:
				return fmt.Errorf("please enter either a channel URL or name")
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
	ignoreURLCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	ignoreURLCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// addCrawlToIgnore crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func addCrawlToIgnore(cs models.ChannelStore, s models.Store) *cobra.Command {
	var (
		url, name string
		id        int
		key, val  string
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:   "ignore-current",
		Short: "Crawl a channel for URLs to ignore",
		Long:  "Crawls the current state of a channel page and adds all video URLs to ignore",
		RunE: func(cmd *cobra.Command, args []string) error {

			switch {
			case url != "":
				key = consts.QChanURL
				val = url
			case name != "":
				key = consts.QChanName
				val = name
			case id != 0:
				key = consts.QChanID
				val = strconv.Itoa(id)
			default:
				return fmt.Errorf("please enter either a URL or name")
			}

			if err := cs.CrawlChannelIgnore(key, val, s); err != nil {
				return err
			}
			return nil
		},
	}

	// Primary channel elements
	ignoreCrawlCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	ignoreCrawlCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")
	ignoreCrawlCmd.Flags().IntVarP(&id, "id", "i", 0, "Channel ID in the DB")

	return ignoreCrawlCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(cs models.ChannelStore) *cobra.Command {
	var (
		url, name, vDir, jDir, cookieSource,
		externalDownloader, externalDownloaderArgs, dateFmt, renameFlag string
		filterInput, metaOps, fileSfxReplace []string
		crawlFreq, concurrency               int
	)

	now := time.Now()
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel",
		Long:  "Add channel adds a new channel to the database using inputted URLs, names, etc.",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case vDir == "", url == "":
				return fmt.Errorf("must enter both a video directory and url")
			}

			// Infer empty fields
			if jDir == "" {
				jDir = vDir
			}

			if name == "" {
				name = url
			}

			// Verify filters
			filters, err := verifyChannelOps(filterInput)
			if err != nil {
				return err
			}

			if dateFmt != "" {
				if !dateFormat(dateFmt) {
					return fmt.Errorf("invalid Metarr filename date tag format")
				}
			}

			if len(metaOps) > 0 {
				if metaOps, err = validateMetaOps(metaOps); err != nil {
					return err
				}
			}

			if len(fileSfxReplace) > 0 {
				if fileSfxReplace, err = validateFilenameSuffixReplace(fileSfxReplace); err != nil {
					return err
				}
			}

			if renameFlag != "" {
				if err := setRenameFlag(renameFlag); err != nil {
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
					Filters:                filters,
					CookieSource:           cookieSource,
					ExternalDownloader:     externalDownloader,
					ExternalDownloaderArgs: externalDownloaderArgs,
					Concurrency:            concurrency,
				},

				MetarrArgs: models.MetarrArgs{
					MetaOps:            metaOps,
					FileDatePfx:        dateFmt,
					RenameStyle:        renameFlag,
					FilenameReplaceSfx: strings.Join(fileSfxReplace, ","),
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
	addCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	addCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")
	addCmd.Flags().StringVarP(&vDir, keys.VideoDir, "v", "", "Output directory (videos will be saved here)")
	addCmd.Flags().StringVarP(&jDir, keys.JSONDir, "j", "", "Output directory (JSON metafiles will be saved here)")

	// Channel settings
	addCmd.Flags().IntVar(&crawlFreq, keys.CrawlFreq, 30, "How often to check for new videos (in minutes)")
	addCmd.Flags().IntVarP(&concurrency, keys.Concurrency, "l", 3, "Maximum concurrent videos to download/process for this channel")
	addCmd.Flags().StringSliceVar(&filterInput, keys.FilterOpsInput, []string{}, "Filter video downloads (e.g. 'title:contains:frogs' ignores downloads without 'frogs' in the title metafield)")
	addCmd.Flags().StringVar(&cookieSource, keys.CookieSource, "", "Please enter the browser to grab cookies from for sites requiring authentication (e.g. 'firefox')")
	addCmd.Flags().StringVar(&externalDownloader, keys.ExternalDownloader, "", "External downloader option (e.g. 'aria2c')")
	addCmd.Flags().StringVar(&externalDownloaderArgs, keys.ExternalDownloaderArgs, "", "External downloader arguments (e.g. '\"-x 16 -s 16\"')")

	// Metarr operations
	addCmd.Flags().StringSliceVar(&fileSfxReplace, keys.InputFilenameReplaceSfx, nil, "Replace a filename suffix element in Metarr")
	addCmd.Flags().StringVar(&renameFlag, keys.RenameStyle, "", "Rename style for Metarr (e.g. 'spaces')")
	addCmd.Flags().StringVar(&dateFmt, keys.InputFileDatePfx, "", "Prefix a filename with a particular date tag (ymd format where Y means yyyy and y means yy)")
	addCmd.Flags().StringSliceVar(&metaOps, keys.MetaOps, nil, "Meta operations to perform in Metarr")

	return addCmd
}

// deleteChannelCmd deletes a channel from the database.
func deleteChannelCmd(cs models.ChannelStore) *cobra.Command {
	var (
		url, name string
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete channels",
		Long:  "Delete a channel by name or URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" && name == "" {
				return fmt.Errorf("must enter both a video directory and url")
			}

			var key, val string
			if url != "" {
				key = consts.QChanURL
				val = url
			} else if name != "" {
				key = consts.QChanName
				val = name
			}

			if err := cs.DeleteChannel(key, val); err != nil {
				return err
			}
			logging.S(0, "Successfully deleted channel with key %q and value %q", key, val)
			return nil
		},
	}

	// Primary channel elements
	delCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	delCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")

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

			for i := range chans {
				fmt.Printf("\nChannel ID: %d\n\nName: %s\nURL: %s\nVideo Directory: %s\nJSON Directory: %s\nCrawl Frequency: %d minutes\nFilters: %v\n",
					chans[i].ID, chans[i].Name, chans[i].URL, chans[i].VDir, chans[i].JDir, chans[i].Settings.CrawlFreq, chans[i].Settings.Filters)

				// Display Metarr operations if they exist
				if len(chans[i].MetarrArgs.MetaOps) > 0 {
					fmt.Printf("Metarr Operations:\n")
					for _, op := range chans[i].MetarrArgs.MetaOps {
						fmt.Printf("  - %s\n", op)
					}
				}
				if chans[i].MetarrArgs.FileDatePfx != "" {
					fmt.Printf("Filename Date Format: %s\n", chans[i].MetarrArgs.FileDatePfx)
				}
				if chans[i].MetarrArgs.RenameStyle != "" {
					fmt.Printf("Rename Style: %s\n", chans[i].MetarrArgs.RenameStyle)
				}
				if chans[i].MetarrArgs.FilenameReplaceSfx != "" {
					fmt.Printf("Filename Suffix Replace: %s\n", chans[i].MetarrArgs.FilenameReplaceSfx)
				}
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
		key, val  string
	)

	crawlCmd := &cobra.Command{
		Use:   "crawl",
		Short: "Crawl a channel for new URLs",
		Long:  "Initiate a crawl for new URLs of a channel that have not yet been downloaded",
		RunE: func(cmd *cobra.Command, args []string) error {

			switch {
			case url != "":
				key = consts.QChanURL
				val = url
			case name != "":
				key = consts.QChanName
				val = name
			case id != 0:
				key = consts.QChanID
				val = strconv.Itoa(id)
			default:
				return fmt.Errorf("please enter either a URL or name")
			}

			if err := cs.CrawlChannel(key, val, s); err != nil {
				return err
			}
			return nil
		},
	}

	// Primary channel elements
	crawlCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	crawlCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")
	crawlCmd.Flags().IntVarP(&id, "id", "i", 0, "Channel ID in the DB")

	return crawlCmd
}

// updateChannelSettingsCmd updates channel settings.
func updateChannelSettingsCmd(cs models.ChannelStore) *cobra.Command {
	var (
		id, concurrency, crawlFreq int
		url, name, key, val        string
		vDir, jDir, outDir         string
		downloadCmd, downloadArgs  string
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-settings",
		Short: "Update channel settings",
		RunE: func(cmd *cobra.Command, args []string) error {

			switch {
			case url != "":
				key = consts.QChanURL
				val = url
			case name != "":
				key = consts.QChanName
				val = name
			case id != 0:
				key = consts.QChanID
				val = strconv.Itoa(id)
			default:
				return fmt.Errorf("please enter either a URL or name")
			}

			if vDir != "" {
				if err := cs.UpdateChannelEntry(key, val, consts.QChanVDir, vDir); err != nil {
					return fmt.Errorf("failed to update video directory: %w", err)
				}
				logging.S(0, "Updated video directory to %q", vDir)
			}

			if jDir != "" {
				if err := cs.UpdateChannelEntry(key, val, consts.QChanJDir, jDir); err != nil {
					return fmt.Errorf("failed to update JSON directory: %w", err)
				}
				logging.S(0, "Updated JSON directory to %q", jDir)
			}

			if concurrency != 0 {
				if err := cs.UpdateConcurrencyLimit(key, val, concurrency); err != nil {
					return fmt.Errorf("failed to update concurrency limit: %w", err)
				}
				logging.S(0, "Updated concurrency to %d minutes", concurrency)
			}

			if crawlFreq != 0 {
				if err := cs.UpdateCrawlFrequency(key, val, crawlFreq); err != nil {
					return fmt.Errorf("failed to update crawl frequency: %w", err)
				}
				logging.S(0, "Updated crawl frequency to %d minutes", crawlFreq)
			}

			if outDir != "" {
				if err := cs.UpdateMetarrOutputDir(key, val, outDir); err != nil {
					return fmt.Errorf("failed to update Metarr output directory: %w", err)
				}
				logging.S(0, "Updated Metarr output directory to %q", outDir)
			}

			if downloadCmd != "" {
				if err := cs.UpdateExternalDownloader(key, val, downloadCmd, downloadArgs); err != nil {
					return fmt.Errorf("failed to update external downloader settings: %w", err)
				}
				logging.S(0, "Updated external downloader settings")
			}

			return nil
		},
	}

	// Primary channel elements
	updateSettingsCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	updateSettingsCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")
	updateSettingsCmd.Flags().IntVarP(&id, "id", "i", 0, "Channel ID in the DB")

	// Edits:
	// Files/dirs
	updateSettingsCmd.Flags().StringVar(&vDir, keys.VideoDir, "", "This is where videos for this channel will be saved (some {{}} templating commands available)")
	updateSettingsCmd.Flags().StringVar(&jDir, keys.JSONDir, "", "This is where JSON files for this channel will be saved (some {{}} templating commands available)")
	updateSettingsCmd.Flags().StringVar(&outDir, "metarr-output-dir", "", "Metarr will move files to this location on completion (some {{}} templating commands available)")

	// Program related
	updateSettingsCmd.Flags().IntVarP(&concurrency, keys.Concurrency, "l", 0, "Maximum concurrent videos to download/process for this channel")
	updateSettingsCmd.Flags().IntVar(&crawlFreq, "crawl-freq", 0, "New crawl frequency in minutes")
	updateSettingsCmd.Flags().StringVar(&downloadCmd, "downloader", "", "External downloader command")
	updateSettingsCmd.Flags().StringVar(&downloadArgs, "downloader-args", "", "External downloader arguments")

	return updateSettingsCmd
}

// updateChannelRow provides a command allowing the alteration of a channel row.
func updateChannelRow(cs models.ChannelStore) *cobra.Command {
	var (
		col, newVal, url, name string
		id                     int

		key, val string
	)

	updateRowCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a channel column",
		Long:  "Enter a column to update and a value to update that column to",
		RunE: func(cmd *cobra.Command, args []string) error {

			switch {
			case url != "":
				key = consts.QChanURL
				val = url
			case name != "":
				key = consts.QChanName
				val = name
			case id != 0:
				key = consts.QChanID
				val = strconv.Itoa(id)
			default:
				return fmt.Errorf("please enter either a URL or name")
			}

			if err := verifyChanRowUpdateValid(col, newVal); err != nil {
				return err
			}
			if err := cs.UpdateChannelRow(key, val, col, newVal); err != nil {
				return err
			}
			logging.S(0, "Updated channel column %q to value %q", col, newVal)
			return nil
		},
	}

	// Primary channel elements
	updateRowCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	updateRowCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")
	updateRowCmd.Flags().IntVarP(&id, "id", "i", 0, "Channel ID in the DB")

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
		return fmt.Errorf("cannot set a custom value for internal DB elements")
	}
	return nil
}

// verifyChannelOps verifies that the user inputted filters are valid
func verifyChannelOps(ops []string) ([]models.DLFilters, error) {

	var filters = make([]models.DLFilters, 0, len(ops))
	for _, op := range ops {
		split := strings.Split(op, ":")
		if len(split) < 3 {
			return nil, fmt.Errorf("please enter filters in the format 'field:filter_type:value' (e.g. 'title:omit:frogs' ignores videos with frogs in the metatitle)")
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
				return nil, fmt.Errorf("please enter a filter type of either 'contains' or 'omit'")
			}
		case 2:
			switch split[1] {
			case "contains", "omit":
				filters = append(filters, models.DLFilters{
					Field: split[0],
					Type:  split[1],
				})
			default:
				return nil, fmt.Errorf("please enter a filter type of either 'contains' or 'omit'")
			}
		default:
			return nil, fmt.Errorf("invalid filter. Valid examples: 'title:contains:frogs','date:omit' (contains only metatitles with frogs, and omits downloads including a date metafield)")

		}
	}
	return filters, nil
}
