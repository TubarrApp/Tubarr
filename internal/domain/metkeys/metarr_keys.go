// Package metkeys holds command flag constants for Metarr.
package metkeys

// Main program elements.
const (
	Concurrency = "--concurrency"
	Debug       = "--debug"
	MaxCPU      = "--max-cpu"
	MinFreeMem  = "--min-free-mem"
	RenameStyle = "--rename-style"
)

// File operations.
const (
	Ext         = "--output-ext"
	FilenameOps = "--filename-ops"
	OutputDir   = "--output-directory"
)

// Metadata operations.
const (
	MetaOps = "--meta-ops"
	MetaOW  = "--meta-overwrite"
	MetaPS  = "--meta-preserve"
)

// JSON/Video files and directories.
const (
	JSONDirectory  = "--json-directory"
	VideoDirectory = "--video-directory"
	JSONFile       = "--json-file"
	VideoFile      = "--video-file"
)

// FFmpeg related commands.
const (
	ExtraFFmpegArgs      = "--extra-ffmpeg-args"
	HWAccel              = "--hwaccel"
	GPUDir               = "--transcode-gpu-directory"
	TranscodeCodec       = "--transcode-video-codec"
	TranscodeAudioCodec  = "--transcode-audio-codec"
	TranscodeQuality     = "--transcode-quality"
	TranscodeVideoFilter = "--transcode-video-filter"
)
