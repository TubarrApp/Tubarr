// Package enums contains enumerated variables.
package enums

type LineSelectType int

const (
	LSingle LineSelectType = iota
	LMulti
)

type DLFilterType int

const (
	DLFilterOmit DLFilterType = iota
	DLFilterContains
	DLFilterOmitField
	DLFilterContainsField
)
