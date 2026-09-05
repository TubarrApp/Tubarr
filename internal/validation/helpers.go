package validation

import (
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// CheckForOpURL checks if a specific URL is attached to a particular meta operation.
func CheckForOpURL(op string) (chanURL string, ops string) {
	chanURL, ops, err := sharedparsing.SplitOpURL(op)
	if err != nil {
		logger.Pl.W("%v. Ignore this if the operation does not contain a channel URL (format is 'channel URL|filter:ops:go:here')", err)
	}
	return chanURL, ops
}

// DeduplicateSliceEntries removes duplicate entries in slices.
func DeduplicateSliceEntries(input []string) []string {
	deduped, removed := sharedparsing.Deduplicate(input)
	for _, r := range removed {
		logger.Pl.W("Removing duplicate of entry %q", r)
	}
	return deduped
}
