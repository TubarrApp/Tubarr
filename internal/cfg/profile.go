package cfg

import (
	"errors"
	"fmt"
	"strconv"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/cobra"
)

// InitProfileCmds is the entrypoint for initializing profile commands.
func InitProfileCmds(cs contracts.ChannelStore) *cobra.Command {
	profileCmd := &cobra.Command{
		Use:   "profile",
		Short: "Profile commands.",
		Long:  "Manage profiles, named sets of settings filling in those a channel or channel URL using one leaves unset.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("please specify a subcommand. Use --help to see available subcommands")
		},
	}

	profileCmd.AddCommand(addProfileCmd(cs))
	profileCmd.AddCommand(editProfileCmd(cs))
	profileCmd.AddCommand(deleteProfileCmd(cs))
	profileCmd.AddCommand(listProfilesCmd(cs))

	return profileCmd
}

// addProfileCmd adds a profile.
func addProfileCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		flags models.ChannelFlagValues
		name  string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a profile.",
		Long:  fmt.Sprintf("Adds a profile with the settings given. Give it to channels or channel URLs with --%s.", keys.Profile),
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := &models.Profile{Name: name, ProfileSettings: &models.Settings{}, ProfileMetarrArgs: &models.MetarrArgs{}}
			if err := applyProfileFlags(cmd, &flags, p); err != nil {
				return err
			}
			return cs.AddProfile(p)
		},
	}

	addCmd.Flags().StringVar(&name, keys.ProfileName, "", "Profile name")
	setProfileFlags(addCmd, &flags)
	return addCmd
}

// editProfileCmd changes a profile's settings.
func editProfileCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		flags   models.ChannelFlagValues
		name    string
		newName string
	)

	editCmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a profile.",
		Long:  "Changes the settings given on a profile, an empty value clearing one. Channels and URLs using it get the new settings.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := lookupProfile(cs, name)
			if err != nil {
				return err
			}
			if err := applyProfileFlags(cmd, &flags, p); err != nil {
				return err
			}
			if newName != "" {
				p.Name = newName
			}
			return cs.UpdateProfile(p)
		},
	}

	editCmd.Flags().StringVar(&name, keys.ProfileName, "", "Profile name")
	editCmd.Flags().StringVar(&newName, keys.ChangeNameTo, "", "Change the profile name to this value")
	setProfileFlags(editCmd, &flags)
	return editCmd
}

// deleteProfileCmd deletes a profile.
func deleteProfileCmd(cs contracts.ChannelStore) *cobra.Command {
	var name string

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a profile.",
		Long:  "Deletes a profile, once no channel or channel URL uses it.",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := lookupProfile(cs, name)
			if err != nil {
				return err
			}
			return cs.DeleteProfile(consts.QProfileID, strconv.FormatInt(p.ID, 10))
		},
	}

	deleteCmd.Flags().StringVar(&name, keys.ProfileName, "", "Profile name")
	return deleteCmd
}

// listProfilesCmd lists profiles.
func listProfilesCmd(cs contracts.ChannelStore) *cobra.Command {
	var name string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List profiles.",
		Long:  "Lists every profile, or the one named, with its settings and the channels and URLs using it.",
		RunE: func(_ *cobra.Command, _ []string) error {
			// One profile.
			if name != "" {
				p, err := lookupProfile(cs, name)
				if err != nil {
					return err
				}
				cs.DisplayProfile(p)
				return nil
			}

			// Every profile.
			profiles, err := cs.GetAllProfiles()
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				logger.Pl.I("No profiles in the database")
				return nil
			}
			for _, p := range profiles {
				cs.DisplayProfile(p)
			}
			return nil
		},
	}

	listCmd.Flags().StringVar(&name, keys.ProfileName, "", "Only list this profile")
	return listCmd
}

// ******************************** Private ********************************

// setProfileFlags registers the flags for a profile's settings: a channel's, bar pausing.
func setProfileFlags(c *cobra.Command, f *models.ChannelFlagValues) {
	cmd.SetFileDirFlags(c, nil, &f.JSONDir, &f.VideoDir)
	setChannelSettingsFlags(c, f)
	_ = c.Flags().MarkHidden(keys.Pause) // Channels and URLs pause, not profiles.
}

// applyProfileFlags applies the settings flags given to a profile.
func applyProfileFlags(cmd *cobra.Command, f *models.ChannelFlagValues, p *models.Profile) error {
	if cmd.Flags().Changed(keys.Pause) {
		return fmt.Errorf("--%s pauses channels and channel URLs, not profiles", keys.Pause)
	}
	var input models.ChannelInput
	mergeFlagsIntoInput(cmd, f, &input)
	if err := parsing.ApplySettingsInput(p.ProfileSettings, &input.SettingsInput); err != nil {
		return err
	}
	return parsing.ApplyMetarrArgsInput(p.ProfileMetarrArgs, &input.MetarrArgsInput)
}

// lookupProfile returns the profile with the given name.
func lookupProfile(cs contracts.ChannelStore, name string) (*models.Profile, error) {
	if name == "" {
		return nil, fmt.Errorf("please enter a profile name with --%s", keys.ProfileName)
	}
	p, found, err := cs.GetProfile(consts.QProfileName, name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("no profile named %q", name)
	}
	return p, nil
}
