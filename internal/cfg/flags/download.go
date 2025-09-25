package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// SetDownloadFlags sets flags related to download tasks.
func SetDownloadFlags(cmd *cobra.Command, retries *int, ytdlpOutputExt, fromDate, toDate, cookieSource, maxFilesize, dlFilterFile *string, dlFilters *[]string) {

	if fromDate != nil {
		cmd.Flags().StringVar(fromDate, keys.FromDate, "", "Only grab videos uploaded on or after this date")
	}

	if toDate != nil {
		cmd.Flags().StringVar(toDate, keys.ToDate, "", "Only grab videos uploaded up to this date")
	}

	if ytdlpOutputExt != nil {
		cmd.Flags().StringVar(ytdlpOutputExt, keys.YtdlpOutputExt, "", "The preferred downloaded output format for videos")
	}

	if retries != nil {
		cmd.Flags().IntVar(retries, keys.DLRetries, 0, "Number of retries to attempt a download before failure")
	}

	if cookieSource != nil {
		cmd.Flags().StringVar(cookieSource, keys.CookieSource, "", "Cookie source to use for downloading videos (e.g. firefox)")
	}

	if maxFilesize != nil {
		cmd.Flags().StringVar(maxFilesize, keys.MaxFilesize, "", "Enter your desired yt-dlp max filesize parameter")
	}

	if dlFilters != nil {
		cmd.Flags().StringSliceVar(dlFilters, keys.FilterOpsInput, nil, "Filter in or out videos with certain metafields")
	}

	if dlFilterFile != nil {
		cmd.Flags().StringVar(dlFilterFile, keys.FilterOpsFile, "", "Path to a filter operations file (one operation per line [Format is: 'field:contains/omit:VALUE:must/any'])")
	}
}
