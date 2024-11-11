package domain

type LineSelectType int

const (
	L_SINGLE LineSelectType = iota
	L_MULTI
)

type DLFilterType int

const (
	DLFILTER_OMIT DLFilterType = iota
	DLFILTER_CONTAINS
)
