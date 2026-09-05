package models

import (
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// ------ Filters -----------------------------------------------------------------

// FiltersArrayToSlice converts filter models back into slice form.
func FiltersArrayToSlice(fModels []Filters, withMustAny bool) []string {
	if len(fModels) == 0 {
		return []string{}
	}
	filters := make([]string, 0, len(fModels))

	for _, f := range fModels {
		filters = append(filters, FiltersToString(f, withMustAny))
	}
	return filters
}

// FiltersToString converts a filter model back into string form.
//
// withMustAny must match what the filter's context accepts when parsed back in, i.e. false
// for filtered meta/filename ops, which carry no condition.
func FiltersToString(f Filters, withMustAny bool) string {
	fields := []string{f.Field, f.FilterType, f.Value}
	if withMustAny {
		fields = append(fields, f.MustAny)
	}

	op := sharedparsing.JoinEscaped(fields, ':')
	// Add channel URL if present. A URL cannot hold an unescaped separator.
	if f.ChannelURL != "" {
		op = f.ChannelURL + "|" + op
	}
	return op
}

// ------ Meta Filter Move Ops -----------------------------------------------------------------

// MetaFilterMoveOpsArrayToSlice converts meta filter move ops back to slice.
func MetaFilterMoveOpsArrayToSlice(mf []MetaFilterMoveOps) []string {
	out := make([]string, 0, len(mf))
	for _, m := range mf {
		out = append(out, MetaFilterMoveOpsToString(m))
	}
	return out
}

// MetaFilterMoveOpsToString converts meta filter move ops back to slice.
//
// url|title:dog:/dogs
func MetaFilterMoveOpsToString(m MetaFilterMoveOps) string {
	// OutputDir is a path, so it is the field most likely to hold a separator.
	op := sharedparsing.JoinEscaped([]string{m.Field, m.ContainsValue, m.OutputDir}, ':')
	// Add channel URL if present.
	if m.ChannelURL != "" {
		op = m.ChannelURL + "|" + op
	}
	return op
}

// ------ Filtered Meta Ops -----------------------------------------------------------------

// FilteredMetaOpsToSlice converts filtered meta ops back to slice.
func FilteredMetaOpsToSlice(f FilteredMetaOps) []string {
	slice := make([]string, 0, len(f.Filters))

	filterStrings := FiltersArrayToSlice(f.Filters, false)
	metaOpStrings := sharedparsing.FormatMetaOps(f.MetaOps, "", true)

	if len(filterStrings) != len(metaOpStrings) {
		logger.Pl.E("Mismatch in filter string and meta op string entry amounts for %v (got filters: %d, meta ops %d)", f, len(filterStrings), len(metaOpStrings))
		return []string{}
	}

	for i := range filterStrings {
		slice = append(slice, filterStrings[i]+"|"+metaOpStrings[i])
	}
	return slice
}

// ------ Filtered Filename Ops -----------------------------------------------------------------

// FilteredFilenameOpsToSlice converts filtered filename ops back to slice.
func FilteredFilenameOpsToSlice(f FilteredFilenameOps) []string {
	slice := make([]string, 0, len(f.Filters))

	filterStrings := FiltersArrayToSlice(f.Filters, false)
	filenameOpStrings := sharedparsing.FormatFilenameOps(f.FilenameOps, "", true)

	if len(filterStrings) != len(filenameOpStrings) {
		logger.Pl.E("Mismatch in filter string and meta op string entry amounts for %v (got filters: %d, meta ops %d)", f, len(filterStrings), len(filenameOpStrings))
		return []string{}
	}

	for i := range filterStrings {
		slice = append(slice, filterStrings[i]+"|"+filenameOpStrings[i])
	}
	return slice
}
