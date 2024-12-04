package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// SetDownloadFlags sets flags related to download tasks.
func SetDownloadFlags(cmd *cobra.Command, retries *int, cookieSource, maxFilesize *string, dlFilters *[]string) {
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
}
