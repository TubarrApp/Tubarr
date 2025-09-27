// Package cfgvideo sets up Cobra video commands.
package cfgvideo

import (
	"errors"
	"strconv"
	cfgflags "tubarr/internal/cfg/flags"
	"tubarr/internal/domain/consts"
	"tubarr/internal/interfaces"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
)

// InitVideoCmds is the entrypoint for initializing video commands.
func InitVideoCmds(s interfaces.Store) *cobra.Command {
	vidCmd := &cobra.Command{
		Use:   "video",
		Short: "Video commands",
		Long:  "Manage videos with various subcommands like delete and list.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	vs := s.VideoStore()
	cs := s.ChannelStore()

	// Add subcommands with dependencies
	vidCmd.AddCommand(deletecmdvideo(vs, cs))

	return vidCmd
}

// deletecmdvideo deletes a channel from the database.
func deletecmdvideo(vs interfaces.VideoStore, cs interfaces.ChannelStore) *cobra.Command {
	var (
		chanName, chanKey, chanVal, url string
		chanID                          int
	)

	delCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete video entry",
		Long:  "Delete a video entry from a channel by URL",
		RunE: func(cmd *cobra.Command, args []string) error {

			switch {
			case chanName != "":
				chanKey = consts.QChanName
				chanVal = chanName
			case chanID != 0:
				chanKey = consts.QChanID
				chanVal = strconv.Itoa(chanID)
			default:
				return errors.New("must enter a channel name/URL, and a video URL to delete")
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
	cfgflags.SetPrimaryChannelFlags(delCmd, &chanName, nil, &chanID)
	delCmd.Flags().StringVar(&url, "delete-url", "", "Video URL")

	return delCmd
}
