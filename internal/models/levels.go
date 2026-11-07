package models

import "slices"

// SettingsLevel is one level a channel URL's filters and op lists come from (its channel's profile, its channel, its
// profile or itself), as saved, bar the lists a nearer level replaces.
type SettingsLevel struct {
	Settings   *Settings
	MetarrArgs *MetarrArgs
}

// The functions below combine one list from levels given farthest first. Files are read only when the lists are used,
// so each takes the entries read from every level's file for that list, by level, or nil when they aren't read.

// StackFilters returns the download filters of every level, each level's typed ones then its file's.
func StackFilters(levels []SettingsLevel, fileFilters [][]Filters) []Filters {
	var filters []Filters
	for i, l := range levels {
		if l.Settings != nil {
			filters = slices.Concat(filters, l.Settings.Filters)
		}
		filters = slices.Concat(filters, levelFile(fileFilters, i))
	}
	return filters
}

// StackMoveOps returns the move ops of every level, nearest level first as the first match wins, each level's typed ones
// then its file's.
func StackMoveOps(levels []SettingsLevel, fileOps [][]MetaFilterMoveOps) []MetaFilterMoveOps {
	var ops []MetaFilterMoveOps
	for i, l := range levels {
		levelOps := levelFile(fileOps, i)
		if l.Settings != nil {
			levelOps = slices.Concat(l.Settings.MetaFilterMoveOps, levelOps)
		}
		ops = slices.Concat(levelOps, ops)
	}
	return ops
}

// StackMetaOps returns the meta ops of every level: a level's file ops beat its typed ones they conflict with, and a
// nearer level's beat a farther one's.
func StackMetaOps(levels []SettingsLevel, fileOps [][]MetaOps) []MetaOps {
	var ops []MetaOps
	for i, l := range levels {
		levelOps := levelFile(fileOps, i)
		if l.MetarrArgs != nil {
			levelOps = slices.Concat(levelOps, FilterConflictingMetaOps(levelOps, l.MetarrArgs.MetaOps))
		}
		ops = slices.Concat(FilterConflictingMetaOps(levelOps, ops), levelOps)
	}
	return ops
}

// StackFilenameOps returns the filename ops of every level: a level's file ops beat its typed ones they conflict with,
// and a nearer level's beat a farther one's.
func StackFilenameOps(levels []SettingsLevel, fileOps [][]FilenameOps) []FilenameOps {
	var ops []FilenameOps
	for i, l := range levels {
		levelOps := levelFile(fileOps, i)
		if l.MetarrArgs != nil {
			levelOps = slices.Concat(levelOps, FilterConflictingFilenameOps(levelOps, l.MetarrArgs.FilenameOps))
		}
		ops = slices.Concat(FilterConflictingFilenameOps(levelOps, ops), levelOps)
	}
	return ops
}

// FilteredMetaOpsByLevel returns each level's filtered meta ops, its typed ones then its file's. They stay by level, as
// which of two matching ops wins depends on the levels they come from.
func FilteredMetaOpsByLevel(levels []SettingsLevel, fileOps [][]FilteredMetaOps) [][]FilteredMetaOps {
	byLevel := make([][]FilteredMetaOps, len(levels))
	for i, l := range levels {
		if l.MetarrArgs != nil {
			byLevel[i] = l.MetarrArgs.FilteredMetaOps
		}
		byLevel[i] = slices.Concat(byLevel[i], levelFile(fileOps, i))
	}
	return byLevel
}

// FilteredFilenameOpsByLevel returns each level's filtered filename ops, its typed ones then its file's. They stay by
// level, as which of two matching ops wins depends on the levels they come from.
func FilteredFilenameOpsByLevel(levels []SettingsLevel, fileOps [][]FilteredFilenameOps) [][]FilteredFilenameOps {
	byLevel := make([][]FilteredFilenameOps, len(levels))
	for i, l := range levels {
		if l.MetarrArgs != nil {
			byLevel[i] = l.MetarrArgs.FilteredFilenameOps
		}
		byLevel[i] = slices.Concat(byLevel[i], levelFile(fileOps, i))
	}
	return byLevel
}

// levelFile returns the entries read from level i's file, or nil if none were read.
func levelFile[T any](files [][]T, i int) []T {
	if i < len(files) {
		return files[i]
	}
	return nil
}
