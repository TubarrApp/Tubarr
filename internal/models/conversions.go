package models

import (
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// ------ Filters -----------------------------------------------------------------

// FiltersArrayToSlice converts filter models back into slice form.
func FiltersArrayToSlice(fModels []Filters) []string {
	if len(fModels) == 0 {
		return []string{}
	}
	filters := make([]string, 0, len(fModels))

	for _, f := range fModels {
		filters = append(filters, FiltersToString(f))
	}
	return filters
}

// FiltersToString converts a filter model back into string form.
func FiltersToString(f Filters) string {
	var op string
	// Add channel URL if present
	if f.ChannelURL != "" {
		op = f.ChannelURL + "|"
	}
	// Reconstruct operation
	op += f.Field + ":" + f.ContainsOmits + ":" + f.Value + ":" + f.MustAny
	return op
}

// ------ Meta Ops -----------------------------------------------------------------

// MetaOpsArrayToSlice converts meta ops models back into slice form.
func MetaOpsArrayToSlice(moModels []MetaOps) []string {
	if len(moModels) == 0 {
		return []string{}
	}
	metaOps := make([]string, 0, len(moModels))

	for _, m := range moModels {
		metaOps = append(metaOps, MetaOpToString(m, true))
	}
	return metaOps
}

// MetaOpToString converts a meta ops model back to a string.
func MetaOpToString(m MetaOps, addURLPart bool) string {
	var op string
	// Add channel URL if present
	if addURLPart && m.ChannelURL != "" {
		op = m.ChannelURL + "|"
	}
	// Reconstruct operations
	switch m.OpType {
	case sharedconsts.OpDateTag, sharedconsts.OpDeleteDateTag:
		op += m.Field + ":" + m.OpType + ":" + m.OpLoc + ":" + m.DateFormat

	case sharedconsts.OpReplace, sharedconsts.OpReplaceSuffix, sharedconsts.OpReplacePrefix:
		op += m.Field + ":" + m.OpType + ":" + m.OpFindString + ":" + m.OpValue

	default:
		op += m.Field + ":" + m.OpType + ":" + m.OpValue
	}
	return op
}

// ------ Filename Ops -----------------------------------------------------------------

// FilenameOpsArrayToSlice converts filename ops models back into slice form.
func FilenameOpsArrayToSlice(foModels []FilenameOps) []string {
	if len(foModels) == 0 {
		return []string{}
	}
	filenameOps := make([]string, 0, len(foModels))

	for _, f := range foModels {
		filenameOps = append(filenameOps, FilenameOpToString(f, true))
	}
	return filenameOps
}

// FilenameOpToString converts a filename ops model back to a string.
//
// date-tag:prefix:ymd
// replace:_:
// prefix:[Video]
func FilenameOpToString(f FilenameOps, addURLPart bool) string {
	var op string
	// Add channel URL if present
	if addURLPart && f.ChannelURL != "" {
		op = f.ChannelURL + "|"
	}
	// Reconstruct operations
	switch f.OpType {
	case sharedconsts.OpDateTag, sharedconsts.OpDeleteDateTag:
		op += f.OpType + ":" + f.OpLoc + ":" + f.DateFormat

	case sharedconsts.OpReplace, sharedconsts.OpReplaceSuffix, sharedconsts.OpReplacePrefix:
		op += f.OpType + ":" + f.OpFindString + ":" + f.OpValue

	default:
		op += f.OpType + ":" + f.OpValue
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
	var op string
	// Add channel URL if present
	if m.ChannelURL != "" {
		op = m.ChannelURL + "|"
	}
	op += m.Field + ":" + m.ContainsValue + ":" + m.OutputDir
	return op
}

// ------ Filtered Meta Ops -----------------------------------------------------------------

// FilteredMetaOpsToSlice converts filtered meta ops back to slice.
func FilteredMetaOpsToSlice(f FilteredMetaOps) []string {
	slice := make([]string, 0, len(f.Filters))

	filterStrings := FiltersArrayToSlice(f.Filters)
	metaOpStrings := MetaOpsArrayToSlice(f.MetaOps)

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

	filterStrings := FiltersArrayToSlice(f.Filters)
	filenameOpStrings := FilenameOpsArrayToSlice(f.FilenameOps)

	if len(filterStrings) != len(filenameOpStrings) {
		logger.Pl.E("Mismatch in filter string and meta op string entry amounts for %v (got filters: %d, meta ops %d)", f, len(filterStrings), len(filenameOpStrings))
		return []string{}
	}

	for i := range filterStrings {
		slice = append(slice, filterStrings[i]+"|"+filenameOpStrings[i])
	}
	return slice
}
