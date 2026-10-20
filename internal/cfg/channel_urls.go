package cfg

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/cobra"
)

// urlFlag names the channel URL to update.
const urlFlag = "url"

// updateChannelURLSettingsCmd updates settings for specific URL(s) within a channel.
func updateChannelURLSettingsCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		flags         models.ChannelFlagValues
		channelID     int
		url           string
		resetSettings bool
		clearNames    []string
	)

	updateURLSettingsCmd := &cobra.Command{
		Use:   "update-url-settings",
		Short: "Update settings for URL(s) within a channel.",
		Long:  fmt.Sprintf("Update settings for a specific channel URL, or all URLs if --%s is not specified. Use --%s to clear a URL's own settings so it uses the channel's, or --%s to clear only those named.", urlFlag, keys.ClearAllSettings, keys.ClearSetting),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate channel name or ID is present.
			key, val, err := getChanKeyVal(channelID, flags.Name)
			if err != nil {
				return err
			}

			// As saved, as the URLs' settings are saved back.
			c, hasRows, err := cs.GetChannelModel(key, val, false)
			if err != nil {
				return err
			}
			if !hasRows {
				return fmt.Errorf("channel not found with %s %q", key, val)
			}

			// Determine which URLs to update.
			var urlsToUpdate []*models.ChannelURL

			if url != "" {
				// Update specific URL.
				cu, hasRows, err := cs.GetChannelURLModel(c.ID, url, false)
				if err != nil {
					return err
				}
				if !hasRows {
					return fmt.Errorf("URL %q not found for channel %q", url, c.Name)
				}
				urlsToUpdate = append(urlsToUpdate, cu)
				logger.Pl.I("Updating settings for URL: %s", url)
			} else {
				// Update all URLs.
				urlsToUpdate = c.URLModels
				logger.Pl.I("Updating settings for %d URL(s) in channel %q", len(urlsToUpdate), c.Name)
			}

			// If reset flag is set, clear settings to inherit from channel.
			if resetSettings {
				var errs []error
				for _, cu := range urlsToUpdate {
					cu.ChanURLSettings = nil
					cu.ChanURLMetarrArgs = nil

					if err := cs.UpdateChannelURLSettings(cu); err != nil {
						errs = append(errs, fmt.Errorf("failed to reset settings for URL %q: %w", cu.URL, err))
						continue
					}

					logger.Pl.S("Reset settings for URL %q - will now inherit from channel defaults", cu.URL)
				}
				return errors.Join(errs...)
			}

			// Apply the settings given to every URL before saving any, so invalid ones leave all unchanged.
			var input models.ChannelInput
			mergeFlagsIntoInput(cmd, &flags, &input)
			var changed []*models.ChannelURL
			for _, cu := range urlsToUpdate {
				oldS := cmp.Or(cu.ChanURLSettings, &models.Settings{})
				oldM := cmp.Or(cu.ChanURLMetarrArgs, &models.MetarrArgs{})
				s, m := *oldS, *oldM
				if err := clearURLSettings(&s, &m, clearNames); err != nil {
					return err
				}
				if err := parsing.ApplySettingsInput(&s, &input.SettingsInput); err != nil {
					return fmt.Errorf("URL %q: %w", cu.URL, err)
				}
				if err := parsing.ApplyMetarrArgsInput(&m, &input.MetarrArgsInput); err != nil {
					return fmt.Errorf("URL %q: %w", cu.URL, err)
				}
				if reflect.DeepEqual(s, *oldS) && reflect.DeepEqual(m, *oldM) {
					continue
				}
				cu.ChanURLSettings, cu.ChanURLMetarrArgs = &s, &m
				changed = append(changed, cu)
			}

			// Save to database.
			updatedCount := 0
			var errs []error
			for _, cu := range changed {
				if err := cs.UpdateChannelURLSettings(cu); err != nil {
					errs = append(errs, fmt.Errorf("failed to update URL %q: %w", cu.URL, err))
					continue
				}

				logger.Pl.D(1, "Updated settings for URL: %s", cu.URL)
				updatedCount++
			}

			if updatedCount > 0 || len(errs) == 0 {
				logger.Pl.S("Successfully updated settings for %d URL(s) in channel %q", updatedCount, c.Name)
			}
			return errors.Join(errs...)
		},
	}

	// Primary identifiers.
	updateURLSettingsCmd.Flags().StringVarP(&flags.Name, "channel-name", "n", "", "Channel name")
	updateURLSettingsCmd.Flags().IntVar(&channelID, "channel-id", 0, "Channel ID")
	updateURLSettingsCmd.Flags().StringVar(&url, urlFlag, "", "Specific URL to update (if not provided, updates all URLs in channel)")

	// Files/dirs.
	cmd.SetFileDirFlags(updateURLSettingsCmd, nil, &flags.JSONDir, &flags.VideoDir)

	// Settings.
	setChannelSettingsFlags(updateURLSettingsCmd, &flags)

	// Reset flags.
	updateURLSettingsCmd.Flags().BoolVar(&resetSettings, keys.ClearAllSettings, false, "Clear all URL-specific settings and inherit from channel defaults")
	updateURLSettingsCmd.Flags().StringSliceVar(&clearNames, keys.ClearSetting, nil, fmt.Sprintf("Clear these URL-specific settings, named by flag, so they use the channel's (e.g. %s,%s)", keys.AutoDownload, keys.CrawlFreq))

	return updateURLSettingsCmd
}

// clearURLSettings clears the URL settings named by flag (with or without "--"), so the URL uses the channel's.
func clearURLSettings(s *models.Settings, m *models.MetarrArgs, names []string) error {
	for _, name := range names {
		switch strings.TrimPrefix(name, "--") {
		// Settings.
		case keys.VideoDir:
			s.VideoDir = ""
		case keys.JSONDir:
			s.JSONDir = ""
		case keys.CookiesFromBrowser:
			s.CookiesFromBrowser = ""
		case keys.ExternalDownloader:
			s.ExternalDownloader = ""
		case keys.ExternalDownloaderArgs:
			s.ExternalDownloaderArgs = ""
		case keys.MaxFilesize:
			s.MaxFilesize = ""
		case keys.YtdlpOutputExt:
			s.YtdlpOutputExt = ""
		case keys.FromDate:
			s.FromDate = ""
		case keys.ToDate:
			s.ToDate = ""
		case keys.YTDLPPreferredVideoCodecs:
			s.YTDLPPreferredVideoCodecs = nil
		case keys.YTDLPPreferredAudioCodecs:
			s.YTDLPPreferredAudioCodecs = nil
		case keys.UseGlobalCookies:
			s.UseGlobalCookies = nil
		case keys.AutoDownload:
			s.AutoDownload = nil
		case keys.FilterOpsInput:
			s.Filters = nil
		case keys.FilterOpsFile:
			s.FilterFile = ""
		case keys.MoveOps:
			s.MetaFilterMoveOps = nil
		case keys.MoveOpsFile:
			s.MetaFilterMoveOpFile = ""
		case keys.ExtraYTDLPVideoArgs:
			s.ExtraYTDLPVideoArgs = ""
		case keys.ExtraYTDLPMetaArgs:
			s.ExtraYTDLPMetaArgs = ""
		case keys.ExtraYTDLPCrawlArgs:
			s.ExtraYTDLPCrawlArgs = ""
		case keys.CrawlFreq:
			s.CrawlFreq = 0
		case keys.DownloadConcurrency:
			s.Concurrency = 0
		case keys.DLRetries:
			s.Retries = 0
		case keys.Pause:
			s.Paused = false

		// Metarr arguments.
		case keys.MOutputDir:
			m.OutputDir = ""
		case keys.MMetaPurge:
			m.MetaPurge = ""
		case keys.MOutputExt:
			m.OutputExt = ""
		case keys.MRenameStyle:
			m.RenameStyle = ""
		case keys.MMetaOpsFile:
			m.MetaOpsFile = ""
		case keys.MFilteredMetaOpsFile:
			m.FilteredMetaOpsFile = ""
		case keys.MFilenameOpsFile:
			m.FilenameOpsFile = ""
		case keys.MFilteredFilenameOpsFile:
			m.FilteredFilenameOpsFile = ""
		case keys.MMetaOps:
			m.MetaOps = nil
		case keys.MFilenameOps:
			m.FilenameOps = nil
		case keys.MFilteredMetaOps:
			m.FilteredMetaOps = nil
		case keys.MFilteredFilenameOps:
			m.FilteredFilenameOps = nil
		case keys.TranscodeGPU:
			m.TranscodeGPU = ""
		case keys.TranscodeGPUNode:
			m.TranscodeGPUNode = ""
		case keys.TranscodeQuality:
			m.TranscodeQuality = ""
		case keys.TranscodeVideoFilter:
			m.TranscodeVideoFilter = ""
		case keys.TranscodeCodec:
			m.TranscodeVideoCodecs = nil
		case keys.TranscodeAudioCodec:
			m.TranscodeAudioCodecs = nil
		case keys.MMinFreeMem:
			m.MinFreeMem = ""
		case keys.MMaxCPU:
			m.MaxCPU = 0
		case keys.MConcurrency:
			m.Concurrency = 0
		case keys.MExtraFFmpegArgs:
			m.ExtraFFmpegArgs = ""

		default:
			return fmt.Errorf("--%s: %q isn't a URL setting's flag", keys.ClearSetting, name)
		}
	}
	return nil
}
