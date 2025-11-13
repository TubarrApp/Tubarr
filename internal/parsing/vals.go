package parsing

// NilOrZeroValue returns zero value if nil (e.g. integer = 0), or dereferenced T.
func NilOrZeroValue[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
