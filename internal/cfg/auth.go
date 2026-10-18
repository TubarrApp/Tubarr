package cfg

import (
	"errors"
	"os"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/server/serverauth"

	"github.com/spf13/cobra"
)

// InitAuthCmds is the entrypoint for initializing web interface login commands.
func InitAuthCmds(s contracts.Store) *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Web interface login commands",
		Long:  "Manage the web interface login, e.g. to recover from a forgotten password.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	authCmd.AddCommand(resetLoginCmd(s.TubarrSettingsStore()))

	return authCmd
}

// resetLoginCmd removes the web interface login, so the next visit sets up a new one.
func resetLoginCmd(ss contracts.TubarrSettingsStore) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Remove the web interface login",
		Long: "Removes the web interface username and password and signs out every browser, so the next visit to the web interface " +
			"asks for a new login. Works while Tubarr is running, e.g. 'docker exec -u tubarr tubarr tubarr auth reset'.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := serverauth.ResetLogin(ss.GetDB(), ss); err != nil {
				return err
			}
			logger.Pl.S("Web interface login removed: the next visit to the web interface will ask for a new one")
			if os.Getenv(consts.EnvWebPassword) != "" {
				logger.Pl.W("%s is set, so the login is set again from it when Tubarr next starts", consts.EnvWebPassword)
			}
			return nil
		},
	}
}
