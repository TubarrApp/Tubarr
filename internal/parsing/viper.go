package parsing

import (
	"fmt"
	"strings"
	"tubarr/internal/abstractions"
)

// GetConfigValue normalizes and retrieves values from the config file.
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
