package process

import (
	"tubarr/internal/command"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// InitMetarr begins processing with Metarr
func InitMetarr(v *models.Video) error {
	mc := command.NewMetarrCommandBuilder(v)
	cmd, err := mc.MakeMetarrCommands()
	if err != nil {
		return err
	}

	if err := command.RunMetarr(cmd); err != nil {
		return err
	}
	logging.S(1, "Finished Metarr command for '%s'", v.VPath)
	return nil
}
