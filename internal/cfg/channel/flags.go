package cfgchannel

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// SetPrimaryChannelFlags sets the main flags for channels in, or intended for, the database.
func SetPrimaryChannelFlags(cmd *cobra.Command, name, url *string, id *int) {
	if id != nil {
		cmd.Flags().IntVarP(id, keys.ID, "i", 0, "Channel ID in the DB")
	}
	if name != nil {
		cmd.Flags().StringVarP(name, keys.Name, "n", "", "Channel name")
	}
	if url != nil {
		cmd.Flags().StringVarP(url, keys.URL, "u", "", "Channel URL")
	}
}
