package validation

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

var validMetaActions = map[string]bool{
	"append": true, "copy-to": true, "paste-from": true, "prefix": true,
	"replace-prefix": true, "replace-suffix": true, "replace": true, "set": true,
	"date-tag": true, "delete-date-tag": true,
}

var validFilenameActions = map[string]bool{
	"append": true, "prefix": true, "replace-prefix": true, "replace-suffix": true,
	"replace": true, "date-tag": true, "delete-date-tag": true, "set": true,
}

// ValidateFilenameOps parses and validates filename transformation operations.
func ValidateFilenameOps(filenameOps []string) ([]models.FilenameOps, error) {
	if len(filenameOps) == 0 {
		logging.D(4, "No filename operations passed in to verification")
		return nil, nil
	}
	const dupMsg = "Duplicate filename operation %q, skipping"
	const invalidArg = "Invalid filename operation %q (format should be: 'prefix:[COOL CATEGORY] ', or 'date-tag:prefix:ymd', etc.)"

	// Deduplicate
	filenameOps = DeduplicateSliceEntries(filenameOps)

	// Proceed
	valid := make([]models.FilenameOps, 0, len(filenameOps))
	exists := make(map[string]bool, len(filenameOps))

	logging.D(1, "Validating filename operations %v...", filenameOps)

	// Check filename ops
	for _, op := range filenameOps {
		opURL, opPart := CheckForOpURL(op)
		split := EscapedSplit(opPart, ':')

		if len(split) < 2 || len(split) > 3 {
			logging.E(invalidArg, op)
			continue
		}

		if !validFilenameActions[split[0]] {
			logging.E(invalidArg, op)
			continue
		}

		var newFilenameOp models.FilenameOps
		var key string
		switch len(split) {
		case 2: // e.g. 'prefix:[DOG VIDEOS]'
			newFilenameOp.OpType = split[0]  // e.g. 'append'
			newFilenameOp.OpValue = split[1] // e.g. '(new)'

			// Build uniqueness key
			key = strings.Join([]string{newFilenameOp.OpType, newFilenameOp.OpValue}, ":")

		case 3: // e.g. 'replace-suffix:_1:'
			switch split[0] {
			case "replace-suffix", "replace-prefix", "replace":
				newFilenameOp.OpType = split[0]       // e.g. 'replace-suffix'
				newFilenameOp.OpFindString = split[1] // e.g. '_1'
				newFilenameOp.OpValue = split[2]      // e.g. ''

				// Build uniqueness key
				key = strings.Join([]string{newFilenameOp.OpType, newFilenameOp.OpFindString, newFilenameOp.OpValue}, ":")

			case "date-tag", "delete-date-tag":
				newFilenameOp.OpType = split[0]     // e.g. 'date-tag'
				newFilenameOp.OpLoc = split[1]      // e.g. 'prefix'
				newFilenameOp.DateFormat = split[2] // e.g. 'ymd'

				if newFilenameOp.OpType == "date-tag" &&
					newFilenameOp.OpLoc != "prefix" && newFilenameOp.OpLoc != "suffix" {
					logging.E("invalid date tag location %q, use prefix or suffix", newFilenameOp.OpLoc)
					continue
				}

				if newFilenameOp.OpType == "delete-date-tag" &&
					newFilenameOp.OpLoc != "prefix" && newFilenameOp.OpLoc != "suffix" && newFilenameOp.OpLoc != "all" {
					logging.E("invalid date tag location %q, use prefix or suffix", newFilenameOp.OpLoc)
					continue
				}

				if !ValidateDateFormat(newFilenameOp.DateFormat) {
					logging.E("invalid date tag format %q", newFilenameOp.DateFormat)
					continue
				}
				// Build uniqueness key
				key = newFilenameOp.OpType
			}
		default:
			logging.E(invalidArg, opPart)
			continue
		}

		// Completed switch, check if key exists (sets true on key if not)
		if exists[key] {
			logging.I(dupMsg, opPart)
			continue
		}
		exists[key] = true

		// Add channel URL is present
		if opURL != "" {
			newFilenameOp.ChannelURL = opURL
		}

		// Add successful filename operation
		valid = append(valid, newFilenameOp)
	}

	// Check length of valid filename operations
	if len(valid) == 0 {
		return nil, errors.New("no valid filename operations")
	}
	return valid, nil
}

// ValidateMetaOps parses and validates meta transformation operations.
func ValidateMetaOps(metaOps []string) ([]models.MetaOps, error) {
	if len(metaOps) == 0 {
		logging.D(4, "No meta operations passed in to verification")
		return nil, nil
	}
	const dupMsg = "Duplicate meta operation %q, skipping"

	// Deduplicate
	metaOps = DeduplicateSliceEntries(metaOps)

	// Proceed
	valid := make([]models.MetaOps, 0, len(metaOps))
	exists := make(map[string]bool, len(metaOps))

	logging.D(1, "Validating meta operations %v...", metaOps)

	// Check meta ops
	for _, op := range metaOps {
		opURL, opPart := CheckForOpURL(op)
		split := EscapedSplit(opPart, ':')

		if len(split) < 3 || len(split) > 4 {
			logging.E("Invalid meta operation %q", op)
			continue
		}

		if !validMetaActions[split[1]] {
			logging.E("invalid meta operation %q", split[1])
			continue
		}

		var newMetaOp models.MetaOps
		var key string
		switch len(split) {
		case 3: // e.g. 'director:set:Spielberg'
			newMetaOp.Field = split[0]   // e.g. 'director'
			newMetaOp.OpType = split[1]  // e.g. 'set'
			newMetaOp.OpValue = split[2] // e.g. 'Spielberg'

			// Build uniqueness key
			key = strings.Join([]string{newMetaOp.Field, newMetaOp.OpType, newMetaOp.OpValue}, ":")

		case 4: // e.g. 'title:date-tag:suffix:ymd' or 'title:replace:old:new'
			newMetaOp.Field = split[0]
			newMetaOp.OpType = split[1]

			switch newMetaOp.OpType {
			case "date-tag", "delete-date-tag":
				newMetaOp.OpLoc = split[2]
				newMetaOp.DateFormat = split[3]

				if newMetaOp.OpType == "date-tag" &&
					newMetaOp.OpLoc != "prefix" && newMetaOp.OpLoc != "suffix" {
					logging.E("invalid date tag location %q, use prefix or suffix", newMetaOp.OpLoc)
					continue
				}

				if newMetaOp.OpType == "delete-date-tag" &&
					newMetaOp.OpLoc != "prefix" && newMetaOp.OpLoc != "suffix" && newMetaOp.OpLoc != "all" {
					logging.E("invalid date tag location %q, use prefix or suffix", newMetaOp.OpLoc)
					continue
				}

				if !ValidateDateFormat(newMetaOp.DateFormat) {
					logging.E("invalid date tag format %q", newMetaOp.DateFormat)
					continue
				}
				// Build uniqueness key
				key = strings.Join([]string{newMetaOp.Field, newMetaOp.OpType}, ":")

			case "replace", "replace-suffix", "replace-prefix":
				newMetaOp.OpFindString = split[2]
				newMetaOp.OpValue = split[3]

				key = strings.Join([]string{newMetaOp.Field, newMetaOp.OpType, newMetaOp.OpFindString, newMetaOp.OpValue}, ":")

			default:
				logging.E("invalid 4-part meta op type %q", newMetaOp.OpType)
				continue
			}
		default:
			logging.E("invalid meta op %q", opPart)
			continue
		}

		// Completed switch, check if key exists (sets true on key if not)
		if exists[key] {
			logging.I(dupMsg, opPart)
			continue
		}
		exists[key] = true

		// Add channel URL is present
		if opURL != "" {
			newMetaOp.ChannelURL = opURL
		}

		// Add successful meta operation
		valid = append(valid, newMetaOp)
	}
	if len(valid) == 0 {
		return nil, errors.New("no valid meta operations")
	}
	return valid, nil
}

// ValidateRenameFlag validates the rename style to apply.
func ValidateRenameFlag(flag string) error {
	// Trim whitespace for more robust validation
	flag = strings.TrimSpace(strings.ToLower(flag))

	switch flag {
	case "spaces", "underscores", "fixes-only", "skip", "":
		return nil
	default:
		return errors.New("'spaces', 'underscores', 'skip', or 'fixes-only' not selected for renaming style, skipping these modifications")
	}
}

// ValidateDateFormat returns the date format enum type.
func ValidateDateFormat(dateFmt string) bool {
	if len(dateFmt) > 2 {
		switch dateFmt {
		case "Ymd", "ymd", "Ydm", "ydm", "dmY", "dmy", "mdY", "mdy", "md", "dm":
			return true
		}
	}
	logging.E("Invalid date format entered as %q, please enter up to three characters (where 'Y' is yyyy and 'y' is yy)", dateFmt)
	return false
}

// ValidateMinFreeMem flag verifies the format of the free memory flag.
func ValidateMinFreeMem(minFreeMem string) error {
	if minFreeMem == "" {
		return nil
	}
	minFreeMem = strings.ToUpper(minFreeMem)
	minFreeMem = strings.TrimSuffix(minFreeMem, "B")

	switch {
	case strings.HasSuffix(minFreeMem, "G"),
		strings.HasSuffix(minFreeMem, "M"),
		strings.HasSuffix(minFreeMem, "K"):
		if len(minFreeMem) < 2 {
			return fmt.Errorf("invalid format for min free mem: %q", minFreeMem)
		}
	default:
		if _, err := strconv.Atoi(minFreeMem); err != nil {
			return fmt.Errorf("invalid min free memory argument %q for Metarr, should end in 'G', 'GB', 'M', 'MB', 'K' or 'KB'", minFreeMem)
		}
	}
	return nil
}

// ValidateOutputFiletype verifies the output filetype is valid for FFmpeg.
func ValidateOutputFiletype(o string) (dottedExt string, err error) {
	o = strings.ToLower(strings.TrimSpace(o))

	fmt.Printf("Output filetype: %s\n", o)

	valid := false
	for _, ext := range consts.AllVidExtensions {
		if o != ext {
			continue
		}
		valid = true
		break
	}

	if valid {
		logging.I("Outputting files as %s", o)
		if !strings.HasPrefix(o, ".") {
			o = "." + o
		}
		return o, nil
	}
	return "", fmt.Errorf("output filetype %q is not supported", o)
}

// ValidatePurgeMetafiles checks and sets the type of metafile purge to perform.
func ValidatePurgeMetafiles(purgeType string) bool {
	purgeType = strings.TrimSpace(purgeType)
	purgeType = strings.ToLower(purgeType)
	purgeType = strings.ReplaceAll(purgeType, ".", "")

	switch purgeType {
	case "all", "json", "nfo":
		fmt.Printf("Purge metafiles post-Metarr: %s\n", purgeType)
		return true
	}
	return false
}

// ValidateGPU validates the GPU selection.
func ValidateGPU(g, devDir string) (gpuType string, dir string, err error) {
	g = strings.ToLower(strings.TrimSpace(g))

	// Alias lookup
	if !consts.ValidGPUAccelTypes[g] {
		switch g {
		case "", "none":
			return "", "", nil
		case "automatic", "automate", "automated":
			g = consts.AccelTypeAuto
		case "radeon", "amd":
			g = consts.AccelTypeAMF
		case "intel":
			g = consts.AccelTypeIntel
		case "nvidia", "nvenc":
			g = consts.AccelTypeNvidia
		default:
			return "", "", fmt.Errorf("GPU %q not supported. Valid: auto, intel, amd, nvidia", g)
		}
	}
	// Check device directory exists
	if devDir != "" {
		if _, err := os.Stat(devDir); err != nil {
			return "", "", fmt.Errorf("cannot access driver location %q: %w", devDir, err)
		}
	}

	return g, devDir, nil
}

// ValidateAudioTranscodeCodecSlice sets mappings for audio codec inputs.
func ValidateAudioTranscodeCodecSlice(pairs []string) (validPairs []string, err error) {
	// Deduplicate
	dedupPairs := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		if !slices.Contains(dedupPairs, p) {
			dedupPairs = append(dedupPairs, p)
		}
	}

	// Iterate deduped pairs
	for _, p := range dedupPairs {
		split := strings.Split(p, ":")
		if _, err := validateTranscodeAudioCodec(split[0]); err != nil {
			return nil, err
		}

		// Singular value, apply to every entry
		if len(split) < 2 {
			validPairs = append(validPairs, p)
			continue
		}

		// Multi value entry, apply specific output to specific input
		output := split[1]
		if _, err = validateTranscodeAudioCodec(output); err != nil {
			return nil, err
		}
		validPairs = append(validPairs, p)
	}

	logging.D(1, "Got audio codec array: %v", validPairs)
	return validPairs, nil
}

// validateTranscodeAudioCodec validates the audio codec to use.
func validateTranscodeAudioCodec(a string) (string, error) {
	a = strings.ToLower(strings.TrimSpace(a))
	a = strings.ReplaceAll(a, ".", "")
	a = strings.ReplaceAll(a, "-", "")
	a = strings.ReplaceAll(a, "_", "")

	if !consts.ValidAudioCodecs[a] {
		switch a {
		case "", "none", "auto", "automatic", "automated":
			a = ""
		case "aac", "aaclc", "m4a", "mp4a", "aaclowcomplexity":
			a = consts.ACodecAAC
		case "alac", "applelossless", "m4aalac":
			a = consts.ACodecALAC
		case "dca", "dts", "dtshd", "dtshdma", "dtsma", "dtsmahd", "dtscodec":
			a = consts.ACodecDTS
		case "ddplus", "dolbydigitalplus", "ac3e", "ec3", "eac3":
			a = consts.ACodecEAC3
		case "flac", "flaccodec", "fla", "losslessflac":
			a = consts.ACodecFLAC
		case "mp2", "mpa", "mpeg2audio", "mpeg2", "m2a", "mp2codec":
			a = consts.ACodecMP2
		case "mp3", "libmp3lame", "mpeg3", "mpeg3audio", "mpg3", "mp3codec":
			a = consts.ACodecMP3
		case "opus", "opuscodec", "oggopus", "webmopus":
			a = consts.ACodecOpus
		case "pcm", "wavpcm", "rawpcm", "pcm16", "pcms16le", "pcms24le", "pcmcodec":
			a = consts.ACodecPCM
		case "truehd", "dolbytruehd", "thd", "truehdcodec":
			a = consts.ACodecTrueHD
		case "vorbis", "oggvorbis", "webmvorbis", "vorbiscodec", "vorb":
			a = consts.ACodecVorbis
		case "wav", "wave", "waveform", "pcmwave", "wavcodec":
			a = consts.ACodecWAV
		default:
			return "", fmt.Errorf(
				"audio codec %q is not supported. Supported: %v", a, consts.ValidAudioCodecs)
		}
	}
	return a, nil
}

// ValidateVideoTranscodeCodecSlice validates the input video transcode slice.
func ValidateVideoTranscodeCodecSlice(pairs []string, accel string) (validPairs []string, err error) {
	// Deduplicate
	dedupPairs := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		if !slices.Contains(dedupPairs, p) {
			dedupPairs = append(dedupPairs, p)
		}
	}

	// Iterate deduped pairs
	for _, p := range dedupPairs {
		split := strings.Split(p, ":")
		if _, err := validateVideoTranscodeCodec(split[0], accel); err != nil {
			return nil, err
		}

		// Singular value, apply to every entry
		if len(split) < 2 {
			validPairs = append(validPairs, p)
			continue
		}

		// Multi value entry, apply specific output to specific input
		output := split[1]
		if _, err = validateVideoTranscodeCodec(output, accel); err != nil {
			return nil, err
		}

		validPairs = append(validPairs, p)
	}

	logging.D(1, "Got video codec array: %v", validPairs)
	return validPairs, nil
}

// validateTranscodeCodec validates the video codec based on GPU acceleration.
func validateVideoTranscodeCodec(c, accel string) (string, error) {
	c = strings.ToLower(strings.TrimSpace(c))
	c = strings.ReplaceAll(c, ".", "")
	c = strings.ReplaceAll(c, "-", "")
	c = strings.ReplaceAll(c, "_", "")

	if !consts.ValidVideoCodecs[c] {
		switch c {
		case "", "none", "auto", "automatic", "automated":
			if accel == "" || accel == consts.AccelTypeAuto {
				return "", nil
			}
			return "", fmt.Errorf("GPU acceleration %q requires a codec (entered %q)", accel, c)
		case "aom", "libaom", "libaomav1", "av01", "svtav1", "libsvtav1":
			c = consts.VCodecAV1
		case "x264", "avc", "h264avc", "mpeg4avc", "h264mpeg4", "libx264":
			c = consts.VCodecH264
		case "x265", "h265", "hevc265", "libx265", "hevc":
			c = consts.VCodecHEVC
		case "mpg2", "mpeg2video", "mpeg2v", "mpg", "mpeg", "mpeg2":
			c = consts.VCodecMPEG2
		case "libvpx", "vp08", "vpx", "vpx8":
			c = consts.VCodecVP8
		case "libvpxvp9", "libvpx9", "vpx9", "vp09", "vpxvp9":
			c = consts.VCodecVP9
		default:
			return "", fmt.Errorf("video codec %q is not supported. Supported: h264, hevc, av1, mpeg2, vp8, vp9", c)
		}
	}
	return c, nil
}

// ValidateTranscodeQuality validates the transcode quality preset.
func ValidateTranscodeQuality(q string) (quality string, err error) {
	if q == "" {
		return q, nil
	}
	q = strings.ToLower(q)
	q = strings.ReplaceAll(q, " ", "")
	qNum, err := strconv.ParseInt(q, 10, 64)
	if err != nil {
		return "", fmt.Errorf("transcode quality input should be numerical")
	}
	qNum = min(max(qNum, 0), 51)

	return strconv.FormatInt(qNum, 10), nil
}

// ValidateTranscodeVideoFilter validates the transcode video filter preset.
func ValidateTranscodeVideoFilter(q string) (vf string, err error) {
	logging.D(1, "No checks in place for transcode video filter...")
	return q, nil
}
