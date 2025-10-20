package validation

import (
	"errors"
	"fmt"
	"tubarr/internal/domain/consts"
)

// ValidateColumnKey ensures that the provided column 'key' is valid.
func ValidateColumnKey(key string) error {
	if key == "" {
		return errors.New("key must not be empty")
	}
	if !consts.ValidDBColumns[key] {
		return fmt.Errorf("invalid database column key: %q", key)
	}
	return nil
}

// ValidateColumnKeyVal ensures that the provided column 'key' with value 'val' is valid.
func ValidateColumnKeyVal(key, val string) error {
	if key == "" || val == "" {
		return errors.New("key and value must not be empty")
	}
	if !consts.ValidDBColumns[key] {
		return fmt.Errorf("invalid database column key: %q", key)
	}
	return nil
}
