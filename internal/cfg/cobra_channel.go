package cfg

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
)

// InitChannelCmds is the entrypoint for initializing channel commands
func initChannelCmds(s interfaces.Store) *cobra.Command {
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
	channelCmd.AddCommand(crawlChannelCmd(cs, s))
	channelCmd.AddCommand(deleteChannelCmd(cs))
	channelCmd.AddCommand(listChannelCmd(cs))
	channelCmd.AddCommand(updateChannelRow(cs))

	return channelCmd
}

// addChannelCmd adds a new channel into the database
func addChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
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
	addCmd.Flags().StringVarP(&jDir, keys.JSONDir, "j", "", "Output directory (videos will be saved here)")

	// Channel settings
	addCmd.Flags().IntVar(&crawlFreq, keys.CrawlFreq, 30, "How often to check for new videos (in minutes)")
	addCmd.Flags().IntVarP(&concurrency, keys.Concurrency, "l", 3, "Output directory (videos will be saved here)")
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

// deleteChannelCmd deletes a channel from the database
func deleteChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
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
			logging.S(0, "Successfully deleted channel with key '%s' and value '%s'", key, val)
			return nil
		},
	}

	// Primary channel elements
	delCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")
	delCmd.Flags().StringVarP(&name, "name", "n", "", "Channel name")

	return delCmd
}

// listChannelCmd returns a list of channels in the database
func listChannelCmd(cs interfaces.ChannelStore) *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all channels",
		Long:  "Lists all channels currently saved in the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			channels, err, hasRows := cs.ListChannels()
			if !hasRows {
				logging.I("No entries in the database")
				return nil
			}
			if err != nil {
				return err
			}

			for _, c := range channels {
				fmt.Printf("\nChannel ID: %d\n\nName: %s\nURL: %s\nCrawl Frequency: %d minutes\nFilters: %v\n",
					c.ID, c.Name, c.URL, c.Settings.CrawlFreq, c.Settings.Filters)

				// Display Metarr operations if they exist
				if len(c.MetarrArgs.MetaOps) > 0 {
					fmt.Printf("Metarr Operations:\n")
					for _, op := range c.MetarrArgs.MetaOps {
						fmt.Printf("  - %s\n", op)
					}
				}
				if c.MetarrArgs.FileDatePfx != "" {
					fmt.Printf("Filename Date Format: %s\n", c.MetarrArgs.FileDatePfx)
				}
				if c.MetarrArgs.RenameStyle != "" {
					fmt.Printf("Rename Style: %s\n", c.MetarrArgs.RenameStyle)
				}
				if c.MetarrArgs.FilenameReplaceSfx != "" {
					fmt.Printf("Filename Suffix Replace: %s\n", c.MetarrArgs.FilenameReplaceSfx)
				}
			}
			return nil
		},
	}

	return listCmd
}

// crawlChannelCmd initiates a crawl of a given channel
func crawlChannelCmd(cs interfaces.ChannelStore, s interfaces.Store) *cobra.Command {
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

// updateChannelRow provides a command allowing the alteration of a channel row
func updateChannelRow(cs interfaces.ChannelStore) *cobra.Command {
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
			logging.S(0, "Updated channel column '%s' to value '%s'", col, newVal)
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
