package cfg

import (
	"github.com/spf13/viper"
)

// Set sets the value for the key in the override register. Set is case-insensitive for a key. Will be used instead of values obtained via flags, config file, ENV, default, or key/value store.
func Set(key string, value any) {

	viper.Set(key, value)
}

// Get can retrieve any value given the key to use. Get is case-insensitive for a key. Get has the behavior of returning the value associated with the first place from where it is set. Viper will check in the following order: override, flag, env, config file, key/value store, default
// Get returns an interface. For a specific value use one of the Get____ methods.
func Get(key string) any {
	return viper.Get(key)
}

// GetBool returns the value associated with the key as a boolean.
func GetBool(key string) bool {
	return viper.GetBool(key)
}

// GetInt returns the value associated with the key as an integer.
func GetInt(key string) int {
	return viper.GetInt(key)
}

// GetUint64 returns the value associated with the key as an unsigned integer.
func GetUint64(key string) uint64 {
	return viper.GetUint64(key)
}

// GetFloat64 returns the value associated with the key as a float64.
func GetFloat64(key string) float64 {
	return viper.GetFloat64(key)
}

// GetString returns the value associated with the key as a string.
func GetString(key string) string {
	return viper.GetString(key)
}

// GetStringSlice returns the value associated with the key as a slice of strings.
func GetStringSlice(key string) []string {
	return viper.GetStringSlice(key)
}

// IsSet checks to see if the key has been set in any of the data locations.
// IsSet is case-insensitive for a key.
func IsSet(key string) bool {
	return viper.IsSet(key)
}
