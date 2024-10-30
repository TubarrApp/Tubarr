package domain

// AV copy
var (
	AVCodecCopy    = []string{"-codec", "copy"}
	VideoCodecCopy = []string{"-c:v", "copy"}
	AudioCodecCopy = []string{"-c:a", "copy"}
)

var (
	AudioToAAC          = []string{"-c:a", "aac"}
	VideoToH264Balanced = []string{"-c:v", "libx264", "-crf", "23", "-profile:v", "main"}
	AudioBitrate        = []string{"-b:a", "256k"}
)

var (
	PixelFmtYuv420p  = []string{"-pix_fmt", "yuv420p"}
	KeyframeBalanced = []string{"-g", "50", "-keyint_min", "30"}
)

var (
	OutputExt = []string{"-f", "mp4"}
)

var (
	NvidiaAccel = []string{"-hwaccel", "nvdec"}
	AMDAccel    = []string{"-hwaccel", "vulkan"}
	IntelAccel  = []string{"-hwaccel", "qsv"}
)
