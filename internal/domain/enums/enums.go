// Package enums holds shared enumerated types.
package enums

// SettingsLevel says what settings belong to, which decides the output directory tags their directories can use.
type SettingsLevel int

// SettingsLevel enums.
const (
	ChannelLevel    SettingsLevel = iota // A channel or profile.
	ChannelURLLevel                      // A channel URL or Download As.
)
