package validation

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/utils/logging"
)

// ValidateMetaOps parses the meta transformation operations.
func ValidateMetaOps(metaOps []string) ([]string, error) {
	if len(metaOps) == 0 {
		logging.I("No meta operations passed in to verification")
		return metaOps, nil
	}

	const (
		dupMsg = "Duplicate meta operation %q, skipping"
	)
	var b strings.Builder

	logging.D(1, "Validating meta operations...")
	valid := make([]string, 0, len(metaOps))
	exists := make(map[string]bool, len(metaOps))

	for _, m := range metaOps {
		split := strings.Split(m, ":")
		switch len(split) {
		case 3:
			switch split[1] {
			case "append", "copy-to", "paste-from", "prefix", "trim-prefix", "trim-suffix", "replace", "set":

				b.Grow(len(m))
				b.WriteString(split[0])
				b.WriteByte(':')
				b.WriteString(split[1])
				b.WriteByte(':')
				b.WriteString(split[2])
				key := b.String()
				b.Reset()

				if exists[key] {
					logging.I(dupMsg, m)
					continue
				}
				exists[key] = true
				valid = append(valid, m)
			default:
				return nil, fmt.Errorf("invalid meta operation %q", split[1])
			}
		case 4:
			if split[1] == "date-tag" {
				switch split[2] {
				case "prefix", "suffix":
					if ValidateDateFormat(split[3]) {

						b.Grow(len(m))
						b.WriteString(split[0])
						b.WriteByte(':')
						b.WriteString(split[1])
						b.WriteByte(':')
						b.WriteString(split[2]) // Leave out split[3], probably don't want multiple date tags with diff formats
						key := b.String()
						b.Reset()

						if exists[key] {
							logging.I(dupMsg, m)
							continue
						}
						exists[key] = true
						valid = append(valid, m)
					}
				default:
					return nil, fmt.Errorf("invalid date tag location %q, use prefix or suffix", split[2])
				}
			}
		default:
			return nil, fmt.Errorf("invalid meta op %q", m)
		}
	}

	if len(valid) != 0 {
		return valid, nil
	}

	return nil, errors.New("no valid meta operations")
}

// ValidateFilenameSuffixReplace checks if the input format for filename suffix replacement is valid.
func ValidateFilenameSuffixReplace(fileSfxReplace []string) ([]string, error) {
	valid := make([]string, 0, len(fileSfxReplace))

	lengthStrings := 0
	for _, pair := range fileSfxReplace {
		parts := strings.Split(pair, ":")
		if len(parts) < 2 {
			return nil, errors.New("invalid use of filename-replace-suffix, values must be written as (suffix:replacement)")
		}
		lengthStrings += len(parts[0]+parts[1]) + 1
		valid = append(valid, pair)
	}
	return valid, nil
}

// ValidateRenameFlag validates the rename style to apply.
func ValidateRenameFlag(flag string) error {

	// Trim whitespace for more robust validation
	flag = strings.TrimSpace(strings.ToLower(flag))

	switch flag {
	case "spaces", "space", "underscores", "underscore", "fixes", "fix", "fixes-only", "fixesonly":
		return nil
	default:
		return errors.New("'spaces', 'underscores' or 'fixes-only' not selected for renaming style, skipping these modifications")
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
	logging.E(0, "Invalid date format entered as %q, please enter up to three characters (where 'Y' is yyyy and 'y' is yy)", dateFmt)
	return false
}

// ValidateMinFreeMem flag verifies the format of the free memory flag.
func ValidateMinFreeMem(minFreeMem string) error {
	minFreeMem = strings.TrimSuffix(strings.ToUpper(minFreeMem), "B")
	switch {
	case strings.HasSuffix(minFreeMem, "G"), strings.HasSuffix(minFreeMem, "M"), strings.HasSuffix(minFreeMem, "K"):
		if len(minFreeMem) < 2 {
			return fmt.Errorf("invalid format for min free mem: %q", minFreeMem)
		}
	default:
		if _, err := strconv.Atoi(minFreeMem); err != nil {
			return fmt.Errorf("invalid min free memory argument %q for Metarr, should ", minFreeMem)
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
		} else {
			valid = true
			break
		}
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

// ValidateTranscodeAudioCodec verifies the audio codec to use for transcode/encode operations.
func ValidateTranscodeAudioCodec(a string) (audioCodec string, err error) {
	a = strings.ToLower(a)
	switch a {
	case "aac", "ac3", "alac", "copy", "eac3", "libmp3lame", "libopus", "libvorbis":
		return a, nil
	case "mp3":
		return "libmp3lame", nil
	case "opus":
		return "libopus", nil
	case "vorbis":
		return "libvorbis", nil
	case "":
		return "", nil
	default:
		return "", fmt.Errorf("audio codec flag %q is not currently implemented in this program, aborting", a)
	}
}

// ValidateGPU validates the user input GPU selection.
func ValidateGPU(g, devDir string) (gpu, gpuDir string, err error) {
	g = strings.ToLower(g)

	switch g {
	case "qsv", "intel":
		gpu = "qsv"
	case "amd", "radeon", "vaapi":
		gpu = "vaapi"
	case "nvidia", "cuda":
		gpu = "cuda"
	case "auto", "automatic", "automated":
		return "auto", devDir, nil // Return early, no directory needed for auto
	case "":
		return "", "", nil // Return early, no HW acceleration required
	default:
		return "", devDir, fmt.Errorf("entered GPU %q not supported. Tubarr supports Auto, Intel, AMD, or Nvidia", g)
	}

	_, err = os.Stat(devDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("driver location %q does not appear to exist?", devDir)
		}
	}

	return gpu, devDir, nil
}

// ValidateTranscodeCodec validates the user input codec selection.
func ValidateTranscodeCodec(c string, accel string) (codec string, err error) {

	logging.D(3, "Checking codec %q with acceleration type %q...", c, accel)
	c = strings.ToLower(c)
	c = strings.ReplaceAll(c, ".", "")
	switch c {
	case "h264", "hevc":
		return c, nil
	case "avc", "x264":
		return "h264", nil
	case "h265":
		return "hevc", nil
	case "":
		if accel == "" {
			return "", nil
		} else {
			return "", fmt.Errorf("entered codec %q not supported with acceleration type %q. Tubarr supports h264 and HEVC (h265)", c, accel)
		}
	default:
		return "", fmt.Errorf("entered codec %q not supported. Tubarr supports h264 and HEVC (h265)", c)
	}
}

// ValidateTranscodeQuality validates the transcode quality preset.
func ValidateTranscodeQuality(q string) (quality string, err error) {
	q = strings.ToLower(q)
	q = strings.ReplaceAll(q, " ", "")

	switch q {
	case "p1", "p2", "p3", "p4", "p5", "p6", "p7":
		logging.I("Got transcode quality profile %q", q)
		return q, nil
	}

	qNum, err := strconv.Atoi(q)
	if err != nil {
		return "", fmt.Errorf("input should be p1 to p7, validation of transcoder quality failed")
	}

	var qualProf string
	switch {
	case qNum < 0:
		qualProf = "p1"
	case qNum > 7:
		qualProf = "p7"
	default:
		qualProf = "p" + strconv.Itoa(qNum)
	}
	logging.I("Got transcode quality profile %q", qualProf)
	return qualProf, nil
}

// ValidateTranscodeVideoFilter validates the transcode video filter preset.
func ValidateTranscodeVideoFilter(q string) (vf string, err error) {
	logging.D(1, "No checks in place for transcode video filter at present...")
	return q, nil
}
