package cfgflags

import (
	"tubarr/internal/utils/logging"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// SetChangedFlag sets a flag only if it was explicitly entered (on first 'add', skip this to have defaults set).
func SetChangedFlag(key string, val any, f *pflag.FlagSet) {
	if !f.Changed(key) {
		return
	}

	if f == nil {
		logging.E(0, "Dev error: Flag sent in nil for key val %q:%q", key, val)
		return
	}

	viper.Set(key, val)
}
