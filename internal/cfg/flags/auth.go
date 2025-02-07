package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// SetAuthFlags sets flags related to channel authorization.
func SetAuthFlags(cmd *cobra.Command, username, password, loginURL *[]string) {
	if username != nil {
		cmd.Flags().StringSliceVar(username, keys.AuthUsername, nil, "Username for authentication.")
	}

	if password != nil {
		cmd.Flags().StringSliceVar(password, keys.AuthPassword, nil, "Password for authentication.")
	}

	if loginURL != nil {
		cmd.Flags().StringSliceVar(loginURL, keys.AuthURL, nil, "Login URL for authentication.")
	}
}
