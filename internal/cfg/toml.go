package cfg

import (
	"fmt"
	"os"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"

	"github.com/BurntSushi/toml"
	"github.com/spf13/viper"
)

func parseTomlFile() (*models.Config, error) {
	if !viper.IsSet(keys.TomlPath) {
		logging.D(3, "No config file sent in")
		return nil, nil
	}

	path := viper.GetString(keys.TomlPath)
	checkPath, err := os.Stat(path)

	switch {
	case err != nil:
		return nil, err
	case checkPath.IsDir():
		return nil, fmt.Errorf("toml file passed in as directory '%s', should be file", path)
	case !checkPath.Mode().IsRegular():
		return nil, fmt.Errorf("'%s' is not a regular file", path)
	case checkPath.Size() == 0:
		return nil, fmt.Errorf("file '%s' is empty", path)
	}

	var config models.Config

	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
