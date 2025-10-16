package cfg

import (
	"errors"
	"fmt"
	"tubarr/internal/contracts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
)

// updateChannelURLSettingsCmd updates settings for a specific URL within a channel.
func updateChannelURLSettingsCmd(cs contracts.ChannelStore) *cobra.Command {
	var (
		channelName                                                               string
		channelID                                                                 int
		url                                                                       string
		concurrency, crawlFreq, metarrConcurrency, retries                        int
		maxCPU                                                                    float64
		vDir, jDir, outDir                                                        string
		urlOutDirs                                                                []string
		cookieSource                                                              string
		minFreeMem, renameStyle, filenameDateTag, metarrExt                       string
		maxFilesize, externalDownloader, externalDownloaderArgs                   string
		dlFilters, metaOps, moveOps                                               []string
		dlFilterFile, moveOpsFile                                                 string
		fileSfxReplace                                                            []string
		useGPU, gpuDir, codec, audioCodec, transcodeQuality, transcodeVideoFilter string
		fromDate, toDate                                                          string
		ytdlpOutExt                                                               string
		useGlobalCookies, pause, resetSettings                                    bool
		extraYTDLPVideoArgs, extraYTDLPMetaArgs, extraFFmpegArgs                  string
	)

	updateURLSettingsCmd := &cobra.Command{
		Use:   "update-url-settings",
		Short: "Update settings for a specific URL within a channel.",
		Long:  "Update settings for a specific channel URL. Use --reset-settings to clear URL-specific overrides and inherit from channel defaults.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate we have either channel name or ID
			key, val, err := getChanKeyVal(channelID, channelName)
			if err != nil {
				return err
			}

			// Validate URL is provided
			if url == "" {
				return errors.New("must provide --url to specify which URL to update")
			}

			// Get channel ID
			id, err := cs.GetChannelID(key, val)
			if err != nil {
				return err
			}
			if id == 0 {
				return fmt.Errorf("could not find channel with %s %q", key, val)
			}

			// Get the ChannelURL model
			cu, hasRows, err := cs.GetChannelURLModel(id, url)
			if err != nil {
				return err
			}
			if !hasRows {
				return fmt.Errorf("URL %q not found for channel with %s %q", url, key, val)
			}

			// If reset flag is set, clear settings to inherit from channel
			if resetSettings {
				cu.ChanURLSettings = nil
				cu.ChanURLMetarrArgs = nil

				if err := cs.UpdateChannelURLSettings(cu); err != nil {
					return err
				}

				logging.S("Reset settings for URL %q - will now inherit from channel defaults", url)
				return nil
			}

			// Initialize settings if nil
			if cu.ChanURLSettings == nil {
				cu.ChanURLSettings = &models.Settings{}
			}
			if cu.ChanURLMetarrArgs == nil {
				cu.ChanURLMetarrArgs = &models.MetarrArgs{}
			}

			// Gather settings update functions
			fnSettingsArgs, err := getSettingsArgFns(cmd, chanSettings{
				concurrency:            concurrency,
				cookieSource:           cookieSource,
				crawlFreq:              crawlFreq,
				externalDownloader:     externalDownloader,
				externalDownloaderArgs: externalDownloaderArgs,
				filters:                dlFilters,
				filterFile:             dlFilterFile,
				fromDate:               fromDate,
				maxFilesize:            maxFilesize,
				moveOps:                moveOps,
				moveOpsFile:            moveOpsFile,
				paused:                 pause,
				retries:                retries,
				toDate:                 toDate,
				videoDir:               vDir,
				jsonDir:                jDir,
				useGlobalCookies:       useGlobalCookies,
				ytdlpOutputExt:         ytdlpOutExt,
				extraYtdlpVideoArgs:    extraYTDLPVideoArgs,
				extraYtdlpMetaArgs:     extraYTDLPMetaArgs,
			})
			if err != nil {
				return err
			}

			// Apply settings updates
			if len(fnSettingsArgs) > 0 {
				for _, fn := range fnSettingsArgs {
					if err := fn(cu.ChanURLSettings); err != nil {
						return err
					}
				}
			}

			// Gather metarr update functions
			fnMetarrArray, err := getMetarrArgFns(cmd, cobraMetarrArgs{
				filenameReplaceSfx:   fileSfxReplace,
				renameStyle:          renameStyle,
				extraFFmpegArgs:      extraFFmpegArgs,
				filenameDateTag:      filenameDateTag,
				metarrExt:            metarrExt,
				metaOps:              metaOps,
				outputDir:            outDir,
				urlOutputDirs:        urlOutDirs,
				concurrency:          metarrConcurrency,
				maxCPU:               maxCPU,
				minFreeMem:           minFreeMem,
				useGPU:               useGPU,
				gpuDir:               gpuDir,
				transcodeCodec:       codec,
				transcodeAudioCodec:  audioCodec,
				transcodeQuality:     transcodeQuality,
				transcodeVideoFilter: transcodeVideoFilter,
			})
			if err != nil {
				return err
			}

			// Apply metarr updates
			if len(fnMetarrArray) > 0 {
				for _, fn := range fnMetarrArray {
					if err := fn(cu.ChanURLMetarrArgs); err != nil {
						return err
					}
				}
			}

			// Save to database
			if err := cs.UpdateChannelURLSettings(cu); err != nil {
				return err
			}

			logging.S("Updated settings for URL %q in channel with %s %q", url, key, val)
			return nil
		},
	}

	// Primary identifiers
	updateURLSettingsCmd.Flags().StringVarP(&channelName, "channel-name", "n", "", "Channel name")
	updateURLSettingsCmd.Flags().IntVar(&channelID, "channel-id", 0, "Channel ID")
	updateURLSettingsCmd.Flags().StringVar(&url, "url", "", "URL to update (required)")
	updateURLSettingsCmd.MarkFlagRequired("url")

	// Files/dirs
	setFileDirFlags(updateURLSettingsCmd, nil, &jDir, &vDir)

	// Program related
	setProgramRelatedFlags(updateURLSettingsCmd, &concurrency, &crawlFreq,
		&externalDownloaderArgs, &externalDownloader, &moveOpsFile,
		&moveOps, &pause, true)

	// Download
	setDownloadFlags(updateURLSettingsCmd, &retries, &useGlobalCookies,
		&ytdlpOutExt, &fromDate, &toDate,
		&cookieSource, &maxFilesize, &dlFilterFile,
		&dlFilters)

	// Metarr
	setMetarrFlags(updateURLSettingsCmd, &maxCPU, &metarrConcurrency,
		&metarrExt, &extraFFmpegArgs, &filenameDateTag,
		&minFreeMem, &outDir, &renameStyle,
		&urlOutDirs, &fileSfxReplace, &metaOps)

	// Transcoding
	setTranscodeFlags(updateURLSettingsCmd, &useGPU, &gpuDir,
		&transcodeVideoFilter, &codec, &audioCodec,
		&transcodeQuality)

	// Additional YTDLP args
	setCustomYDLPArgFlags(updateURLSettingsCmd, &extraYTDLPVideoArgs, &extraYTDLPMetaArgs)

	// Reset flag
	updateURLSettingsCmd.Flags().BoolVar(&resetSettings, "reset", false, "Clear all URL-specific settings and inherit from channel defaults")

	return updateURLSettingsCmd
}
