// Package utils holds various utility functions.
package utils

// StarPassword returns a string of asterisks as long as the password.
func StarPassword(p string) string {
	var sp []rune
	for range p {
		sp = append(sp, '*')
	}
	return string(sp)
}
