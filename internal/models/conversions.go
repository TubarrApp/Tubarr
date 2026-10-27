package models

import (
	"github.com/TubarrApp/gocommon/sharedconsts"
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
// title:contains:dog&&duration:more-than:600||/dogs
func MetaFilterMoveOpsToString(m MetaFilterMoveOps) string {
	return sharedparsing.FormatFilters(m.Filters) + sharedconsts.FilterSeparator + m.OutputDir
}

// ------ Filtered Meta Ops -----------------------------------------------------------------

// FilteredMetaOpsToSlice converts filtered meta ops back to slice, one entry per meta op.
func FilteredMetaOpsToSlice(f FilteredMetaOps) []string {
	filters := sharedparsing.FormatFilters(f.Filters)
	slice := make([]string, 0, len(f.MetaOps))
	for _, op := range sharedparsing.FormatMetaOps(f.MetaOps) {
		slice = append(slice, filters+sharedconsts.FilterSeparator+op)
	}
	return slice
}

// ------ Filtered Filename Ops -----------------------------------------------------------------

// FilteredFilenameOpsToSlice converts filtered filename ops back to slice, one entry per filename op.
func FilteredFilenameOpsToSlice(f FilteredFilenameOps) []string {
	filters := sharedparsing.FormatFilters(f.Filters)
	slice := make([]string, 0, len(f.FilenameOps))
	for _, op := range sharedparsing.FormatFilenameOps(f.FilenameOps) {
		slice = append(slice, filters+sharedconsts.FilterSeparator+op)
	}
	return slice
}
