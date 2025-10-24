package validation

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

var validMetaActions = map[string]bool{
	"append": true, "copy-to": true, "paste-from": true, "prefix": true,
	"trim-prefix": true, "trim-suffix": true, "replace": true, "set": true,
	"date-tag": true, "delete-date-tag": true,
}

var validFilenameActions = map[string]bool{
	"append": true, "prefix": true, "trim-prefix": true, "trim-suffix": true,
	"replace": true, "date-tag": true, "delete-date-tag": true,
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
			case "trim-suffix", "trim-prefix", "replace":
				newFilenameOp.OpType = split[0]       // e.g. 'trim-suffix'
				newFilenameOp.OpFindString = split[1] // e.g. '_1'
				newFilenameOp.OpValue = split[2]      // e.g. ''

				// Build uniqueness key
				key = strings.Join([]string{newFilenameOp.OpType, newFilenameOp.OpFindString, newFilenameOp.OpValue}, ":")

			case "date-tag", "delete-date-tag":
				newFilenameOp.OpType = split[0]     // e.g. 'date-tag'
				newFilenameOp.OpLoc = split[1]      // e.g. 'prefix'
				newFilenameOp.DateFormat = split[2] // e.g. 'ymd'

				if newFilenameOp.OpLoc != "prefix" && newFilenameOp.OpLoc != "suffix" {
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

				if newMetaOp.OpLoc != "prefix" && newMetaOp.OpLoc != "suffix" {
					logging.E("invalid date tag location %q, use prefix or suffix", newMetaOp.OpLoc)
					continue
				}
				if !ValidateDateFormat(newMetaOp.DateFormat) {
					logging.E("invalid date tag format %q", newMetaOp.DateFormat)
					continue
				}
				// Build uniqueness key
				key = strings.Join([]string{newMetaOp.Field, newMetaOp.OpType}, ":")

			case "replace":
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
	case "spaces", "underscores", "fixes-only":
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
	logging.E("Invalid date format entered as %q, please enter up to three characters (where 'Y' is yyyy and 'y' is yy)", dateFmt)
	return false
}

// ValidateMinFreeMem flag verifies the format of the free memory flag.
func ValidateMinFreeMem(minFreeMem string) error {
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
		return "", "", fmt.Errorf("cannot access driver location %q: %w", devDir, err)
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
	case "h265", "x265":
		return "hevc", nil
	case "":
		if accel == "" {
			return "", nil
		}
		return "", fmt.Errorf("acceleration type %q requires a codec to be specified. Tubarr supports h264 and HEVC (h265)", accel)
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
