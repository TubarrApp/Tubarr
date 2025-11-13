package cfg

import (
	"fmt"
	"strconv"
	"strings"
	"tubarr/internal/file"
	"tubarr/internal/parsing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// loadDefaultsFromConfig loads in variables from config file(s).
func loadDefaultsFromConfig(cmd *cobra.Command, primaryFile, secondaryFile string) error {
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
				if val, ok := parsing.GetConfigValue[string](key); ok {
					if err := f.Value.Set(val); err != nil {
						errOrNil = err
					}
				}
			case "int":
				if val, ok := parsing.GetConfigValue[int](key); ok {
					if err := f.Value.Set(strconv.Itoa(val)); err != nil {
						errOrNil = err
					}
				}
			case "bool":
				if val, ok := parsing.GetConfigValue[bool](key); ok {
					if err := f.Value.Set(strconv.FormatBool(val)); err != nil {
						errOrNil = err
					}
				}
			case "float64":
				if val, ok := parsing.GetConfigValue[float64](key); ok {
					if err := f.Value.Set(fmt.Sprintf("%f", val)); err != nil {
						errOrNil = err
					}
				}
			case "stringSlice":
				if slice, ok := parsing.GetConfigValue[[]string](key); ok && len(slice) > 0 {
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
