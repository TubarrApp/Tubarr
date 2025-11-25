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
	Ext           = "--output-ext"
	FilenameOps   = "--filename-ops"
	OutputDir     = "--output-directory"
	PurgeMetaFile = "--purge-metafile"
)

// Metadata operations.
const (
	MetaOps = "--meta-ops"
	MetaOW  = "--meta-overwrite"
)

// JSON/Video files and directories.
const (
	MetaFile  = "--meta-file"
	VideoFile = "--video-file"
)

// FFmpeg related commands.
const (
	ExtraFFmpegArgs       = "--extra-ffmpeg-args"
	TranscodeGPU          = "--transcode-gpu"
	TranscodeGPUDirectory = "--transcode-gpu-node"
	TranscodeVideoCodecs  = "--transcode-video-codecs"
	TranscodeAudioCodecs  = "--transcode-audio-codecs"
	TranscodeQuality      = "--transcode-quality"
	TranscodeVideoFilter  = "--transcode-video-filter"
)
