package models

// Config represents the top-level configuration structure
type Config struct {
	Sites    map[string]*Site    `toml:"sites"`
	Channels map[string]*Channel `toml:"channels"`
	RawURLs  *RawURLs            `toml:"raw-urls"`
}

// Site represents a video site configuration
type Site struct {
	Channels []string     `toml:"channels"`
	Settings SiteSettings `toml:"settings"`
}

// SiteSettings contains site-specific settings
type SiteSettings struct {
	SkipTitles        []string `toml:"skip:title"`
	Retries           int      `toml:"retries"`
	RestrictFilenames string   `toml:"restrict_filenames"`
}

// Channel represents a channel configuration
type Channel struct {
	URL        string   `toml:"url"`
	Directory  string   `toml:"directory"`
	SkipTitles []string `toml:"skip:title"`
	Settings   ChannelSettings
}

// SiteSettings contains channel-specific settings
type ChannelSettings struct {
	SkipTitles        []string `toml:"skip:title"`
	Retries           int      `toml:"retries"`
	RestrictFilenames string   `toml:"restrict_filenames"`
}

// RawURLs contains direct video/channel URLs
type RawURLs struct {
	URLs []string `toml:"url"`
}
