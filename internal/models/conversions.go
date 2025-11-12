package models

import "fmt"

// ------ Filters -----------------------------------------------------------------
// FiltersArrayToSlice converts filter models back into slice form.
func FiltersArrayToSlice(fModels []Filters) []string {
	if len(fModels) == 0 {
		return []string{}
	}
	filters := make([]string, 0, len(fModels))

	for _, f := range fModels {
		var op string
		// Add channel URL if present
		if f.ChannelURL != "" {
			op = f.ChannelURL + "|"
		}

		op += f.Field + ":" + f.ContainsOmits + ":" + f.Value + ":" + f.MustAny
		filters = append(filters, op)
	}
	return filters
}

// ------ Meta Ops -----------------------------------------------------------------
// MetaOpsArrayToSlice converts meta ops models back into slice form.
func MetaOpsArrayToSlice(moModels []MetaOps) []string {
	if len(moModels) == 0 {
		return []string{}
	}
	metaOps := make([]string, 0, len(moModels))

	for _, m := range moModels {
		var op string
		// Add channel URL if present
		if m.ChannelURL != "" {
			op = m.ChannelURL + "|"
		}

		// Reconstruct operations
		switch m.OpType {
		case "date-tag", "delete-date-tag":
			op += m.Field + ":" + m.OpType + ":" + m.OpLoc + ":" + m.DateFormat

		case "replace", "replace-suffix", "replace-prefix":
			op += m.Field + ":" + m.OpType + ":" + m.OpFindString + ":" + m.OpValue

		default:
			op += m.Field + ":" + m.OpType + ":" + m.OpValue
		}

		// Append
		metaOps = append(metaOps, op)
	}
	return metaOps
}

// MetaOpToSlice converts a meta ops model back to a string.
func MetaOpToSlice(m MetaOps) string {
	var op string
	// Add channel URL if present
	if m.ChannelURL != "" {
		op = m.ChannelURL + "|"
	}

	// Reconstruct operations
	switch m.OpType {
	case "date-tag", "delete-date-tag":
		op += m.Field + ":" + m.OpType + ":" + m.OpLoc + ":" + m.DateFormat

	case "replace", "replace-suffix", "replace-prefix":
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
		var op string
		// Add channel URL if present
		if f.ChannelURL != "" {
			op = f.ChannelURL + "|"
		}

		// Reconstruct operations
		switch f.OpType {
		case "date-tag", "delete-date-tag":
			op += f.OpType + ":" + f.OpLoc + ":" + f.DateFormat

		case "replace", "replace-suffix", "replace-prefix":
			op += f.OpType + ":" + f.OpFindString + ":" + f.OpValue

		default:
			op += f.OpType + ":" + f.OpValue
		}

		// Append
		filenameOps = append(filenameOps, op)
	}
	return filenameOps
}

// FilenameOpToSlice converts a filename ops model back to a string.
func FilenameOpToSlice(f FilenameOps) string {
	var op string
	// Add channel URL if present
	if f.ChannelURL != "" {
		op = f.ChannelURL + "|"
	}

	// Reconstruct operations
	switch f.OpType {
	case "date-tag", "delete-date-tag":
		op += f.OpType + ":" + f.OpLoc + ":" + f.DateFormat

	case "replace", "replace-suffix", "replace-prefix":
		op += f.OpType + ":" + f.OpFindString + ":" + f.OpValue

	default:
		op += f.OpType + ":" + f.OpValue
	}

	return op
}

// ------ Meta Filter Move Ops -----------------------------------------------------------------
// MetaFilterMoveOpsToSlice converts meta filter move ops back to slice.
func MetaFilterMoveOpsToSlice(m MetaFilterMoveOps) string {
	var op string
	// Add channel URL if present
	if m.ChannelURL != "" {
		op = m.ChannelURL + "|"
	}

	op += m.Field + ":" + m.Value + ":" + m.OutputDir

	return op
}

// ------ Filtered Meta Ops -----------------------------------------------------------------
// FilteredMetaOpsToSlice converts filtered meta ops back to slice.
func FilteredMetaOpsToSlice(f FilteredMetaOps) []string {
	slice := make([]string, 0, len(f.Filters))

	filterStrings := FiltersArrayToSlice(f.Filters)
	metaOpStrings := MetaOpsArrayToSlice(f.MetaOps)

	if len(filterStrings) != len(metaOpStrings) {
		fmt.Printf("Mismatch in filter string and meta op string entry amounts for %v (got filters: %d, meta ops %d)", f, len(filterStrings), len(metaOpStrings))
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
		fmt.Printf("Mismatch in filter string and meta op string entry amounts for %v (got filters: %d, meta ops %d)", f, len(filterStrings), len(filenameOpStrings))
		return []string{}
	}

	for i := range filterStrings {
		slice = append(slice, filterStrings[i]+"|"+filenameOpStrings[i])
	}
	return slice
}
