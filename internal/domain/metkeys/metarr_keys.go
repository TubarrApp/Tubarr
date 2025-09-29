// Package metkeys holds command flag constants for Metarr.
package metkeys

const (
	Concurrency = "--concurrency"
	Debug       = "--debug"
	MaxCPU      = "--max-cpu"
	MinFreeMem  = "--min-free-mem"
	RenameStyle = "--rename-style"
)

// File ops
const (
	Ext                = "--ext"
	FilenameDateTag    = "--filename-date-tag"
	FilenameReplaceSfx = "--filename-replace-suffix"
	OutputDir          = "--output-directory"
)

// Meta ops
const (
	MetaOps = "--meta-ops"
	MetaOW  = "--meta-overwrite"
	MetaPS  = "--meta-preserve"
)

const (
	JSONDirectory  = "--json-directory"
	VideoDirectory = "--video-directory"
	JSONFile       = "--json-file"
	VideoFile      = "--video-file"
)

// FFmpeg
const (
	ExtraFFmpegArgs      = "--extra-ffmpeg-args"
	HWAccel              = "--hwaccel"
	GPUDir               = "--transcode-gpu-directory"
	TranscodeCodec       = "--transcode-codec"
	TranscodeAudioCodec  = "--transcode-audio-codec"
	TranscodeQuality     = "--transcode-quality"
	TranscodeVideoFilter = "--transcode-video-filter"
)
