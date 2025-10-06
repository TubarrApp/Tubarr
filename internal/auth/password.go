// Package auth contains functions related to authorization
package auth

import "strings"

// StarPassword returns a string of asterisks matching the password length.
func StarPassword(p string) string {
	return strings.Repeat("*", len(p))
}
