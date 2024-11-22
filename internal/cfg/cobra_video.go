package cfg

import (
	"fmt"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
)

// InitChannelCmds is the entrypoint for initializing channel commands.
func initVideoCmds(s models.Store) *cobra.Command {
	vidCmd := &cobra.Command{
		Use:   "video",
		Short: "Video commands",
		Long:  "Manage videos with various subcommands like delete and list.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	vs := s.GetVideoStore()
	cs := s.GetChannelStore()

	// Add subcommands with dependencies
	vidCmd.AddCommand(deleteVideoCmd(vs, cs))

	return vidCmd
}

// deleteVideoCmd deletes a channel from the database.
func deleteVideoCmd(vs models.VideoStore, cs models.ChannelStore) *cobra.Command {
	var (
		chanName, chanURL, url, chanKey, chanVal string
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete video entry",
		Long:  "Delete a video entry from a channel by URL",
		RunE: func(cmd *cobra.Command, args []string) error {

			switch {
			case chanURL != "" && url != "":
				chanKey = consts.QChanURL
				chanVal = chanURL
			case chanName != "" && url != "":
				chanKey = consts.QChanName
				chanVal = chanName
			default:
				return fmt.Errorf("must enter a channel name/URL, and a video URL to delete")
			}

			cid, err := cs.GetID(chanKey, chanVal)
			if err != nil {
				return err
			}

			if err := vs.DeleteVideo(consts.QVidURL, url, cid); err != nil {
				return err
			}
			logging.S(0, "Successfully deleted video with URL %q", url)
			return nil
		},
	}

	// Primary channel elements
	delCmd.Flags().StringVarP(&chanName, "channel-name", "n", "", "Channel name")
	delCmd.Flags().StringVarP(&chanURL, "channel-url", "u", "", "Channel URL")
	delCmd.Flags().StringVar(&url, "delete-url", "", "Video URL")

	return delCmd
}
