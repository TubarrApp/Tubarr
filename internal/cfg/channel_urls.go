package cfg

import (
	"fmt"
	"tubarr/internal/cmd"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/spf13/cobra"
)

// updateChannelURLSettingsCmd updates settings for specific URL(s) within a channel.
func updateChannelURLSettingsCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		channelName                                                                                           string
		channelID                                                                                             int
		url                                                                                                   string
		concurrency, crawlFreq, metarrConcurrency, retries                                                    int
		maxCPU                                                                                                float64
		vDir, jDir, outDir                                                                                    string
		videoCodec, audioCodec                                                                                []string
		cookiesFromBrowser                                                                                    string
		minFreeMem, renameStyle, metarrExt                                                                    string
		maxFilesize, externalDownloader, externalDownloaderArgs                                               string
		dlFilters, metaOps, moveOps, filteredMetaOps, filenameOps, filteredFilenameOps                        []string
		dlFilterFile, moveOpsFile, metaOpsFile, filteredMetaOpsFile, filenameOpsFile, filteredFilenameOpsFile string
		useGPU, gpuNode, transcodeQuality, transcodeVideoFilter                                               string
		fromDate, toDate                                                                                      string
		ytdlpOutExt                                                                                           string
		ytdlpPreferredVideoCodecs, ytdlpPreferredAudioCodecs                                                  []string
		useGlobalCookies, pause, resetSettings                                                                bool
		extraYTDLPVideoArgs, extraYTDLPMetaArgs, extraFFmpegArgs                                              string
	)

	updateURLSettingsCmd := &cobra.Command{
		Use:   "update-url-settings",
		Short: "Update settings for URL(s) within a channel.",
		Long:  "Update settings for a specific channel URL, or all URLs if --url is not specified. Use --reset-settings to clear URL-specific overrides and inherit from channel defaults.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate channel name or ID is present.
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			// Get channel model.
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
				for _, cu := range urlsToUpdate {
					cu.ChanURLSettings = nil
					cu.ChanURLMetarrArgs = nil

					if err := cs.UpdateChannelURLSettings(cu); err != nil {
						logger.Pl.E("Failed to reset settings for URL %s: %v", cu.URL, err)
						continue
					}

					logger.Pl.S("Reset settings for URL %q - will now inherit from channel defaults", cu.URL)
				}
				return nil
			}

			// Gather settings update functions.
			fnSettingsArgs, err := getSettingsArgFns(cmd, chanSettings{
				concurrency:               concurrency,
				cookiesFromBrowser:        cookiesFromBrowser,
				crawlFreq:                 crawlFreq,
				externalDownloader:        externalDownloader,
				externalDownloaderArgs:    externalDownloaderArgs,
				filters:                   dlFilters,
				filterFile:                dlFilterFile,
				fromDate:                  fromDate,
				maxFilesize:               maxFilesize,
				metaFilterMoveOps:         moveOps,
				metaFilterMoveOpsFile:     moveOpsFile,
				paused:                    pause,
				retries:                   retries,
				toDate:                    toDate,
				videoDir:                  vDir,
				jsonDir:                   jDir,
				useGlobalCookies:          useGlobalCookies,
				ytdlpOutputExt:            ytdlpOutExt,
				extraYtdlpVideoArgs:       extraYTDLPVideoArgs,
				extraYtdlpMetaArgs:        extraYTDLPMetaArgs,
				ytdlpPreferredVideoCodecs: ytdlpPreferredVideoCodecs,
				ytdlpPreferredAudioCodecs: ytdlpPreferredAudioCodecs,
			})
			if err != nil {
				return err
			}

			// Gather metarr update functions.
			fnMetarrArray, err := getMetarrArgFns(cmd, cobraMetarrArgs{
				renameStyle:     renameStyle,
				extraFFmpegArgs: extraFFmpegArgs,
				metarrExt:       metarrExt,

				metaOps:             metaOps,
				metaOpsFile:         metaOpsFile,
				filteredMetaOps:     filteredMetaOps,
				filteredMetaOpsFile: filteredMetaOpsFile,

				filenameOps:             filenameOps,
				filenameOpsFile:         filenameOpsFile,
				filteredFilenameOps:     filteredFilenameOps,
				filteredFilenameOpsFile: filteredFilenameOpsFile,

				outputDir: outDir,

				concurrency:          metarrConcurrency,
				maxCPU:               maxCPU,
				minFreeMem:           minFreeMem,
				useGPU:               useGPU,
				transcodeVideoCodec:  videoCodec,
				transcodeAudioCodec:  audioCodec,
				transcodeQuality:     transcodeQuality,
				transcodeVideoFilter: transcodeVideoFilter,
			})
			if err != nil {
				return err
			}

			// Apply updates to each URL.
			updatedCount := 0
			for _, cu := range urlsToUpdate {
				// Initialize settings if nil.
				if cu.ChanURLSettings == nil {
					cu.ChanURLSettings = &models.Settings{}
				}
				if cu.ChanURLMetarrArgs == nil {
					cu.ChanURLMetarrArgs = &models.MetarrArgs{}
				}

				// Apply settings updates.
				if len(fnSettingsArgs) > 0 {
					for _, fn := range fnSettingsArgs {
						if err := fn(cu.ChanURLSettings); err != nil {
							return err
						}
					}
				}

				// Apply metarr updates.
				if len(fnMetarrArray) > 0 {
					for _, fn := range fnMetarrArray {
						if err := fn(cu.ChanURLMetarrArgs); err != nil {
							return err
						}
					}
				}

				// Save to database.
				if err := cs.UpdateChannelURLSettings(cu); err != nil {
					logger.Pl.E("Failed to update URL %s: %v", cu.URL, err)
					continue
				}

				logger.Pl.D(1, "Updated settings for URL: %s", cu.URL)
				updatedCount++
			}

			logger.Pl.S("Successfully updated settings for %d URL(s) in channel %q", updatedCount, c.Name)
			return nil
		},
	}

	// Primary identifiers.
	updateURLSettingsCmd.Flags().StringVarP(&channelName, "channel-name", "n", "", "Channel name")
	updateURLSettingsCmd.Flags().IntVar(&channelID, "channel-id", 0, "Channel ID")
	updateURLSettingsCmd.Flags().StringVar(&url, "url", "", "Specific URL to update (if not provided, updates all URLs in channel)")

	// Files/dirs.
	cmd.SetFileDirFlags(updateURLSettingsCmd, nil, &jDir, &vDir)

	// Program related.
	cmd.SetProgramRelatedFlags(updateURLSettingsCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader, &moveOpsFile,
		&moveOps, &pause)

	// Download.
	cmd.SetDownloadFlags(updateURLSettingsCmd, &retries, &useGlobalCookies,
		&ytdlpOutExt, &fromDate, &toDate,
		&cookiesFromBrowser, &maxFilesize, &dlFilterFile,
		&dlFilters, &ytdlpPreferredVideoCodecs, &ytdlpPreferredAudioCodecs)

	// Metarr.
	cmd.SetMetarrFlags(updateURLSettingsCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &minFreeMem,
		&outDir, &renameStyle, &metaOpsFile,
		&filteredMetaOpsFile, &filenameOpsFile, &filteredFilenameOpsFile,
		&filenameOps, &filteredFilenameOps, &metaOps,
		&filteredMetaOps)

	// Transcoding.
	cmd.SetTranscodeFlags(updateURLSettingsCmd, &useGPU, &gpuNode,
		&transcodeVideoFilter, &transcodeQuality, &videoCodec,
		&audioCodec)

	// Additional YTDLP args.
	cmd.SetCustomYDLPArgFlags(updateURLSettingsCmd, &extraYTDLPVideoArgs, &extraYTDLPMetaArgs)

	// Reset flag.
	updateURLSettingsCmd.Flags().BoolVar(&resetSettings, "clear-settings", false, "Clear all URL-specific settings and inherit from channel defaults")

	return updateURLSettingsCmd
}
