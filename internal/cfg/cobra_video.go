package cfg

import (
	"fmt"
	"tubarr/internal/domain/consts"
	"tubarr/internal/interfaces"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
)

// InitChannelCmds is the entrypoint for initializing channel commands
func initVideoCmds(s interfaces.Store) *cobra.Command {
	vidCmd := &cobra.Command{
		Use:   "video",
		Short: "Video commands",
		Long:  "Manage videos with various subcommands like delete and list.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	vs := s.GetVideoStore()

	// Add subcommands with dependencies
	vidCmd.AddCommand(deleteVideoCmd(vs))

	return vidCmd
}

// deleteVideoCmd deletes a channel from the database
func deleteVideoCmd(vs interfaces.VideoStore) *cobra.Command {
	var url string

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete video entry",
		Long:  "Delete a video entry from the database by URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				return fmt.Errorf("must enter a URL")
			}

			if err := vs.DeleteVideo(consts.QVidURL, url); err != nil {
				return err
			}
			logging.S(0, "Successfully deleted video with URL '%s'", url)
			return nil
		},
	}

	// Primary channel elements
	delCmd.Flags().StringVarP(&url, "url", "u", "", "Channel URL")

	return delCmd
}
