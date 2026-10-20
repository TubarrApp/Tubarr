package cfg

import (
	"errors"
	"fmt"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"

	"github.com/spf13/cobra"
)

// InitVideoCmds is the entrypoint for initializing video commands.
func InitVideoCmds(s contracts.Store) *cobra.Command {
	vidCmd := &cobra.Command{
		Use:   "video",
		Short: "Video commands",
		Long:  "Manage videos with various subcommands like delete and list.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	// Add subcommands with dependencies.
	vidCmd.AddCommand(deleteCmdVideo(s.ChannelStore()))

	return vidCmd
}

// deleteCmdVideo deletes a channel from the database.
func deleteCmdVideo(cs contracts.ChannelStore) *cobra.Command {
	var (
		chanName    string
		urls        []string
		chanID      int
		deleteFiles bool
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete video entry",
		Long:  fmt.Sprintf("Deletes video entries from a channel by URL, so later crawls treat them as new. Their video and metadata files are kept unless --%s is given.", keys.DeleteFiles),
		RunE: func(_ *cobra.Command, _ []string) error {
			if len(urls) == 0 {
				return errors.New("must enter a video URL to delete")
			}

			cID, _, _, err := lookupChannelID(cs, chanID, chanName)
			if err != nil {
				return err
			}

			// Delete video.
			if err := cs.DeleteVideosByURLs(cID, urls, deleteFiles); err != nil {
				return err
			}
			logger.Pl.S("Successfully deleted videos with URLs: %v", urls)
			return nil
		},
	}

	// Primary channel elements.
	cmd.SetPrimaryChannelFlags(delCmd, &chanName, nil, &chanID)
	delCmd.Flags().StringSliceVar(&urls, "delete-urls", []string{}, "Video URLs to delete")
	delCmd.Flags().BoolVar(&deleteFiles, keys.DeleteFiles, false, "Also delete the videos' video and metadata files from disk.")

	return delCmd
}
