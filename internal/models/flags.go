package models

// ChannelFlagValues contains concrete value types for all flags.
type ChannelFlagValues struct {
	// Channel identifiers
	Name              string
	URLs              []string
	ChannelConfigFile string

	// Directory paths
	VideoDir string
	JSONDir  string
	OutDir   string

	// Configuration files
	DLFilterFile            string
	MoveOpFile              string
	MetaOpsFile             string
	FilteredMetaOpsFile     string
	FilenameOpsFile         string
	FilteredFilenameOpsFile string

	// Authentication details
	Username    string
	Password    string
	LoginURL    string
	AuthDetails []string

	// Notification details
	Notification []string

	// Download settings
	CookiesFromBrowser     string
	ExternalDownloader     string
	ExternalDownloaderArgs string
	MaxFilesize            string
	YTDLPOutputExt         string
	FromDate               string
	ToDate                 string
	UseGlobalCookies       bool

	// Filter and operation settings
	DLFilters           []string
	MoveOps             []string
	MetaOps             []string
	FilenameOps         []string
	FilteredMetaOps     []string
	FilteredFilenameOps []string
	URLOutputDirs       []string

	// Metarr settings
	MetarrExt   string
	RenameStyle string
	MinFreeMem  string

	// Transcoding settings
	TranscodeGPU         string
	TranscodeGPUNode     string
	TranscodeQuality     string
	TranscodeVideoFilter string
	TranscodeVideoCodec  []string
	TranscodeAudioCodec  []string

	// Extra arguments
	ExtraYTDLPVideoArgs string
	ExtraYTDLPMetaArgs  string
	ExtraFFmpegArgs     string

	// Concurrency and performance settings
	CrawlFreq         int
	Concurrency       int
	MetarrConcurrency int
	Retries           int
	MaxCPU            float64

	// Boolean flags
	Pause     bool
	IgnoreRun bool
}
