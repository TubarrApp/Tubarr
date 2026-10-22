package validation

import (
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// DeduplicateSliceEntries removes duplicate entries in slices.
func DeduplicateSliceEntries(input []string) []string {
	deduped, removed := sharedparsing.Deduplicate(input)
	for _, r := range removed {
		logger.Pl.W("Removing duplicate of entry %q", r)
	}
	return deduped
}
