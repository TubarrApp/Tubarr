package models

import (
	"strings"
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
		filters = append(filters, sharedparsing.FormatFilter(f, withMustAny))
	}
	return filters
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

// MetaFilterMoveOpsToString converts a meta filter move op back to its string form.
//
// url|title:contains:dog||/dogs
func MetaFilterMoveOpsToString(m MetaFilterMoveOps) string {
	return sharedparsing.FormatFilter(m.Filter(), false) + sharedparsing.FilterSeparator + strings.ReplaceAll(m.OutputDir, `\`, `\\`)
}

// ------ Filtered Meta Ops -----------------------------------------------------------------

// FilteredMetaOpsToSlice converts filtered meta ops back to slice.
func FilteredMetaOpsToSlice(f FilteredMetaOps) []string {
	slice := make([]string, 0, len(f.Filters))

	filterStrings := FiltersArrayToSlice(f.Filters, false)
	metaOpStrings := sharedparsing.FormatMetaOps(f.MetaOps)

	if len(filterStrings) != len(metaOpStrings) {
		logger.Pl.E("Mismatch in filter string and meta op string entry amounts for %v (got filters: %d, meta ops %d)", f, len(filterStrings), len(metaOpStrings))
		return []string{}
	}

	for i := range filterStrings {
		slice = append(slice, filterStrings[i]+sharedparsing.FilterSeparator+metaOpStrings[i])
	}
	return slice
}

// ------ Filtered Filename Ops -----------------------------------------------------------------

// FilteredFilenameOpsToSlice converts filtered filename ops back to slice.
func FilteredFilenameOpsToSlice(f FilteredFilenameOps) []string {
	slice := make([]string, 0, len(f.Filters))

	filterStrings := FiltersArrayToSlice(f.Filters, false)
	filenameOpStrings := sharedparsing.FormatFilenameOps(f.FilenameOps)

	if len(filterStrings) != len(filenameOpStrings) {
		logger.Pl.E("Mismatch in filter string and meta op string entry amounts for %v (got filters: %d, meta ops %d)", f, len(filterStrings), len(filenameOpStrings))
		return []string{}
	}

	for i := range filterStrings {
		slice = append(slice, filterStrings[i]+sharedparsing.FilterSeparator+filenameOpStrings[i])
	}
	return slice
}
