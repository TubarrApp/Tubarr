package file

import (
	"tubarr/internal/validation"

	"github.com/spf13/viper"
)

// LoadConfigFile loads in the preset configuration file.
func LoadConfigFile(file string) error {
	if _, err := validation.ValidateFile(file, false); err != nil {
		return err
	}

	viper.SetConfigFile(file)
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	return nil
}
