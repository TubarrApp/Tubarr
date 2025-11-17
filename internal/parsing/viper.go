package parsing

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"tubarr/internal/auth"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// BuildChannelFromInput builds a Channel model from the unified ChannelInputPtrs struct.
// It performs all validation and returns a fully prepared channel + auth map.
func BuildChannelFromInput(input models.ChannelInputPtrs) (
	*models.Channel,
	map[string]*models.ChannelAccessDetails,
	error,
) {
	// Validate required fields
	if input.VideoDir == nil || *input.VideoDir == "" ||
		input.Name == nil || *input.Name == "" ||
		input.URLs == nil || len(*input.URLs) == 0 {
		return nil, nil, fmt.Errorf("channels require a video directory, name, and at least one channel URL")
	}

	// Default JSONDir to VideoDir
	if input.JSONDir == nil {
		input.JSONDir = input.VideoDir
	}

	// Validate the video directory exists
	if _, err := validation.ValidateDirectory(*input.VideoDir, true); err != nil {
		return nil, nil, err
	}

	// Channel config file
	if input.ChannelConfigFile != nil {
		if _, err := validation.ValidateFile(*input.ChannelConfigFile, false); err != nil {
			return nil, nil, err
		}
	}

	// ===============================
	// Parse filter operations
	// ===============================

	var dlFilterModels []models.Filters
	if input.DLFilters != nil {
		m, err := ParseFilterOps(*input.DLFilters)
		if err != nil {
			return nil, nil, err
		}
		dlFilterModels = m
	}

	moveOpsModels, err := ParseMetaFilterMoveOps(NilOrZeroValue(input.MoveOps))
	if err != nil {
		return nil, nil, err
	}

	var metaOpsModels []models.MetaOps
	if input.MetaOps != nil {
		m, err := ParseMetaOps(*input.MetaOps)
		if err != nil {
			return nil, nil, err
		}
		metaOpsModels = m
	}

	var filenameOpsModels []models.FilenameOps
	if input.FilenameOps != nil {
		m, err := ParseFilenameOps(*input.FilenameOps)
		if err != nil {
			return nil, nil, err
		}
		filenameOpsModels = m
	}

	var filteredMetaOpsModels []models.FilteredMetaOps
	if input.FilteredMetaOps != nil {
		m, err := ParseFilteredMetaOps(*input.FilteredMetaOps)
		if err != nil {
			return nil, nil, err
		}
		filteredMetaOpsModels = m
	}

	var filteredFilenameOpsModels []models.FilteredFilenameOps
	if input.FilteredFilenameOps != nil {
		m, err := ParseFilteredFilenameOps(*input.FilteredFilenameOps)
		if err != nil {
			return nil, nil, err
		}
		filteredFilenameOpsModels = m
	}

	// ===============================
	// Validate rename style, dates, GPU, codecs, etc.
	// ===============================

	if input.RenameStyle != nil && *input.RenameStyle != "" {
		if err := validation.ValidateRenameFlag(*input.RenameStyle); err != nil {
			return nil, nil, err
		}
	}

	if input.MinFreeMem != nil && *input.MinFreeMem != "" {
		if _, err := sharedvalidation.ValidateMinFreeMem(*input.MinFreeMem); err != nil {
			return nil, nil, err
		}
	}

	if input.FromDate != nil && *input.FromDate != "" {
		v, err := validation.ValidateToFromDate(*input.FromDate)
		if err != nil {
			return nil, nil, err
		}
		input.FromDate = &v
	}

	if input.ToDate != nil && *input.ToDate != "" {
		v, err := validation.ValidateToFromDate(*input.ToDate)
		if err != nil {
			return nil, nil, err
		}
		input.ToDate = &v
	}

	if input.TranscodeGPU != nil && *input.TranscodeGPU != "" {
		g, d, err := validation.ValidateGPU(*input.TranscodeGPU, NilOrZeroValue(input.GPUDir))
		if err != nil {
			return nil, nil, err
		}
		input.TranscodeGPU = &g
		input.GPUDir = &d
	}

	if input.VideoCodec != nil && len(*input.VideoCodec) != 0 {
		c, err := validation.ValidateVideoTranscodeCodecSlice(*input.VideoCodec, NilOrZeroValue(input.TranscodeGPU))
		if err != nil {
			return nil, nil, err
		}
		input.VideoCodec = &c
	}

	if input.AudioCodec != nil && len(*input.AudioCodec) != 0 {
		c, err := validation.ValidateAudioTranscodeCodecSlice(*input.AudioCodec)
		if err != nil {
			return nil, nil, err
		}
		input.AudioCodec = &c
	}

	if input.TranscodeQuality != nil && *input.TranscodeQuality != "" {
		q, err := sharedvalidation.ValidateTranscodeQuality(*input.TranscodeQuality)
		if err != nil {
			return nil, nil, err
		}
		input.TranscodeQuality = &q
	}

	if input.YTDLPOutputExt != nil && *input.YTDLPOutputExt != "" {
		v := strings.ToLower(*input.YTDLPOutputExt)
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return nil, nil, err
		}
		input.YTDLPOutputExt = &v
	}

	// ===============================
	// Authentication
	// ===============================

	authMap, err := auth.ParseAuthDetails(
		NilOrZeroValue(input.Username),
		NilOrZeroValue(input.Password),
		NilOrZeroValue(input.LoginURL),
		NilOrZeroValue(input.AuthDetails),
		NilOrZeroValue(input.URLs),
		false,
	)
	if err != nil {
		return nil, nil, err
	}

	// ===============================
	// Build URLModels with per-URL overrides
	// ===============================

	now := time.Now()
	var chanURLs []*models.ChannelURL

	for _, u := range *input.URLs {
		if u == "" {
			continue
		}

		var parsedUsername, parsedPassword, parsedLoginURL string
		if a, exists := authMap[u]; exists {
			parsedUsername = a.Username
			parsedPassword = a.Password
			parsedLoginURL = a.LoginURL
		}

		chanURL := &models.ChannelURL{
			URL:       u,
			Username:  parsedUsername,
			Password:  parsedPassword,
			LoginURL:  parsedLoginURL,
			LastScan:  now,
			CreatedAt: now,
			UpdatedAt: now,
		}

		// Per-URL overrides
		if input.URLSettings != nil {
			nURL := strings.ToLower(u)
			if override, ok := input.URLSettings[nURL]; ok {
				if override.Settings != nil {
					s, err := buildSettingsFromInput(override.Settings)
					if err != nil {
						return nil, nil, fmt.Errorf("per-URL settings error for %s: %w", u, err)
					}
					chanURL.ChanURLSettings = s
				}
				if override.Metarr != nil {
					m, err := buildMetarrArgsFromInput(override.Metarr)
					if err != nil {
						return nil, nil, fmt.Errorf("per-URL metarr error for %s: %w", u, err)
					}
					chanURL.ChanURLMetarrArgs = m
				}
			}
		}

		chanURLs = append(chanURLs, chanURL)
	}

	// ===============================
	// Create the Channel model
	// ===============================

	c := &models.Channel{
		URLModels:  chanURLs,
		Name:       *input.Name,
		ConfigFile: NilOrZeroValue(input.ChannelConfigFile),

		ChanSettings: &models.Settings{
			Concurrency:            NilOrZeroValue(input.Concurrency),
			CookiesFromBrowser:     NilOrZeroValue(input.CookiesFromBrowser),
			CrawlFreq:              NilOrZeroValue(input.CrawlFreq),
			ExternalDownloader:     NilOrZeroValue(input.ExternalDownloader),
			ExternalDownloaderArgs: NilOrZeroValue(input.ExternalDownloaderArgs),
			Filters:                dlFilterModels,
			FilterFile:             NilOrZeroValue(input.DLFilterFile),
			FromDate:               NilOrZeroValue(input.FromDate),
			JSONDir:                *input.JSONDir,
			MaxFilesize:            NilOrZeroValue(input.MaxFilesize),
			MetaFilterMoveOps:      moveOpsModels,
			MetaFilterMoveOpFile:   NilOrZeroValue(input.MoveOpFile),
			Paused:                 NilOrZeroValue(input.Pause),
			Retries:                NilOrZeroValue(input.Retries),
			ToDate:                 NilOrZeroValue(input.ToDate),
			UseGlobalCookies:       NilOrZeroValue(input.UseGlobalCookies),
			VideoDir:               *input.VideoDir,
			YtdlpOutputExt:         NilOrZeroValue(input.YTDLPOutputExt),
			ExtraYTDLPVideoArgs:    NilOrZeroValue(input.ExtraYTDLPVideoArgs),
			ExtraYTDLPMetaArgs:     NilOrZeroValue(input.ExtraYTDLPMetaArgs),
		},

		ChanMetarrArgs: &models.MetarrArgs{
			OutputExt:               NilOrZeroValue(input.MetarrExt),
			ExtraFFmpegArgs:         NilOrZeroValue(input.ExtraFFmpegArgs),
			MetaOps:                 metaOpsModels,
			MetaOpsFile:             NilOrZeroValue(input.MetaOpsFile),
			FilteredMetaOps:         filteredMetaOpsModels,
			FilteredMetaOpsFile:     NilOrZeroValue(input.FilteredMetaOpsFile),
			FilenameOps:             filenameOpsModels,
			FilenameOpsFile:         NilOrZeroValue(input.FilenameOpsFile),
			FilteredFilenameOps:     filteredFilenameOpsModels,
			FilteredFilenameOpsFile: NilOrZeroValue(input.FilteredFilenameOpsFile),
			RenameStyle:             NilOrZeroValue(input.RenameStyle),
			MaxCPU:                  NilOrZeroValue(input.MaxCPU),
			MinFreeMem:              NilOrZeroValue(input.MinFreeMem),
			OutputDir:               NilOrZeroValue(input.OutDir),
			URLOutputDirs:           NilOrZeroValue(input.URLOutputDirs),
			Concurrency:             NilOrZeroValue(input.MetarrConcurrency),
			UseGPU:                  NilOrZeroValue(input.TranscodeGPU),
			GPUDir:                  NilOrZeroValue(input.GPUDir),
			TranscodeVideoCodecs:    NilOrZeroValue(input.VideoCodec),
			TranscodeAudioCodecs:    NilOrZeroValue(input.AudioCodec),
			TranscodeQuality:        NilOrZeroValue(input.TranscodeQuality),
			TranscodeVideoFilter:    NilOrZeroValue(input.TranscodeVideoFilter),
		},

		LastScan:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return c, authMap, nil
}

// convertConfigValue handles config entry conversions safely.
func convertConfigValue[T any](v any) (T, bool) {
	var zero T

	// Direct type match
	if val, ok := v.(T); ok {
		return val, true
	}

	switch any(zero).(type) {
	case string:
		if s, ok := v.(string); ok {
			val := any(s).(T)
			return val, true
		}
		str := fmt.Sprintf("%v", v)
		val := any(str).(T)
		return val, true

	case int:
		if i, ok := v.(int); ok {
			val := any(i).(T)
			return val, true
		}
		if i64, ok := v.(int64); ok {
			i := int(i64)
			val := any(i).(T)
			return val, true
		}
		if f, ok := v.(float64); ok {
			i := int(f)
			val := any(i).(T)
			return val, true
		}

	case float64:
		if f, ok := v.(float64); ok {
			val := any(f).(T)
			return val, true
		}
		if i, ok := v.(int); ok {
			f := float64(i)
			val := any(f).(T)
			return val, true
		}

	case bool:
		if b, ok := v.(bool); ok {
			val := any(b).(T)
			return val, true
		}

	case []string:
		if slice, ok := v.([]string); ok {
			val := any(slice).(T)
			return val, true
		}
		if slice, ok := v.([]any); ok {
			strSlice := make([]string, len(slice))
			for i, item := range slice {
				strSlice[i] = fmt.Sprintf("%v", item)
			}
			val := any(strSlice).(T)
			return val, true
		}
	}

	return zero, false
}

// LoadViperIntoStruct loads values from a local Viper instance into a struct of variables.
func LoadViperIntoStruct(v interface {
	IsSet(string) bool
	Get(string) any
}, ptr any) error {
	val := reflect.ValueOf(ptr)
	if val.Kind() != reflect.Pointer || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("expected pointer to struct")
	}

	val = val.Elem()
	typ := val.Type()

	for i := range typ.NumField() {
		field := typ.Field(i)
		tag := field.Tag.Get("viper")
		if tag == "" {
			continue
		}

		ft := field.Type
		switch ft.Kind() {

		case reflect.Pointer:
			elem := ft.Elem()

			switch elem.Kind() {
			case reflect.String:
				v, ok := viperPtr[string](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Int:
				v, ok := viperPtr[int](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Float64:
				v, ok := viperPtr[float64](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Bool:
				v, ok := viperPtr[bool](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Slice:
				sliceType := reflect.SliceOf(elem.Elem())
				if sliceType == reflect.TypeOf([]string{}) {
					v, ok := viperPtr[[]string](v, tag)
					if ok {
						val.Field(i).Set(reflect.ValueOf(v))
					}
				}
			}
		}
	}

	return nil
}

// viperPtr returns a pointer to a value from a local viper instance if successful, or nil.
//
// Supports both kebab-case and snake_case keys.
func viperPtr[T any](v interface {
	IsSet(string) bool
	Get(string) any
}, key string) (*T, bool) {
	val, ok := GetConfigValue[T](v, key)
	if !ok {
		return nil, false
	}
	return &val, true
}

// getConfigValue retrieves values from a local viper instance.
//
// Supports both kebab-case and snake_case keys.
func GetConfigValue[T any](v interface {
	IsSet(string) bool
	Get(string) any
}, key string) (T, bool) {
	var zero T

	// Try original key first
	if v.IsSet(key) {
		if val, ok := convertConfigValue[T](v.Get(key)); ok {
			return val, true
		}
	}

	// Try snake_case version
	snakeKey := strings.ReplaceAll(key, "-", "_")
	if snakeKey != key && v.IsSet(snakeKey) {
		if val, ok := convertConfigValue[T](v.Get(snakeKey)); ok {
			return val, true
		}
	}

	// Try kebab-case version
	kebabKey := strings.ReplaceAll(key, "_", "-")
	if kebabKey != key && v.IsSet(kebabKey) {
		if val, ok := convertConfigValue[T](v.Get(kebabKey)); ok {
			return val, true
		}
	}
	return zero, false
}

// ParseURLSettingsFromViper extracts per-URL settings from the config file.
//
// Creates local Viper instances for each URL's settings to reuse validation logic.
func ParseURLSettingsFromViper(v interface {
	IsSet(string) bool
	Get(string) any
}) (map[string]*models.URLSettingsOverride, error) {
	urlSettings := make(map[string]*models.URLSettingsOverride)

	// Check if url-settings exists
	if !v.IsSet("url-settings") && !v.IsSet("url_settings") {
		logger.Pl.I("No url-settings found in config file")
		return urlSettings, nil
	}

	// Get the raw url-settings data
	var urlSettingsRaw any
	if v.IsSet("url-settings") {
		urlSettingsRaw = v.Get("url-settings")
	} else {
		urlSettingsRaw = v.Get("url_settings")
	}

	// Cast to map
	urlSettingsMap, ok := urlSettingsRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("url-settings must be a map[string]any")
	}

	// Process each URL's settings
	for channelURL, settingsData := range urlSettingsMap {

		// Normalize URL to lowercase for case-insensitive matching
		normalizedURL := strings.ToLower(channelURL)

		override := &models.URLSettingsOverride{}
		dataMap, ok := settingsData.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("settings for URL %q must be a map", channelURL)
		}

		// Handle "settings" section
		var settingsMap map[string]any
		if settingsRaw, hasSettings := dataMap["settings"]; hasSettings {
			if settingsMap, ok = settingsRaw.(map[string]any); !ok {
				return nil, fmt.Errorf("settings section for URL %q must be a map", channelURL)
			}

			// Create new Viper instance and load the settings map
			settingsViper := &urlConfigSettingsParser{data: settingsMap}
			settingsInput := &models.ChannelInputPtrs{}
			if err := LoadViperIntoStruct(settingsViper, settingsInput); err != nil {
				return nil, fmt.Errorf("failed to parse settings for URL %q: %w", channelURL, err)
			}

			override.Settings = settingsInput
		} else {
			logger.Pl.D(1, "No settings section found in %+v", settingsMap)
		}

		// Handle "metarr" section
		var metarrMap map[string]any
		if metarrRaw, hasMetarr := dataMap["metarr"]; hasMetarr {
			if metarrMap, ok = metarrRaw.(map[string]any); !ok {
				return nil, fmt.Errorf("metarr section for URL %q must be a map", channelURL)
			}

			// Create new Viper instance and load the metarr map
			metarrViper := &urlConfigSettingsParser{data: metarrMap}
			metarrInput := &models.ChannelInputPtrs{}
			if err := LoadViperIntoStruct(metarrViper, metarrInput); err != nil {
				return nil, fmt.Errorf("failed to parse metarr for URL %q: %w", channelURL, err)
			}

			override.Metarr = metarrInput
		} else {
			logger.Pl.D(1, "No Metarr section found in %+v", metarrMap)
		}

		urlSettings[normalizedURL] = override
	}

	return urlSettings, nil
}

// urlConfigSettingsParser wraps a map to implement the Viper-like interface.
type urlConfigSettingsParser struct {
	data map[string]any
}

// IsSet checks if the key exists in the data.
func (u *urlConfigSettingsParser) IsSet(key string) bool {
	if _, ok := u.data[key]; ok {
		return true
	}
	// Try with underscore/hyphen variations
	snakeKey := strings.ReplaceAll(key, "-", "_")
	if snakeKey != key {
		if _, ok := u.data[snakeKey]; ok {
			return true
		}
	}
	kebabKey := strings.ReplaceAll(key, "_", "-")
	if kebabKey != key {
		if _, ok := u.data[kebabKey]; ok {
			return true
		}
	}
	return false
}

// Get returns the key from the data.
func (u *urlConfigSettingsParser) Get(key string) any {
	// Try original key
	if val, ok := u.data[key]; ok {
		return val
	}
	// Try snake_case
	snakeKey := strings.ReplaceAll(key, "-", "_")
	if snakeKey != key {
		if val, ok := u.data[snakeKey]; ok {
			return val
		}
	}
	// Try kebab-case
	kebabKey := strings.ReplaceAll(key, "_", "-")
	if kebabKey != key {
		if val, ok := u.data[kebabKey]; ok {
			return val
		}
	}
	return nil
}

// buildSettingsFromInput constructs a Settings object from ChannelInputPtrs (for per-URL overrides).
//
// Only sets fields that were explicitly provided (non-nil pointers with non-empty values).
// Returns an error if any validation fails.
func buildSettingsFromInput(input *models.ChannelInputPtrs) (*models.Settings, error) {
	settings := &models.Settings{}

	// Set CookiesFromBrowser (no validation available)
	if input.CookiesFromBrowser != nil {
		settings.CookiesFromBrowser = *input.CookiesFromBrowser
	}

	// Set ExternalDownloader (no validation available)
	if input.ExternalDownloader != nil {
		settings.ExternalDownloader = *input.ExternalDownloader
	}

	// Set ExternalDownloaderArgs (no validation available)
	if input.ExternalDownloaderArgs != nil {
		settings.ExternalDownloaderArgs = *input.ExternalDownloaderArgs
	}

	// Validate and set FilterFile if provided
	if input.DLFilterFile != nil && *input.DLFilterFile != "" {
		if _, err := validation.ValidateFile(*input.DLFilterFile, false); err != nil {
			return nil, fmt.Errorf("invalid filter file for per-URL settings: %w", err)
		}
		settings.FilterFile = *input.DLFilterFile
	}

	// Validate and set JSONDir if provided
	if input.JSONDir != nil && *input.JSONDir != "" {
		if _, err := validation.ValidateDirectory(*input.JSONDir, true); err != nil {
			return nil, fmt.Errorf("invalid JSON directory for per-URL settings: %w", err)
		}
		settings.JSONDir = *input.JSONDir
	}

	// Validate and set MaxFilesize if provided
	if input.MaxFilesize != nil && *input.MaxFilesize != "" {
		v, err := validation.ValidateMaxFilesize(*input.MaxFilesize)
		if err != nil {
			return nil, fmt.Errorf("invalid max filesize for per-URL settings: %w", err)
		}
		settings.MaxFilesize = v
	}

	// Validate and set MoveOpFile if provided
	if input.MoveOpFile != nil && *input.MoveOpFile != "" {
		if _, err := validation.ValidateFile(*input.MoveOpFile, false); err != nil {
			return nil, fmt.Errorf("invalid move ops file for per-URL settings: %w", err)
		}
		settings.MetaFilterMoveOpFile = *input.MoveOpFile
	}

	// Validate and set VideoDir if provided
	if input.VideoDir != nil && *input.VideoDir != "" {
		if _, err := validation.ValidateDirectory(*input.VideoDir, true); err != nil {
			return nil, fmt.Errorf("invalid video directory for per-URL settings: %w", err)
		}
		settings.VideoDir = *input.VideoDir
	}

	// Set ExtraYTDLPVideoArgs (no validation available)
	if input.ExtraYTDLPVideoArgs != nil {
		settings.ExtraYTDLPVideoArgs = *input.ExtraYTDLPVideoArgs
	}

	// Set ExtraYTDLPMetaArgs (no validation available)
	if input.ExtraYTDLPMetaArgs != nil {
		settings.ExtraYTDLPMetaArgs = *input.ExtraYTDLPMetaArgs
	}

	// Validate and set Concurrency if provided
	if input.Concurrency != nil {
		settings.Concurrency = sharedvalidation.ValidateConcurrencyLimit(*input.Concurrency)
	}

	// Set CrawlFreq (no validation available)
	if input.CrawlFreq != nil {
		settings.CrawlFreq = max(*input.CrawlFreq, 0)
	}

	// Set Retries (no validation available)
	if input.Retries != nil {
		settings.Retries = max(*input.Retries, 0)
	}

	// Set bools
	if input.Pause != nil {
		settings.Paused = *input.Pause
	}
	if input.UseGlobalCookies != nil {
		settings.UseGlobalCookies = *input.UseGlobalCookies
	}

	// Validate and set FromDate if provided
	if input.FromDate != nil && *input.FromDate != "" {
		v, err := validation.ValidateToFromDate(*input.FromDate)
		if err != nil {
			return nil, fmt.Errorf("invalid from date for per-URL settings: %w", err)
		}
		settings.FromDate = v
	}

	// Validate and set ToDate if provided
	if input.ToDate != nil && *input.ToDate != "" {
		v, err := validation.ValidateToFromDate(*input.ToDate)
		if err != nil {
			return nil, fmt.Errorf("invalid to date for per-URL settings: %w", err)
		}
		settings.ToDate = v
	}

	// Validate and set YTDLP output extension if provided
	if input.YTDLPOutputExt != nil && *input.YTDLPOutputExt != "" {
		v := strings.ToLower(*input.YTDLPOutputExt)
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			return nil, fmt.Errorf("invalid ytdlp output extension for per-URL settings: %w", err)
		}
		settings.YtdlpOutputExt = v
	}

	// Parse filter ops if provided
	if input.DLFilters != nil {
		m, err := ParseFilterOps(*input.DLFilters)
		if err != nil {
			return nil, fmt.Errorf("invalid filter ops for per-URL settings: %w", err)
		}
		settings.Filters = m
	}

	// Parse move ops if provided
	if input.MoveOps != nil {
		m, err := ParseMetaFilterMoveOps(*input.MoveOps)
		if err != nil {
			return nil, fmt.Errorf("invalid move ops for per-URL settings: %w", err)
		}
		settings.MetaFilterMoveOps = m
	}

	return settings, nil
}

// buildMetarrArgsFromInput constructs a MetarrArgs object from ChannelInputPtrs (for per-URL overrides).
//
// Only sets fields that were explicitly provided (non-nil pointers).
func buildMetarrArgsFromInput(input *models.ChannelInputPtrs) (*models.MetarrArgs, error) {
	metarr := &models.MetarrArgs{}

	// Only set string fields if explicitly provided
	if input.MetarrExt != nil {
		metarr.OutputExt = *input.MetarrExt
	}
	if input.ExtraFFmpegArgs != nil {
		metarr.ExtraFFmpegArgs = *input.ExtraFFmpegArgs
	}
	if input.MetaOpsFile != nil {
		if _, err := validation.ValidateFile(*input.MetaOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid meta ops file for per-URL settings: %w", err)
		}
		metarr.MetaOpsFile = *input.MetaOpsFile
	}
	if input.FilteredMetaOpsFile != nil {
		if _, err := validation.ValidateFile(*input.FilteredMetaOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid filtered meta ops file for per-URL settings: %w", err)
		}
		metarr.FilteredMetaOpsFile = *input.FilteredMetaOpsFile
	}
	if input.FilenameOpsFile != nil {
		if _, err := validation.ValidateFile(*input.FilenameOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid filename ops file for per-URL settings: %w", err)
		}
		metarr.FilenameOpsFile = *input.FilenameOpsFile
	}
	if input.FilteredFilenameOpsFile != nil {
		if _, err := validation.ValidateFile(*input.FilteredFilenameOpsFile, false); err != nil {
			return nil, fmt.Errorf("invalid filtered filename ops file for per-URL settings: %w", err)
		}
		metarr.FilteredFilenameOpsFile = *input.FilteredFilenameOpsFile
	}
	if input.OutDir != nil {
		if _, err := validation.ValidateDirectory(*input.OutDir, true); err != nil {
			return nil, fmt.Errorf("invalid Metarr output directory for per-URL settings: %w", err)
		}
		metarr.OutputDir = *input.OutDir
	}
	if input.TranscodeVideoFilter != nil {
		metarr.TranscodeVideoFilter = *input.TranscodeVideoFilter
	}

	// Only set int fields if explicitly provided
	if input.MetarrConcurrency != nil {
		metarr.Concurrency = max(*input.MetarrConcurrency, 0)
	}

	// Only set float64 fields if explicitly provided
	if input.MaxCPU != nil {
		metarr.MaxCPU = sharedvalidation.ValidateMaxCPU(*input.MaxCPU)
	}

	// Validate and set rename style if provided
	if input.RenameStyle != nil {
		if err := validation.ValidateRenameFlag(*input.RenameStyle); err != nil {
			return nil, fmt.Errorf("failed to validate rename style for per-URL settings: %w", err)
		}
		metarr.RenameStyle = *input.RenameStyle
	}

	// Validate and set min free mem if provided
	if input.MinFreeMem != nil {
		if _, err := sharedvalidation.ValidateMinFreeMem(*input.MinFreeMem); err != nil {
			return nil, fmt.Errorf("failed to validate min free mem for per-URL settings: %w", err)
		}
		metarr.MinFreeMem = *input.MinFreeMem
	}

	// Validate and set GPU settings if provided
	if input.TranscodeGPU != nil {
		g, d, err := validation.ValidateGPU(*input.TranscodeGPU, NilOrZeroValue(input.GPUDir))
		if err != nil {
			return nil, fmt.Errorf("failed to validate GPU settings for per-URL settings: %w", err)
		}
		metarr.UseGPU = g
		metarr.GPUDir = d
	}

	// Validate and set video codecs if provided
	if input.VideoCodec != nil {
		c, err := validation.ValidateVideoTranscodeCodecSlice(*input.VideoCodec, metarr.UseGPU)
		if err != nil {
			return nil, fmt.Errorf("failed to validate video codecs for per-URL settings: %w", err)
		}
		metarr.TranscodeVideoCodecs = c
	}

	// Validate and set audio codecs if provided
	if input.AudioCodec != nil {
		logger.Pl.I("Found audio codecs in per-URL override: %v", *input.AudioCodec)
		c, err := validation.ValidateAudioTranscodeCodecSlice(*input.AudioCodec)
		if err != nil {
			return nil, fmt.Errorf("failed to validate audio codecs for per-URL settings: %w", err)
		}
		metarr.TranscodeAudioCodecs = c
	}

	// Validate and set transcode quality if provided
	if input.TranscodeQuality != nil {
		q, err := sharedvalidation.ValidateTranscodeQuality(*input.TranscodeQuality)
		if err != nil {
			return nil, fmt.Errorf("failed to validate transcode quality for per-URL settings: %w", err)
		}
		metarr.TranscodeQuality = q
	}

	// Parse meta ops if provided
	if input.MetaOps != nil {
		m, err := ParseMetaOps(*input.MetaOps)
		if err != nil {
			return nil, fmt.Errorf("failed to parse meta ops for per-URL settings: %w", err)
		}
		metarr.MetaOps = m
	}

	// Parse filename ops if provided
	if input.FilenameOps != nil {
		m, err := ParseFilenameOps(*input.FilenameOps)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filename ops for per-URL settings: %w", err)
		}
		metarr.FilenameOps = m
	}

	// Parse filtered meta ops if provided
	if input.FilteredMetaOps != nil {
		m, err := ParseFilteredMetaOps(*input.FilteredMetaOps)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filtered meta ops for per-URL settings: %w", err)
		}
		metarr.FilteredMetaOps = m
	}

	// Parse filtered filename ops if provided
	if input.FilteredFilenameOps != nil {
		m, err := ParseFilteredFilenameOps(*input.FilteredFilenameOps)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filtered filename ops for per-URL settings: %w", err)
		}
		metarr.FilteredFilenameOps = m
	}

	return metarr, nil
}
