package parsing

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"tubarr/internal/auth"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
	"github.com/spf13/viper"
)

// configGetter reads config values, from Viper or a per-URL settings map.
type configGetter interface {
	IsSet(string) bool
	Get(string) any
}

// LoadChannelInputFile reads a channel config file into channel input, with its per-URL settings.
func LoadChannelInputFile(path string) (models.ChannelInput, error) {
	var input models.ChannelInput
	v := viper.New()
	if err := file.LoadConfigFile(v, path); err != nil {
		return input, err
	}
	if err := LoadViperIntoStruct(v, &input); err != nil {
		return input, fmt.Errorf("failed to apply config: %w", err)
	}
	urlSettings, err := ParseURLSettingsFromViper(v)
	if err != nil {
		return input, fmt.Errorf("failed to parse url-settings: %w", err)
	}
	input.URLSettings = urlSettings
	return input, nil
}

// BuildChannelFromInput builds a new channel and its logins from input, validating them.
func BuildChannelFromInput(input models.ChannelInput) (
	*models.Channel,
	map[string]*models.ChannelAccessDetails,
	error,
) {
	if input.Name == nil || *input.Name == "" {
		return nil, nil, errors.New("channels require a name")
	}

	// Channel config file.
	if input.ChannelConfigFile != nil {
		if _, _, err := sharedvalidation.ValidateFile(*input.ChannelConfigFile, false, sharedtemplates.NoTemplateTags); err != nil {
			return nil, nil, err
		}
	}

	now := time.Now()
	c := &models.Channel{
		Name:              *input.Name,
		ChannelConfigFile: NilOrZeroValue(input.ChannelConfigFile),
		ChanSettings:      &models.Settings{},
		ChanMetarrArgs:    &models.MetarrArgs{},
		LastScan:          now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := ApplyChannelSettingsInput(c.ChanSettings, &input.SettingsInput); err != nil {
		return nil, nil, err
	}
	if err := ApplyMetarrArgsInput(c.ChanMetarrArgs, &input.MetarrArgsInput); err != nil {
		return nil, nil, err
	}

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

	// URLs, with their logins and per-URL settings.
	for _, u := range NilOrZeroValue(input.URLs) {
		if u == "" {
			continue
		}
		cu := &models.ChannelURL{
			URL:       u,
			LastScan:  now,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if a, exists := authMap[u]; exists {
			cu.Username, cu.Password, cu.LoginURL = a.Username, a.Password, a.LoginURL
		}
		if override, ok := input.URLSettings[strings.ToLower(u)]; ok {
			if override.Settings != nil {
				cu.ChanURLSettings = &models.Settings{}
				if err := ApplySettingsInput(cu.ChanURLSettings, override.Settings); err != nil {
					return nil, nil, fmt.Errorf("per-URL settings error for %s: %w", u, err)
				}
			}
			if override.Metarr != nil {
				cu.ChanURLMetarrArgs = &models.MetarrArgs{}
				if err := ApplyMetarrArgsInput(cu.ChanURLMetarrArgs, override.Metarr); err != nil {
					return nil, nil, fmt.Errorf("per-URL metarr error for %s: %w", u, err)
				}
			}
		}
		c.URLModels = append(c.URLModels, cu)
	}
	return c, authMap, nil
}

// ApplyChannelSettingsInput is ApplySettingsInput for a channel's own settings, which need a video directory, and save
// metadata there when there's no JSON directory.
func ApplyChannelSettingsInput(dst *models.Settings, in *models.SettingsInput) error {
	if err := ApplySettingsInput(dst, in); err != nil {
		return err
	}
	if dst.VideoDir == "" {
		return errors.New("channels require a video directory")
	}
	dst.JSONDir = cmp.Or(dst.JSONDir, dst.VideoDir)
	return nil
}

// ApplySettingsInput validates each setting in gives and sets it in dst, an empty value clearing it.
//
// Files and folders needn't exist: saving checks those it adds or changes.
func ApplySettingsInput(dst *models.Settings, in *models.SettingsInput) (err error) {
	if v := in.DownloadConcurrency; v != nil {
		dst.DownloadConcurrency = max(*v, 0)
	}
	if v := in.CookiesFromBrowser; v != nil {
		dst.CookiesFromBrowser = *v
	}
	if v := in.CrawlFreq; v != nil {
		dst.CrawlFreq = max(*v, 0)
	}
	if v := in.ExternalDownloader; v != nil {
		dst.ExternalDownloader = *v
	}
	if v := in.ExternalDownloaderArgs; v != nil {
		dst.ExternalDownloaderArgs = *v
	}
	if v := in.MaxFilesize; v != nil {
		if dst.MaxFilesize, err = validation.ValidateMaxFilesize(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MaxFilesize, err)
		}
	}
	if v := in.Retries; v != nil {
		dst.Retries = max(*v, 0)
	}
	if v := in.UseGlobalCookies; v != nil {
		b := *v
		dst.UseGlobalCookies = &b
	}
	if v := in.YTDLPOutputExt; v != nil {
		if err := validation.ValidateYtdlpOutputExtension(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.YtdlpOutputExt, err)
		}
		dst.YtdlpOutputExt = *v
	}
	if v := in.YTDLPPreferredVideoCodecs; v != nil {
		if dst.YTDLPPreferredVideoCodecs, err = validation.ValidatePreferredVideoCodecs(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.YTDLPPreferredVideoCodecs, err)
		}
	}
	if v := in.YTDLPPreferredAudioCodecs; v != nil {
		if dst.YTDLPPreferredAudioCodecs, err = validation.ValidatePreferredAudioCodecs(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.YTDLPPreferredAudioCodecs, err)
		}
	}
	if v := in.ExtraYTDLPVideoArgs; v != nil {
		dst.ExtraYTDLPVideoArgs = *v
	}
	if v := in.ExtraYTDLPMetaArgs; v != nil {
		dst.ExtraYTDLPMetaArgs = *v
	}
	if v := in.ExtraYTDLPCrawlArgs; v != nil {
		dst.ExtraYTDLPCrawlArgs = *v
	}
	if v := in.DLFilters; v != nil {
		if dst.Filters, err = ParseFilterOps(nonBlank(*v), true); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.FilterOpsInput, err)
		}
	}
	if v := in.DLFilterFile; v != nil {
		if err := validation.CheckFile(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.FilterOpsFile, err)
		}
		dst.FilterFile = *v
	}
	if v := in.MoveOps; v != nil {
		if dst.MetaFilterMoveOps, err = ParseMetaFilterMoveOps(nonBlank(*v)); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MoveOps, err)
		}
	}
	if v := in.MoveOpFile; v != nil {
		if err := validation.CheckFile(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MoveOpsFile, err)
		}
		dst.MetaFilterMoveOpFile = *v
	}
	if v := in.FromDate; v != nil {
		if dst.FromDate, err = validation.ValidateToFromDate(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.FromDate, err)
		}
	}
	if v := in.ToDate; v != nil {
		if dst.ToDate, err = validation.ValidateToFromDate(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.ToDate, err)
		}
	}
	if v := in.JSONDir; v != nil {
		if err := validation.CheckDirectory(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.JSONDir, err)
		}
		dst.JSONDir = *v
	}
	if v := in.VideoDir; v != nil {
		if err := validation.CheckDirectory(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.VideoDir, err)
		}
		dst.VideoDir = *v
	}
	if v := in.Pause; v != nil {
		dst.Paused = *v
	}
	if v := in.AutoDownload; v != nil {
		b := *v
		dst.AutoDownload = &b
	}
	return nil
}

// ApplyMetarrArgsInput validates each Metarr argument in gives and sets it in dst, an empty value clearing it.
//
// The GPU node and video codecs are checked against the GPU dst ends up with. Files and folders needn't exist: saving
// checks those it adds or changes.
func ApplyMetarrArgsInput(dst *models.MetarrArgs, in *models.MetarrArgsInput) (err error) {
	if v := in.MetarrExt; v != nil {
		if *v != "" {
			if _, err := sharedvalidation.ValidateFFmpegOutputExt(*v); err != nil {
				return fmt.Errorf("invalid %s: %w", keys.MOutputExt, err)
			}
		}
		dst.OutputExt = *v
	}
	if v := in.FilenameOps; v != nil {
		if dst.FilenameOps, err = ParseFilenameOps(nonBlank(*v)); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MFilenameOps, err)
		}
	}
	if v := in.FilenameOpsFile; v != nil {
		if err := validation.CheckFile(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MFilenameOpsFile, err)
		}
		dst.FilenameOpsFile = *v
	}
	if v := in.FilteredFilenameOps; v != nil {
		if dst.FilteredFilenameOps, err = ParseFilteredFilenameOps(nonBlank(*v)); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MFilteredFilenameOps, err)
		}
	}
	if v := in.FilteredFilenameOpsFile; v != nil {
		if err := validation.CheckFile(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MFilteredFilenameOpsFile, err)
		}
		dst.FilteredFilenameOpsFile = *v
	}
	if v := in.MetaOps; v != nil {
		if dst.MetaOps, err = ParseMetaOps(nonBlank(*v)); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MMetaOps, err)
		}
	}
	if v := in.MetaOpsFile; v != nil {
		if err := validation.CheckFile(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MMetaOpsFile, err)
		}
		dst.MetaOpsFile = *v
	}
	if v := in.FilteredMetaOps; v != nil {
		if dst.FilteredMetaOps, err = ParseFilteredMetaOps(nonBlank(*v)); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MFilteredMetaOps, err)
		}
	}
	if v := in.FilteredMetaOpsFile; v != nil {
		if err := validation.CheckFile(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MFilteredMetaOpsFile, err)
		}
		dst.FilteredMetaOpsFile = *v
	}
	if v := in.RenameStyle; v != nil {
		if err := validation.ValidateRenameFlag(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MRenameStyle, err)
		}
		dst.RenameStyle = *v
	}
	if v := in.ExtraFFmpegArgs; v != nil {
		dst.ExtraFFmpegArgs = *v
	}
	if v := in.OutDir; v != nil {
		if err := validation.CheckDirectory(*v, sharedtemplates.AllTemplatesMap); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MOutputDir, err)
		}
		dst.OutputDir = *v
	}
	if v := in.MetaPurge; v != nil {
		if dst.MetaPurge, err = validation.ValidatePurgeMetafiles(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MMetaPurge, err)
		}
	}
	if v := in.MetarrConcurrency; v != nil {
		dst.MetarrConcurrency = max(*v, 0)
	}
	if v := in.MaxCPU; v != nil {
		dst.MaxCPU = sharedvalidation.ValidateMaxCPU(*v, true)
	}
	if v := in.MinFreeMem; v != nil {
		if _, err := sharedvalidation.ValidateMinFreeMem(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.MMinFreeMem, err)
		}
		dst.MinFreeMem = *v
	}
	if v := in.TranscodeGPU; v != nil {
		if dst.TranscodeGPU, err = validation.ValidateGPUAcceleration(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.TranscodeGPU, err)
		}
	}
	if v := in.TranscodeGPUNode; v != nil {
		if dst.TranscodeGPUNode, err = sharedvalidation.ValidateAccelTypeDeviceNode(dst.TranscodeGPU, *v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.TranscodeGPUNode, err)
		}
	}
	if v := in.TranscodeVideoCodec; v != nil {
		gpu := dst.TranscodeGPU
		if gpu == validation.GPUNone {
			gpu = ""
		}
		if dst.TranscodeVideoCodecs, err = validation.ValidateVideoTranscodeCodecSlice(*v, gpu); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.TranscodeCodec, err)
		}
	}
	if v := in.TranscodeAudioCodec; v != nil {
		if dst.TranscodeAudioCodecs, err = validation.ValidateAudioTranscodeCodecSlice(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.TranscodeAudioCodec, err)
		}
	}
	if v := in.TranscodeQuality; v != nil {
		if dst.TranscodeQuality, err = sharedvalidation.ValidateTranscodeQuality(*v); err != nil {
			return fmt.Errorf("invalid %s: %w", keys.TranscodeQuality, err)
		}
	}
	return nil
}

// nonBlank returns ops without blank entries, so a list given as blank clears its ops.
func nonBlank(ops []string) []string {
	var kept []string
	for _, op := range ops {
		if strings.TrimSpace(op) != "" {
			kept = append(kept, op)
		}
	}
	return kept
}

// convertConfigValue handles config entry conversions safely.
func convertConfigValue[T any](v any) (T, bool) {
	var zero T

	// Direct type match.
	if val, ok := v.(T); ok {
		return val, true
	}

	switch any(zero).(type) {
	case string:
		if !isScalar(v) {
			return zero, false
		}
		return any(fmt.Sprint(v)).(T), true

	case int:
		switch n := v.(type) {
		case int64:
			return any(int(n)).(T), true
		case float64:
			return any(int(n)).(T), true
		}

	case float64:
		if i, ok := v.(int); ok {
			return any(float64(i)).(T), true
		}

	case []string:
		if slice, ok := v.([]any); ok {
			strSlice := make([]string, len(slice))
			for i, item := range slice {
				if !isScalar(item) {
					return zero, false
				}
				strSlice[i] = fmt.Sprint(item)
			}
			return any(strSlice).(T), true
		}
	}

	return zero, false
}

// isScalar reports whether a config value is a single value (e.g. a number or bool) which can stand in for a string,
// rather than a map or list, which would otherwise be turned into text like "map[a:1]".
func isScalar(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		return false
	}
	return true
}

// LoadViperIntoStruct loads values from a local Viper instance into a struct of variables.
func LoadViperIntoStruct(v configGetter, ptr any) error {
	val := reflect.ValueOf(ptr)
	if val.Kind() != reflect.Pointer || val.Elem().Kind() != reflect.Struct {
		return errors.New("expected pointer to struct")
	}

	val = val.Elem()
	typ := val.Type()

	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if err := LoadViperIntoStruct(v, val.Field(i).Addr().Interface()); err != nil {
				return err
			}
			continue
		}
		tag := field.Tag.Get("viper")
		if tag == "" || field.Type.Kind() != reflect.Pointer {
			continue
		}

		var (
			p  any
			ok bool
		)
		switch elem := field.Type.Elem(); elem.Kind() {
		case reflect.String:
			p, ok = viperPtr[string](v, tag)
		case reflect.Int:
			p, ok = viperPtr[int](v, tag)
		case reflect.Float64:
			p, ok = viperPtr[float64](v, tag)
		case reflect.Bool:
			p, ok = viperPtr[bool](v, tag)
		case reflect.Slice:
			if elem == reflect.TypeFor[[]string]() {
				p, ok = viperPtr[[]string](v, tag)
			}
		}
		if ok {
			val.Field(i).Set(reflect.ValueOf(p))
		}
	}

	return nil
}

// GivenKeys returns the viper keys of the fields given (non-nil) in the input struct in points to.
func GivenKeys(in any) []string {
	var given []string
	val := reflect.ValueOf(in).Elem()
	for i := range val.NumField() {
		field := val.Type().Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			given = append(given, GivenKeys(val.Field(i).Addr().Interface())...)
			continue
		}
		if tag := field.Tag.Get("viper"); tag != "" && field.Type.Kind() == reflect.Pointer && !val.Field(i).IsNil() {
			given = append(given, tag)
		}
	}
	return given
}

// viperPtr returns a pointer to the value for a viper tag from a local viper instance if successful, or nil.
//
// Supports both kebab-case and snake_case keys.
func viperPtr[T any](v configGetter, tag string) (*T, bool) {
	val, ok := GetConfigValue[T](v, tag)
	if !ok {
		return nil, false
	}
	return &val, true
}

// GetConfigValue retrieves values from a local viper instance.
//
// Supports both kebab-case and snake_case keys.
func GetConfigValue[T any](v configGetter, key string) (T, bool) {
	var zero T

	variants := keyVariants(key)
	for _, k := range variants {
		if v.IsSet(k) {
			if val, ok := convertConfigValue[T](v.Get(k)); ok {
				return val, true
			}
		}
	}

	// Set, but not as a usable value.
	for _, k := range variants {
		if v.IsSet(k) {
			logger.Pl.W("Ignoring config key %q: expected a %T value, got %T", k, zero, v.Get(k))
			break
		}
	}
	return zero, false
}

// keyVariants returns key as given, in snake_case, and in kebab-case.
func keyVariants(key string) []string {
	return []string{key, strings.ReplaceAll(key, "-", "_"), strings.ReplaceAll(key, "_", "-")}
}

// ParseURLSettingsFromViper extracts per-URL settings from the config file.
//
// Creates local Viper instances for each URL's settings to reuse validation logic.
func ParseURLSettingsFromViper(v configGetter) (map[string]*models.URLSettingsOverride, error) {
	urlSettings := make(map[string]*models.URLSettingsOverride)

	// Check if url-settings exists.
	if !v.IsSet("url-settings") && !v.IsSet("url_settings") {
		logger.Pl.D(1, "No url-settings found in config file")
		return urlSettings, nil
	}

	// Get the raw url-settings data.
	var urlSettingsRaw any
	if v.IsSet("url-settings") {
		urlSettingsRaw = v.Get("url-settings")
	} else {
		urlSettingsRaw = v.Get("url_settings")
	}

	// Cast to map.
	urlSettingsMap, ok := urlSettingsRaw.(map[string]any)
	if !ok {
		return nil, errors.New("url-settings must be a map[string]any")
	}

	// Process each URL's settings.
	for channelURL, settingsData := range urlSettingsMap {
		dataMap, ok := settingsData.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("settings for URL %q must be a map", channelURL)
		}
		override := &models.URLSettingsOverride{}

		// Handle "settings" section.
		if settingsRaw, hasSettings := dataMap["settings"]; hasSettings {
			settingsMap, ok := settingsRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("settings section for URL %q must be a map", channelURL)
			}
			override.Settings = &models.SettingsInput{}
			if err := LoadViperIntoStruct(&urlConfigSettingsParser{data: settingsMap}, override.Settings); err != nil {
				return nil, fmt.Errorf("failed to parse settings for URL %q: %w", channelURL, err)
			}
		}

		// Handle "metarr" section.
		if metarrRaw, hasMetarr := dataMap["metarr"]; hasMetarr {
			metarrMap, ok := metarrRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("metarr section for URL %q must be a map", channelURL)
			}
			override.Metarr = &models.MetarrArgsInput{}
			if err := LoadViperIntoStruct(&urlConfigSettingsParser{data: metarrMap}, override.Metarr); err != nil {
				return nil, fmt.Errorf("failed to parse metarr for URL %q: %w", channelURL, err)
			}
		}

		// Lowercase for case-insensitive matching.
		urlSettings[strings.ToLower(channelURL)] = override
	}

	return urlSettings, nil
}

// urlConfigSettingsParser wraps a map to implement the Viper-like interface.
type urlConfigSettingsParser struct {
	data map[string]any
}

// IsSet checks if the key exists in the data.
func (u *urlConfigSettingsParser) IsSet(key string) bool {
	_, ok := u.lookup(key)
	return ok
}

// Get returns the key from the data.
func (u *urlConfigSettingsParser) Get(key string) any {
	val, _ := u.lookup(key)
	return val
}

// lookup returns the value for key as given, in snake_case, or in kebab-case.
func (u *urlConfigSettingsParser) lookup(key string) (any, bool) {
	for _, k := range keyVariants(key) {
		if val, ok := u.data[k]; ok {
			return val, true
		}
	}
	return nil, false
}
