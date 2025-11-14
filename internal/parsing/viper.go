package parsing

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"tubarr/internal/abstractions"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// LoadDefaultsFromConfig loads in variables from config file(s).
func LoadDefaultsFromConfig(cmd *cobra.Command, primaryFile, secondaryFile string) error {
	fileToUse := ""
	if primaryFile != "" {
		fileToUse = primaryFile
	}
	if secondaryFile != "" {
		fileToUse = secondaryFile
	}
	if fileToUse == "" {
		return nil
	}

	if err := file.LoadConfigFile(fileToUse); err != nil {
		return err
	}

	// Apply defaults for all known flags
	var errOrNil error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed {
			key := f.Name
			switch f.Value.Type() {
			case "string":
				if val, ok := GetConfigValue[string](key); ok {
					if err := f.Value.Set(val); err != nil {
						errOrNil = err
					}
				}
			case "int":
				if val, ok := GetConfigValue[int](key); ok {
					if err := f.Value.Set(strconv.Itoa(val)); err != nil {
						errOrNil = err
					}
				}
			case "bool":
				if val, ok := GetConfigValue[bool](key); ok {
					if err := f.Value.Set(strconv.FormatBool(val)); err != nil {
						errOrNil = err
					}
				}
			case "float64":
				if val, ok := GetConfigValue[float64](key); ok {
					if err := f.Value.Set(fmt.Sprintf("%f", val)); err != nil {
						errOrNil = err
					}
				}
			case "stringSlice":
				if slice, ok := GetConfigValue[[]string](key); ok && len(slice) > 0 {
					// Try to type assert pflag.Value to pflag.SliceValue
					if sv, ok := f.Value.(pflag.SliceValue); ok {
						if err := sv.Replace(slice); err != nil {
							errOrNil = err
						}
					} else {
						// Fallback: Try join on comma for types that only implement Set(string)
						if err := f.Value.Set(strings.Join(slice, ",")); err != nil {
							errOrNil = err
						}
					}
				}
			}
		}
	})
	return errOrNil
}

// GetConfigValue normalizes and retrieves values from the config file.
//
// Supports both kebab-case and snake_case keys.
func GetConfigValue[T any](key string) (T, bool) {
	var zero T

	// Try original key first
	if abstractions.IsSet(key) {
		if val, ok := convertConfigValue[T](abstractions.Get(key)); ok {
			return val, true
		}
	}

	// Try snake_case version
	snakeKey := strings.ReplaceAll(key, "-", "_")
	if snakeKey != key && abstractions.IsSet(snakeKey) {
		if val, ok := convertConfigValue[T](abstractions.Get(snakeKey)); ok {
			return val, true
		}
	}

	// Try kebab-case version
	kebabKey := strings.ReplaceAll(key, "_", "-")
	if kebabKey != key && abstractions.IsSet(kebabKey) {
		if val, ok := convertConfigValue[T](abstractions.Get(kebabKey)); ok {
			return val, true
		}
	}
	return zero, false
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

// LoadViperIntoStruct loads values from Viper into a struct of variables.
func LoadViperIntoStruct(ptr any) error {
	val := reflect.ValueOf(ptr)
	if val.Kind() != reflect.Pointer || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("expected pointer to struct")
	}

	val = val.Elem()
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
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
				v, ok := viperPtr[string](tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Int:
				v, ok := viperPtr[int](tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Float64:
				v, ok := viperPtr[float64](tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Bool:
				v, ok := viperPtr[bool](tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Slice:
				sliceType := reflect.SliceOf(elem.Elem())
				if sliceType == reflect.TypeOf([]string{}) {
					v, ok := viperPtr[[]string](tag)
					if ok {
						val.Field(i).Set(reflect.ValueOf(v))
					}
				}
			}
		}
	}

	return nil
}

// viperPtr returns these conditions returns a pointer to a value if successful, or nil.
//
// Supports both kebab-case and snake_case keys via GetConfigValue normalization.
func viperPtr[T any](key string) (*T, bool) {
	val, ok := GetConfigValue[T](key)
	if !ok {
		return nil, false
	}
	return &val, true
}

// LoadViperIntoStructLocal loads values from a local Viper instance into a struct of variables.
func LoadViperIntoStructLocal(v interface {
	IsSet(string) bool
	Get(string) any
}, ptr any) error {
	val := reflect.ValueOf(ptr)
	if val.Kind() != reflect.Pointer || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("expected pointer to struct")
	}

	val = val.Elem()
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
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
				v, ok := viperPtrLocal[string](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Int:
				v, ok := viperPtrLocal[int](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Float64:
				v, ok := viperPtrLocal[float64](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Bool:
				v, ok := viperPtrLocal[bool](v, tag)
				if ok {
					val.Field(i).Set(reflect.ValueOf(v))
				}
			case reflect.Slice:
				sliceType := reflect.SliceOf(elem.Elem())
				if sliceType == reflect.TypeOf([]string{}) {
					v, ok := viperPtrLocal[[]string](v, tag)
					if ok {
						val.Field(i).Set(reflect.ValueOf(v))
					}
				}
			}
		}
	}

	return nil
}

// viperPtrLocal returns a pointer to a value from a local viper instance if successful, or nil.
//
// Supports both kebab-case and snake_case keys.
func viperPtrLocal[T any](v interface {
	IsSet(string) bool
	Get(string) any
}, key string) (*T, bool) {
	val, ok := getConfigValueLocal[T](v, key)
	if !ok {
		return nil, false
	}
	return &val, true
}

// getConfigValueLocal retrieves values from a local viper instance.
//
// Supports both kebab-case and snake_case keys.
func getConfigValueLocal[T any](v interface {
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
		logging.I("No url-settings found in config")
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
		if settingsRaw, hasSettings := dataMap["settings"]; hasSettings {
			settingsMap, ok := settingsRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("settings section for URL %q must be a map", channelURL)
			}

			// Create new Viper instance and load the settings map
			settingsViper := &urlConfigSettingsParser{data: settingsMap}
			settingsInput := &models.ChannelInputPtrs{}
			if err := LoadViperIntoStructLocal(settingsViper, settingsInput); err != nil {
				return nil, fmt.Errorf("failed to parse settings for URL %q: %w", channelURL, err)
			}

			override.Settings = settingsInput
		}

		// Handle "metarr" section
		if metarrRaw, hasMetarr := dataMap["metarr"]; hasMetarr {
			metarrMap, ok := metarrRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("metarr section for URL %q must be a map", channelURL)
			}

			// Create new Viper instance and load the metarr map
			metarrViper := &urlConfigSettingsParser{data: metarrMap}
			metarrInput := &models.ChannelInputPtrs{}
			if err := LoadViperIntoStructLocal(metarrViper, metarrInput); err != nil {
				return nil, fmt.Errorf("failed to parse metarr for URL %q: %w", channelURL, err)
			}

			override.Metarr = metarrInput
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
