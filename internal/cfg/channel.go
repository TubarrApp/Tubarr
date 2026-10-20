package cfg

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"tubarr/internal/app"
	"tubarr/internal/auth"
	"tubarr/internal/blocking"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/state"

	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
	"github.com/spf13/cobra"
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
	channelCmd.AddCommand(manualDownloadVideoURLs(ctx, cs, s))
	channelCmd.AddCommand(runMetarrCmd(ctx, cs, s))
	channelCmd.AddCommand(listAvailableCmd(cs, s))
	channelCmd.AddCommand(downloadAvailableCmd(ctx, cs, s))
	channelCmd.AddCommand(dismissAvailableCmd(cs, s))
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
				return fmt.Errorf(`must enter --%s and --%s, or --%s as JSON ('{"channel_url":"...","username":"...","password":"...","login_url":"..."}')`,
					keys.AuthUsername, keys.AuthURL, keys.AuthDetails)
			}

			c, key, val, err := lookupChannel(cs, channelID, channelName)
			if err != nil {
				return err
			}

			// Parse and set authentication details.
			cURLs := c.GetURLs()
			authDetails, err := auth.ParseAuthDetails(username, password, loginURL, authDetails, cURLs, false)
			if err != nil {
				return err
			}

			if len(authDetails) > 0 {
				if err := cs.AddAuth(c.ID, authDetails); err != nil {
					return err
				}
			}

			// Success.
			logger.Pl.S("Channel with %s %q set authentication details", key, val)
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
		// Channel identifiers.
		channelID   int
		channelName string

		urls        []string
		deleteFiles bool
	)

	deleteURLsCmd := &cobra.Command{
		Use:   "delete-videos",
		Short: "Remove videos from a channel's database list.",
		Long:  fmt.Sprintf("Removes videos from a channel's database list, so later crawls treat them as new. Their video and metadata files are kept unless --%s is given.", keys.DeleteFiles),
		RunE: func(_ *cobra.Command, _ []string) error {
			if len(urls) == 0 {
				return errors.New("must enter video URLs to delete")
			}
			chanID, _, _, err := lookupChannelID(cs, channelID, channelName)
			if err != nil {
				return err
			}

			// Delete videos based on URL list.
			return cs.DeleteVideosByURLs(chanID, urls, deleteFiles)
		},
	}

	cmd.SetPrimaryChannelFlags(deleteURLsCmd, &channelName, nil, &channelID)
	deleteURLsCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Video URLs to remove from the database.")
	deleteURLsCmd.Flags().BoolVar(&deleteFiles, keys.DeleteFiles, false, "Also delete the videos' video and metadata files from disk.")

	return deleteURLsCmd
}

// manualDownloadVideoURLs downloads a list of URLs inputted by the user.
func manualDownloadVideoURLs(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		// Channel identifiers.
		channelID   int
		channelName string

		// URLs and files.
		cFile           string
		urls            []string
		channelURL      string
		sendToAvailable bool
	)

	manualURLCmd := &cobra.Command{
		Use:   "download-video-urls",
		Short: "Download inputted URLs (plaintext or file).",
		Long: fmt.Sprintf("If using a file, the file should contain one URL per line. Use --%s to download through one of the channel's URLs "+
			"(e.g. for its authentication), else they go to the channel's manual downloads.", keys.ChannelURL),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Valid items set?
			if cFile == "" && len(urls) == 0 {
				return errors.New("must enter URLs into the source file, or set at least one URL directly")
			}

			var urlLines []string
			// Check URL file if existent.
			if cFile != "" {
				_, cFileInfo, err := sharedvalidation.ValidateFile(cFile, false, sharedtemplates.NoTemplateTags)
				if err != nil {
					return fmt.Errorf("file entered (%q) is not valid: %w", cFile, err)
				}
				if cFileInfo.Size() == 0 {
					return fmt.Errorf("url file %q is blank", cFile)
				}
				if urlLines, err = file.ReadFileLines(cFile); err != nil {
					return err
				}
			}

			// Combine with video URLs if any.
			videoURLs := append(urls, urlLines...)

			c, _, _, err := lookupChannel(cs, channelID, channelName)
			if err != nil {
				return err
			}

			// Resolve the channel URL the videos go through, if given.
			var channelURLID int64
			if channelURL != "" {
				var valid []string
				for _, cu := range c.URLModels {
					if cu.IsManual {
						continue
					}
					if sharedparsing.NormalizeChannelURL(cu.URL) == sharedparsing.NormalizeChannelURL(channelURL) {
						channelURLID = cu.ID
						break
					}
					valid = append(valid, cu.URL)
				}
				if channelURLID == 0 {
					return fmt.Errorf("--%s %q isn't one of channel %q's URLs: %v", keys.ChannelURL, channelURL, c.Name, valid)
				}
			}
			videos := make([]models.ManualVideo, 0, len(videoURLs))
			for _, u := range videoURLs {
				videos = append(videos, models.ManualVideo{URL: u, ChannelURLID: channelURLID})
			}

			// Add to the Available list instead of downloading.
			if sendToAvailable {
				added, existing, err := app.AddManualAvailable(s, c, videos)
				if err != nil {
					return err
				}
				logger.Pl.S("Added %d video(s) to channel %q's Available list", len(added), c.Name)
				if len(existing) > 0 {
					logger.Pl.W("Skipped %d video(s) already in channel %q:\n%s", len(existing), c.Name, strings.Join(existing, "\n"))
				}
				return nil
			}

			// Lock crawl state.
			crawlCtx, unlock, ok := state.TryLockCrawl(ctx, c.ID)
			if !ok {
				logger.Pl.I("Crawl job is active for channel %q, please try again when it's complete.", c.Name)
				return nil
			}
			defer unlock()

			// Download URLs.
			if err := app.ManualDownloadToChannel(crawlCtx, s, cs, c, videos); err != nil {
				return err
			}

			// Success.
			return nil
		},
	}

	cmd.SetPrimaryChannelFlags(manualURLCmd, &channelName, nil, &channelID)
	manualURLCmd.Flags().StringVarP(&cFile, keys.URLFile, "f", "", "Enter a file containing one URL per line to download them to your given channel")
	manualURLCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Enter a list of URLs to download for a given channel")
	manualURLCmd.Flags().StringVar(&channelURL, keys.ChannelURL, "", "One of the channel's URLs to download the videos through, e.g. for its authentication")
	manualURLCmd.Flags().BoolVar(&sendToAvailable, keys.SendToAvailable, false, "Add the videos to the channel's Available list instead of downloading them")

	return manualURLCmd
}

// runMetarrCmd runs Metarr on downloaded videos which Metarr has not yet processed.
func runMetarrCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		channelID   int
		channelName string
		urls        []string
	)

	runCmd := &cobra.Command{
		Use:          "run-metarr",
		Short:        "Run Metarr on downloaded videos it failed on or skipped.",
		Long:         "Runs Metarr on all pending/failed videos in the channel, or only those given with --urls. The original downloaded files must still exist.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, _, _, err := lookupChannel(cs, channelID, channelName)
			if err != nil {
				return err
			}

			// Resolve URLs to video IDs.
			var ids []int64
			if len(urls) > 0 {
				videos, err := s.VideoStore().GetVideosAwaitingMetarr(c.ID, false)
				if err != nil {
					return err
				}
				for _, u := range urls {
					idx := slices.IndexFunc(videos, func(v *models.Video) bool { return v.URL == u })
					if idx == -1 {
						logger.Pl.W("Video %q is not awaiting Metarr in channel %q, skipping", u, c.Name)
						continue
					}
					ids = append(ids, videos[idx].ID)
				}
				if len(ids) == 0 {
					return nil
				}
			}

			file.UpdateFromConfigFile(cs, c)
			return app.RunMetarr(ctx, s, c, ids, false)
		},
	}

	cmd.SetPrimaryChannelFlags(runCmd, &channelName, nil, &channelID)
	runCmd.Flags().StringSliceVar(&urls, keys.URLs, nil, "Only run Metarr on these video URLs (default: all pending/failed videos in the channel)")

	return runCmd
}

// listAvailableCmd lists videos waiting in Available for the user to download or dismiss.
func listAvailableCmd(cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		channelID   int
		channelName string
	)

	listCmd := &cobra.Command{
		Use:   "list-available",
		Short: "List videos waiting in Available.",
		Long:  "Lists videos waiting to be downloaded or dismissed, for the given channel or all channels.",
		RunE: func(_ *cobra.Command, _ []string) error {
			var chanID int64
			if channelID != 0 || channelName != "" {
				var err error
				if chanID, _, _, err = lookupChannelID(cs, channelID, channelName); err != nil {
					return err
				}
			}

			videos, err := s.VideoStore().GetAvailableVideos(app.AvailableStatuses...)
			if err != nil {
				return err
			}
			listed := 0
			for _, v := range videos {
				if chanID != 0 && v.ChannelID != chanID {
					continue
				}
				listed++
				printAvailableVideo(fmt.Sprintf("[%d]", v.ID), v, chanID == 0)
			}
			if listed == 0 {
				logger.Pl.I("No videos waiting in Available")
			}
			return nil
		},
	}

	cmd.SetPrimaryChannelFlags(listCmd, &channelName, nil, &channelID)
	return listCmd
}

// downloadAvailableCmd downloads a channel's videos waiting in Available.
func downloadAvailableCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		channelID   int
		channelName string
		urls        []string
		ids         []int64
		all         bool
	)

	downloadCmd := &cobra.Command{
		Use:   "download-available",
		Short: "Download videos waiting in Available.",
		Long: fmt.Sprintf("Downloads the channel's videos waiting in Available given with --%s, --%s or --%s, or asks which to download. Download As overrides set in the web UI are kept.",
			keys.AllVideos, keys.VideoIDs, keys.URLs),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, videos, err := getAvailableVideos(cs, s, channelID, channelName)
			if err != nil {
				return err
			}
			if c.LoadErr != nil {
				return c.LoadErr
			}
			if len(videos) == 0 {
				logger.Pl.I("No videos waiting in Available for channel %q", c.Name)
				return nil
			}
			chosen, err := chooseAvailableVideos(c, videos, all, ids, urls, "Download")
			if err != nil {
				return err
			}
			if len(chosen) == 0 {
				return nil
			}

			claimed, err := s.VideoStore().ClaimVideos(c.ID, chosen, app.AvailableStatuses, consts.DLStatusQueued)
			if err != nil {
				return err
			}
			if len(claimed) == 0 {
				logger.Pl.I("Videos are no longer available for channel %q, they may already be downloading", c.Name)
				return nil
			}

			logger.Pl.I("Starting download of %d available video(s) for channel %q", len(claimed), c.Name)
			return app.DownloadAvailable(ctx, s, c, claimed, nil)
		},
	}

	cmd.SetPrimaryChannelFlags(downloadCmd, &channelName, nil, &channelID)
	setChooseAvailableFlags(downloadCmd, &urls, &ids, &all, "download")

	return downloadCmd
}

// dismissAvailableCmd removes videos from a channel's Available list without downloading them.
func dismissAvailableCmd(cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		channelID   int
		channelName string
		urls        []string
		ids         []int64
		all         bool
	)

	dismissCmd := &cobra.Command{
		Use:   "dismiss-available",
		Short: "Dismiss videos waiting in Available.",
		Long: fmt.Sprintf("Removes the channel's videos given with --%s, --%s or --%s from Available without downloading them, or asks which to dismiss. Later crawls skip them.",
			keys.AllVideos, keys.VideoIDs, keys.URLs),
		RunE: func(_ *cobra.Command, _ []string) error {
			c, videos, err := getAvailableVideos(cs, s, channelID, channelName)
			if err != nil {
				return err
			}
			if len(videos) == 0 {
				logger.Pl.I("No videos waiting in Available for channel %q", c.Name)
				return nil
			}
			chosen, err := chooseAvailableVideos(c, videos, all, ids, urls, "Dismiss")
			if err != nil {
				return err
			}
			if len(chosen) == 0 {
				return nil
			}

			claimed, err := s.VideoStore().ClaimVideos(c.ID, chosen, app.AvailableStatuses, consts.DLStatusIgnored)
			if err != nil {
				return err
			}
			app.RemoveDismissedFromNew(cs, c, claimed)
			logger.Pl.S("Dismissed %d video(s) from Available for channel %q", len(claimed), c.Name)
			return nil
		},
	}

	cmd.SetPrimaryChannelFlags(dismissCmd, &channelName, nil, &channelID)
	setChooseAvailableFlags(dismissCmd, &urls, &ids, &all, "dismiss")

	return dismissCmd
}

// setChooseAvailableFlags sets the flags choosing which Available videos to act on.
func setChooseAvailableFlags(c *cobra.Command, urls *[]string, ids *[]int64, all *bool, verb string) {
	c.Flags().StringSliceVar(urls, keys.URLs, nil, fmt.Sprintf("Video URLs to %s", verb))
	c.Flags().Int64SliceVar(ids, keys.VideoIDs, nil, fmt.Sprintf("Video IDs to %s, as shown by 'list-available'", verb))
	c.Flags().BoolVar(all, keys.AllVideos, false, fmt.Sprintf("%s all of the channel's videos in Available", verb))
}

// getAvailableVideos returns the channel and its videos waiting in Available.
func getAvailableVideos(cs contracts.ChannelStore, s contracts.Store, channelID int, channelName string) (*models.Channel, []*models.AvailableVideo, error) {
	c, _, _, err := lookupChannel(cs, channelID, channelName)
	if err != nil {
		return nil, nil, err
	}

	all, err := s.VideoStore().GetAvailableVideos(app.AvailableStatuses...)
	if err != nil {
		return nil, nil, err
	}
	videos := slices.DeleteFunc(all, func(v *models.AvailableVideo) bool { return v.ChannelID != c.ID })
	return c, videos, nil
}

// chooseAvailableVideos returns the IDs of the videos chosen with all, ids or urls, or else picked by the user in the terminal.
func chooseAvailableVideos(c *models.Channel, videos []*models.AvailableVideo, all bool, ids []int64, urls []string, verb string) ([]int64, error) {
	if !all && len(ids) == 0 && len(urls) == 0 {
		return pickAvailableVideos(videos, verb)
	}

	var chosen []int64
	for _, v := range videos {
		if all || slices.Contains(ids, v.ID) || slices.Contains(urls, v.URL) {
			chosen = append(chosen, v.ID)
		}
	}
	for _, id := range ids {
		if !slices.ContainsFunc(videos, func(v *models.AvailableVideo) bool { return v.ID == id }) {
			logger.Pl.W("Video ID %d is not in Available for channel %q, skipping", id, c.Name)
		}
	}
	for _, u := range urls {
		if !slices.ContainsFunc(videos, func(v *models.AvailableVideo) bool { return v.URL == u }) {
			logger.Pl.W("Video %q is not in Available for channel %q, skipping", u, c.Name)
		}
	}
	return chosen, nil
}

// pickAvailableVideos lists the videos numbered and asks the user which to choose, until they give a valid choice or none.
func pickAvailableVideos(videos []*models.AvailableVideo, verb string) ([]int64, error) {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return nil, fmt.Errorf("not running in a terminal, choose videos with --%s, --%s or --%s", keys.AllVideos, keys.VideoIDs, keys.URLs)
	}

	width := len(strconv.Itoa(len(videos)))
	for i, v := range videos {
		printAvailableVideo(fmt.Sprintf("%*d.", width, i+1), v, false)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s which? (e.g. 1,3,5-8 or 'all', blank to cancel): ", verb)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		picks, pickErr := parsePicks(strings.TrimSpace(line), len(videos))
		if pickErr != nil && err == nil {
			fmt.Printf("%v\n", pickErr)
			continue
		}
		chosen := make([]int64, 0, len(picks))
		for _, p := range picks {
			chosen = append(chosen, videos[p-1].ID)
		}
		return chosen, pickErr
	}
}

// parsePicks parses numbers and ranges like "1,3,5-8" (or "all") into distinct numbers from 1 to n.
func parsePicks(input string, n int) ([]int, error) {
	if strings.EqualFold(input, "all") {
		picks := make([]int, n)
		for i := range picks {
			picks[i] = i + 1
		}
		return picks, nil
	}

	var picks []int
	for _, field := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ' ' }) {
		lo, hi, isRange := strings.Cut(field, "-")
		from, err := strconv.Atoi(lo)
		if err != nil {
			return nil, fmt.Errorf("%q isn't a number or range", field)
		}
		to := from
		if isRange {
			if to, err = strconv.Atoi(hi); err != nil {
				return nil, fmt.Errorf("%q isn't a number or range", field)
			}
		}
		if from < 1 || to > n || from > to {
			return nil, fmt.Errorf("%q is outside 1-%d", field, n)
		}
		for p := from; p <= to; p++ {
			if !slices.Contains(picks, p) {
				picks = append(picks, p)
			}
		}
	}
	return picks, nil
}

// printAvailableVideo prints an Available video after label, naming its channel if withChannel.
func printAvailableVideo(label string, v *models.AvailableVideo, withChannel bool) {
	var details []string
	if withChannel {
		details = append(details, v.ChannelName)
	}
	if v.DownloadStatus.Status != consts.DLStatusAvailable {
		details = append(details, string(v.DownloadStatus.Status))
	}
	if !v.UploadDate.IsZero() {
		details = append(details, v.UploadDate.Format("2006-01-02"))
	}

	fmt.Printf("%s %s", label, v.Title)
	if len(details) > 0 {
		fmt.Printf(" (%s)", strings.Join(details, ", "))
	}
	indent := strings.Repeat(" ", len(label)+1)
	fmt.Printf("\n%s%s\n", indent, v.URL)
	if v.DownloadStatus.ErrorMsg != "" {
		fmt.Printf("%sReason: %s\n", indent, v.DownloadStatus.ErrorMsg)
	}
}

// deleteNotifyURLs deletes notification URLs from a channel.
func deleteNotifyURLs(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		channelID   int
		channelName string

		// Notification details.
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

			chanID, key, val, err := lookupChannelID(cs, channelID, channelName)
			if err != nil {
				return err
			}

			// Run command.
			if err := cs.DeleteNotifyURLs(chanID, notifyURLs, notifyNames); err != nil {
				return err
			}

			// Success.
			logger.Pl.S("Deleted %d notify URL(s) for channel with %s %q", len(notifyURLs)+len(notifyNames), key, val)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(deleteNotifyCmd, &channelName, nil, &channelID)
	deleteNotifyCmd.Flags().StringSliceVar(&notifyURLs, "notify-urls", nil, "Full notification URLs (e.g. 'http://YOUR_PLEX_SERVER_IP:32400/library/sections/LIBRARY_ID_NUMBER/refresh?X-Plex-Token=YOUR_PLEX_TOKEN_HERE')")
	deleteNotifyCmd.Flags().StringSliceVar(&notifyNames, "notify-names", nil, "Full notification names")

	return deleteNotifyCmd
}

// addNotifyURLs adds a notification URL (can use to send requests to update Plex libraries on new video addition).
func addNotifyURLs(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		channelID   int
		channelName string

		// Notification details.
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

			chanID, key, val, err := lookupChannelID(cs, channelID, channelName)
			if err != nil {
				return err
			}

			// Parse notification URLs.
			validPairs, err := parsing.ParseNotifications(notification)
			if err != nil {
				return err
			}

			if err := cs.AddNotifyURLs(chanID, validPairs); err != nil {
				return err
			}

			// Success.
			logger.Pl.S("Added %d notify URL(s) for channel with %s %q", len(validPairs), key, val)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(addNotifyCmd, &channelName, nil, &channelID)
	cmd.SetNotifyFlags(addNotifyCmd, &notification)

	return addNotifyCmd
}

// addVideoURLToIgnore adds a user inputted URL to ignore from crawls.
func addVideoURLToIgnore(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		id   int
		name string

		// Ignore details.
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

			chanID, key, val, err := lookupChannelID(cs, id, name)
			if err != nil {
				return err
			}

			// Add URLs to ignore.
			if err := cs.AddURLToIgnore(chanID, ignoreURL); err != nil {
				return err
			}

			// Success.
			logger.Pl.S("Ignoring URL %q for channel with %s %q", ignoreURL, key, val)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(ignoreURLCmd, &name, nil, nil)
	ignoreURLCmd.Flags().StringVarP(&ignoreURL, "ignore-video-url", "i", "", "Video URL to ignore")

	return ignoreURLCmd
}

// ignoreCrawl crawls the current state of the channel page and adds the URLs as though they are already grabbed.
func ignoreCrawl(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		// Channel identifiers.
		id   int
		name string
	)

	ignoreCrawlCmd := &cobra.Command{
		Use:          "ignore-crawl",
		Short:        "Crawl a channel for URLs to ignore.",
		Long:         "Crawls the current state of a channel page and adds all video URLs to ignore.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, key, val, err := lookupChannel(cs, id, name)
			if err != nil {
				return err
			}

			// Log retrieved URLs.
			cURLs := c.GetURLs()
			logger.Pl.D(3, "Retrieved channel %q with URLs: %v", c.Name, cURLs)

			// Run ignore crawl.
			if crawlCtx, unlock, ok := state.TryLockCrawl(ctx, c.ID); ok {
				defer unlock()

				// Clean expired blocks first.
				if err := blocking.CleanExpiredBlocks(cs.GetDB()); err != nil {
					logger.Pl.W("Failed to clean expired blocks: %v", err)
				}

				if err := app.CrawlChannelIgnore(crawlCtx, s, c); err != nil {
					return err
				}
			} else {
				logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
			}

			// Success.
			logger.Pl.S("Completed ignore crawl for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(ignoreCrawlCmd, &name, nil, nil)

	return ignoreCrawlCmd
}

// pauseChannelCmd pauses a channel from downloads in upcoming crawls.
func pauseChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		id   int
		name string
	)

	pauseCmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause a channel.",
		Long:  "Paused channels won't download new videos when the main program runs a crawl.",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, key, val, err := lookupChannel(cs, id, name)
			if err != nil {
				return err
			}

			// Send update.
			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.Settings) error {
				s.Paused = true
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to pause channel: %w", err)
			}

			// Success.
			logger.Pl.S("Paused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(pauseCmd, &name, nil, &id)

	return pauseCmd
}

// unpauseChannelCmd unpauses a channel to allow for downloads in upcoming crawls.
func unpauseChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		id   int
		name string
	)

	unPauseCmd := &cobra.Command{
		Use:   "unpause",
		Short: "Unpause a channel.",
		Long:  "Unpauses a channel, allowing it to download new videos when the main program runs a crawl.",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, key, val, err := lookupChannel(cs, id, name)
			if err != nil {
				return err
			}

			// Send update.
			_, err = cs.UpdateChannelSettingsJSON(key, val, func(s *models.Settings) error {
				s.Paused = false
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to unpause channel: %w", err)
			}

			// Success.
			logger.Pl.S("Unpaused channel %q", c.Name)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(unPauseCmd, &name, nil, &id)

	return unPauseCmd
}

// Flags for unblocking domains.
const (
	unblockDomainFlag  = "domain"
	unblockContextFlag = "context"
	unblockAllFlag     = "all"
)

// unblockChannelCmd unblocks domains which were locked (usually due to bot activity detection).
func unblockChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		domain     string
		contextArg string
		all        bool
	)

	unblockCmd := &cobra.Command{
		Use:   "unblock",
		Short: "Unblock a domain globally.",
		Long:  "Unblocks a domain globally (usually blocked due to sites detecting Tubarr as a bot), allowing channels to access it again.",
		Example: fmt.Sprintf(`  # Unblock a domain for all authentication contexts
  tubarr channel unblock --%[1]s "youtube.com"

  # Unblock a domain for a specific authentication context
  tubarr channel unblock --%[1]s "youtube.com" --%[2]s "auth"

  # Unblock all domains
  tubarr channel unblock --%[3]s`, unblockDomainFlag, unblockContextFlag, unblockAllFlag),
		RunE: func(_ *cobra.Command, _ []string) error {
			db := cs.GetDB()

			if all {
				// Unblock all domains for all contexts.
				allBlocked := blocking.GetAllBlockedDomains()
				if len(allBlocked) == 0 {
					logger.Pl.I("No blocked domains found")
					return nil
				}

				for d := range allBlocked {
					if err := blocking.UnblockDomain(db, d, ""); err != nil {
						return fmt.Errorf("failed to unblock domain %q: %w", d, err)
					}
				}
				logger.Pl.S("Unblocked all domains")
				return nil
			}

			if domain == "" {
				return fmt.Errorf("either --%s or --%s must be specified", unblockDomainFlag, unblockAllFlag)
			}

			blockCtx, err := blocking.ParseBlockContext(contextArg)
			if err != nil {
				return err
			}

			// Unblock the domain.
			if err := blocking.UnblockDomain(db, domain, blockCtx); err != nil {
				return fmt.Errorf("failed to unblock domain: %w", err)
			}

			return nil
		},
	}

	// Flags.
	unblockCmd.Flags().StringVar(&domain, unblockDomainFlag, "", "Domain to unblock (e.g. youtube.com)")
	unblockCmd.Flags().StringVar(&contextArg, unblockContextFlag, "", "Authentication context to unblock (unauth/cookie/auth). If not specified, unblocks all contexts for the domain.")
	unblockCmd.Flags().BoolVar(&all, unblockAllFlag, false, "Unblock all domains")

	return unblockCmd
}

// addChannelCmd adds a new channel into the database.
func addChannelCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		input       models.ChannelInput
		flags       models.ChannelFlagValues
		addFromFile string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a channel.",
		Long:  "Add channel adds a new channel to the database.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			// A channel config file is loaded now, and saved to be reloaded on each run.
			if path := cmp.Or(flags.ChannelConfigFile, addFromFile); path != "" {
				var err error
				if input, err = parsing.LoadChannelInputFile(path); err != nil {
					return err
				}
			}
			mergeFlagsIntoInput(cmd, &flags, &input)
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := app.AddChannelFromInput(cs, input)
			if err != nil {
				return err
			}
			if input.IgnoreRun != nil && *input.IgnoreRun {
				runIgnoreCrawl(ctx, s, c)
			}
			logger.Pl.S("Completed addition of channel %q to Tubarr", c.Name)
			return nil
		},
	}

	cmd.SetPrimaryChannelFlags(addCmd, &flags.Name, &flags.URLs, nil)

	cmd.SetFileDirFlags(addCmd, &flags.ChannelConfigFile, &flags.JSONDir, &flags.VideoDir)

	setChannelSettingsFlags(addCmd, &flags)

	cmd.SetAuthFlags(addCmd, &flags.Username, &flags.Password, &flags.LoginURL, &flags.AuthDetails)

	cmd.SetNotifyFlags(addCmd, &flags.Notification)

	addCmd.Flags().BoolVar(&flags.IgnoreRun, keys.IgnoreRun, false, "Run an 'ignore crawl' first so only new videos are downloaded (rather than the entire channel backlog)")
	addCmd.Flags().StringVar(&addFromFile, "add-channel-from-file", "", "Add a channel using a prewritten file (.json, .toml, .yml/.yaml).\nFile contents example:\n\nchannel-name: 'Cool Channel'\nchannel-urls:\n  - 'https://www.coolchannel.com/'\n")

	return addCmd
}

// addBatchChannelsCmd adds multiple channels from config files in a directory.
func addBatchChannelsCmd(ctx context.Context, cs contracts.ChannelStore, s contracts.Store) *cobra.Command {
	var (
		configDirectory string
		flags           models.ChannelFlagValues
	)

	batchCmd := &cobra.Command{
		Use:   "add-batch",
		Short: "Add multiple channels from a directory.",
		Long:  "Add multiple channels by reading all Viper-compatible config files (.yaml, .yml, .toml, .json, etc.) from a directory.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configDirectory == "" {
				return errors.New("must specify a directory path containing config files")
			}

			batchConfigFiles, err := file.ScanDirectoryForConfigFiles(configDirectory)
			if err != nil {
				return fmt.Errorf("failed to scan directory: %w", err)
			}

			if len(batchConfigFiles) == 0 {
				logger.Pl.I("No config files found in directory %q", configDirectory)
				return nil
			}

			logger.Pl.I("Found %d config file(s) in directory %q", len(batchConfigFiles), configDirectory)

			type failure struct {
				file string
				err  error
			}
			var (
				successes []string
				failures  []failure
			)

			// Process each config file, as 'channel add --add-channel-from-file' does: flags given override each file.
			for _, batchConfigFile := range batchConfigFiles {
				logger.Pl.I("Processing config file: %s", batchConfigFile)

				input, err := parsing.LoadChannelInputFile(batchConfigFile)
				if err != nil {
					failures = append(failures, failure{batchConfigFile, err})
					continue
				}
				mergeFlagsIntoInput(cmd, &flags, &input)

				c, err := app.AddChannelFromInput(cs, input)
				if err != nil {
					failures = append(failures, failure{batchConfigFile, err})
					continue
				}
				if input.IgnoreRun != nil && *input.IgnoreRun {
					runIgnoreCrawl(ctx, s, c)
				}

				successes = append(successes, batchConfigFile)
				logger.Pl.S("Successfully added channel %q from config file: %s", c.Name, batchConfigFile)
			}

			fmt.Println()
			logger.Pl.P("====== Batch Add Summary ======\n")
			if len(successes) > 0 {
				logger.Pl.S("Successfully added %d channel(s)", len(successes))
			}
			if len(failures) > 0 {
				logger.Pl.E("Failed to add %d channel(s):", len(failures))
				for _, f := range failures {
					logger.Pl.E("  - %s: %v", f.file, f.err)
				}
			}

			return nil
		},
	}

	batchCmd.Flags().StringVar(&configDirectory, "add-from-directory", "", "Directory containing channel config files (.json, .toml, .yaml/.yml)")

	cmd.SetPrimaryChannelFlags(batchCmd, &flags.Name, &flags.URLs, nil)
	cmd.SetFileDirFlags(batchCmd, &flags.ChannelConfigFile, &flags.JSONDir, &flags.VideoDir)
	setChannelSettingsFlags(batchCmd, &flags)
	cmd.SetAuthFlags(batchCmd, &flags.Username, &flags.Password, &flags.LoginURL, &flags.AuthDetails)
	cmd.SetNotifyFlags(batchCmd, &flags.Notification)

	batchCmd.Flags().BoolVar(&flags.IgnoreRun, keys.IgnoreRun, false, "Run an 'ignore crawl' for each channel so only new videos are downloaded")

	return batchCmd
}

// deleteChannelCmd deletes a channel from the database.
func deleteChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		id   int
		name string

		// URLs.
		urls []string
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete channels.",
		Long:  "Delete a channel by ID or name.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair.
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Delete channel.
			if err := cs.DeleteChannel(key, val); err != nil {
				return err
			}

			// Success.
			logger.Pl.S("Successfully deleted channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(delCmd, &name, &urls, &id)

	return delCmd
}

// listAllChannel returns details about a single channel in the database.
func listChannelCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		channelID int
		name      string
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List a channel's details.",
		Long:  "Lists details of a channel in the database.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Get and check key/val pair.
			key, val, err := getChanKeyVal(channelID, name)
			if err != nil {
				return err
			}

			// As saved, so each URL shows only its own settings.
			c, hasRows, err := cs.GetChannelModel(key, val, false)
			if !hasRows {
				logger.Pl.I("Entry for channel with %s %q does not exist in the database", key, val)
				return nil
			}
			if err != nil {
				return err
			}

			// Display settings and return.
			cs.DisplaySettings(c)
			return nil
		},
	}
	// Primary channel elements.
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

			// Fetch channels from database.
			chans, hasRows, err := cs.GetAllChannels(false)
			if !hasRows {
				logger.Pl.I("No entries in the database")
				return nil
			}
			if err != nil {
				return err
			}

			// Display settings and return.
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
		// Channel identifiers.
		id   int
		name string
	)

	crawlCmd := &cobra.Command{
		Use:          "crawl",
		Short:        "Crawl a channel for new URLs.",
		Long:         "Initiate a crawl for new URLs of a channel that have not yet been downloaded.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, key, val, err := lookupChannel(cs, id, name)
			if err != nil {
				return err
			}

			// Crawl channel.
			if crawlCtx, unlock, ok := state.TryLockCrawl(ctx, c.ID); ok {
				defer unlock()

				// Clean expired blocks first.
				if err := blocking.CleanExpiredBlocks(cs.GetDB()); err != nil {
					logger.Pl.W("Failed to clean expired blocks: %v", err)
				}
				if performedCrawls, err := app.CrawlChannel(crawlCtx, s, c); err != nil {
					return err
				} else if performedCrawls {
					logger.Pl.S("Completed crawl for channel with %s %q", key, val)
				}
			} else {
				logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
			}

			// Success.
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(crawlCmd, &name, nil, &id)
	return crawlCmd
}

// updateChannelSettingsCmd updates channel settings.
func updateChannelSettingsCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		flags      models.ChannelFlagValues
		id         int
		newName    string
		deleteAuth bool
	)

	updateSettingsCmd := &cobra.Command{
		Use:   "update-channel",
		Short: "Update channel settings.",
		Long:  "Update channel settings with various parameters, both for Tubarr itself and for external software like Metarr.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			// Load in file if specified.
			v := viper.New()
			if flags.ChannelConfigFile != "" {
				if err := file.LoadConfigFile(v, flags.ChannelConfigFile); err != nil {
					return err
				}
			}

			return resetCobraFlagsAndLoadViper(cmd, v)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, key, val, err := lookupChannel(cs, id, flags.Name)
			if err != nil {
				return err
			}

			// Load the channel's saved config file, unless one was given (PreRunE loaded that).
			if flags.ChannelConfigFile == "" && c.ChannelConfigFile != "" {
				v := viper.New()
				if err := file.LoadConfigFile(v, c.ChannelConfigFile); err != nil {
					return err
				}
				if err := resetCobraFlagsAndLoadViper(cmd, v); err != nil {
					return err
				}
			}

			// Change name.
			if newName != "" && newName != c.Name {
				if err := cs.UpdateChannelValue(key, val, consts.QChanName, newName); err != nil {
					return err
				}
			}

			// Check if the user got auth details from flags.
			gotAuthDetails := cmd.Flags().Changed(keys.AuthUsername) ||
				cmd.Flags().Changed(keys.AuthPassword) ||
				cmd.Flags().Changed(keys.AuthURL) ||
				cmd.Flags().Changed(keys.AuthDetails)

			// Parse and set authentication details if set by user, clear all if flag is set.
			if gotAuthDetails || deleteAuth {
				cURLs := c.GetURLs()
				authDetails, err := auth.ParseAuthDetails(flags.Username, flags.Password, flags.LoginURL, flags.AuthDetails, cURLs, deleteAuth)
				if err != nil {
					return err
				}

				if len(authDetails) > 0 {
					if err := cs.AddAuth(c.ID, authDetails); err != nil {
						return err
					}
				}
			}

			// Apply the settings given, saving those that change.
			var input models.ChannelInput
			mergeFlagsIntoInput(cmd, &flags, &input)
			s, m := *c.ChanSettings, *c.ChanMetarrArgs
			if err := parsing.ApplyChannelSettingsInput(&s, &input.SettingsInput); err != nil {
				return err
			}
			if err := parsing.ApplyMetarrArgsInput(&m, &input.MetarrArgsInput); err != nil {
				return err
			}
			if !reflect.DeepEqual(s, *c.ChanSettings) {
				if _, err := cs.UpdateChannelSettingsJSON(key, val, func(saved *models.Settings) error {
					*saved = s
					return nil
				}); err != nil {
					return err
				}
			}
			if !reflect.DeepEqual(m, *c.ChanMetarrArgs) {
				if _, err := cs.UpdateChannelMetarrArgsJSON(key, val, func(saved *models.MetarrArgs) error {
					*saved = m
					return nil
				}); err != nil {
					return err
				}
			}

			// Success.
			logger.Pl.S("Completed update for channel with %s %q", key, val)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(updateSettingsCmd, &flags.Name, &flags.URLs, &id)

	// Files/dirs.
	cmd.SetFileDirFlags(updateSettingsCmd, &flags.ChannelConfigFile, &flags.JSONDir, &flags.VideoDir)

	// Settings.
	setChannelSettingsFlags(updateSettingsCmd, &flags)

	// Auth.
	cmd.SetAuthFlags(updateSettingsCmd, &flags.Username, &flags.Password,
		&flags.LoginURL, &flags.AuthDetails)

	updateSettingsCmd.Flags().StringVar(&newName, "change-name-to", "", "Change the channel name to this value")
	updateSettingsCmd.Flags().BoolVar(&deleteAuth, "delete-auth", false, "Clear all authentication details for this channel and its URLs")

	return updateSettingsCmd
}

// updateChannelValue provides a command allowing the alteration of a channel row.
func updateChannelValue(cs contracts.ChannelStore) *cobra.Command {
	var (
		// Channel identifiers.
		id   int
		name string

		// Update details.
		col    string
		newVal string
	)

	updateRowCmd := &cobra.Command{
		Use:   "update-value",
		Short: "Update a channel column value.",
		Long:  "Enter a column to update and a value to update that column to.",
		RunE: func(_ *cobra.Command, _ []string) error {

			// Check key/val pair.
			key, val, err := getChanKeyVal(id, name)
			if err != nil {
				return err
			}

			// Check validity.
			if err := verifyChanRowUpdateValid(col, newVal); err != nil {
				return err
			}

			// Update value in database.
			if err := cs.UpdateChannelValue(key, val, col, newVal); err != nil {
				return err
			}

			// Success
			logger.Pl.S("Updated channel column: %q → %q", col, newVal)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(updateRowCmd, &name, nil, &id)

	ColNameMessage := fmt.Sprintf("The name of the column in the table. Valid options: %v", slices.Sorted(maps.Keys(consts.ChannelTextColumns)))

	updateRowCmd.Flags().StringVar(&col, "column-name", "", ColNameMessage)
	updateRowCmd.Flags().StringVar(&newVal, "set-value", "", "The value to set in the column (e.g. My Channel)")
	return updateRowCmd
}

// ******************************** Private ********************************

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

// runIgnoreCrawl runs an ignore crawl of a newly added channel, so only videos uploaded from now on download.
func runIgnoreCrawl(ctx context.Context, s contracts.Store, c *models.Channel) {
	crawlCtx, unlock, ok := state.TryLockCrawl(ctx, c.ID)
	if !ok {
		logger.Pl.I("Crawl for channel %q is already active, skipping...", c.Name)
		return
	}
	defer unlock()
	logger.Pl.I("Running an 'ignore crawl' for channel %q...", c.Name)
	if err := app.CrawlChannelIgnore(crawlCtx, s, c); err != nil {
		logger.Pl.E("Failed to complete ignore crawl run for %q: %v", c.Name, err)
	}
}

// lookupChannel returns the channel given by id or name, its URLs' unset settings filled in from the channel's as jobs
// use them, with the key and value it was found by.
func lookupChannel(cs contracts.ChannelStore, id int, name string) (c *models.Channel, key, val string, err error) {
	if key, val, err = getChanKeyVal(id, name); err != nil {
		return nil, "", "", err
	}
	var found bool
	if c, found, err = cs.GetChannelModel(key, val, true); err != nil {
		return nil, "", "", err
	}
	if !found {
		return nil, "", "", fmt.Errorf("no channel in database with %s %q", key, val)
	}
	return c, key, val, nil
}

// lookupChannelID returns the ID of the channel given by id or name, with the key and value it was found by.
func lookupChannelID(cs contracts.ChannelStore, id int, name string) (chanID int64, key, val string, err error) {
	if key, val, err = getChanKeyVal(id, name); err != nil {
		return 0, "", "", err
	}
	if chanID, err = cs.GetChannelID(key, val); err != nil {
		return 0, "", "", err
	}
	if chanID == 0 {
		return 0, "", "", fmt.Errorf("no channel in database with %s %q", key, val)
	}
	return chanID, key, val, nil
}

// verifyChanRowUpdateValid verifies that your update operation is valid.
func verifyChanRowUpdateValid(col, val string) error {
	if _, ok := consts.ChannelTextColumns[col]; !ok {
		return fmt.Errorf("column %q can't be set directly (valid options: %v), use 'update-channel' for channel settings", col, slices.Sorted(maps.Keys(consts.ChannelTextColumns)))
	}
	if col == consts.QChanName && val == "" {
		return fmt.Errorf("cannot set %s blank, please use the 'delete' function if you want to remove this channel entirely", col)
	}
	return nil
}
