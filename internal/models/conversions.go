package models

import (
	"strings"

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

// FilteredMetaOpsToString converts filtered meta ops back to their string form, the ops joined by "&&".
//
// title:contains:dog&&duration:more-than:600||title:suffix: [dogs]&&genre:set:Dogs
func FilteredMetaOpsToString(f FilteredMetaOps) string {
	ops := sharedparsing.FormatMetaOps(f.MetaOps)
	return sharedparsing.FormatFilters(f.Filters) + sharedconsts.FilterSeparator + strings.Join(ops, sharedconsts.FilterJoiner)
}

// ------ Filtered Filename Ops -----------------------------------------------------------------

// FilteredFilenameOpsToString converts filtered filename ops back to their string form, the ops joined by "&&".
//
// title:contains:dog||prefix:[Dogs] &&suffix: (dog)
func FilteredFilenameOpsToString(f FilteredFilenameOps) string {
	ops := sharedparsing.FormatFilenameOps(f.FilenameOps)
	return sharedparsing.FormatFilters(f.Filters) + sharedconsts.FilterSeparator + strings.Join(ops, sharedconsts.FilterJoiner)
}
