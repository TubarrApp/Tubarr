package cfgflags

import (
	"tubarr/internal/domain/keys"

	"github.com/spf13/cobra"
)

// SetAuthFlags sets flags related to channel authorization.
func SetAuthFlags(cmd *cobra.Command, username, password, loginURL *string) {
	if username != nil {
		cmd.Flags().StringVar(username, keys.Username, "", "Username for authentication.")
	}

	if password != nil {
		cmd.Flags().StringVar(password, keys.Password, "", "Password for authentication.")
	}

	if loginURL != nil {
		cmd.Flags().StringVar(loginURL, keys.LoginURL, "", "Login URL for authentication.")
	}
}
