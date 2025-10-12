// Package metarr builds and runs Metarr commands.
package metarr

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/metkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"github.com/spf13/viper"
)

type metCmdMapping struct {
	metarrValue metVals
	valType     metValTypes
	viperKey    string
	cmdKey      string
}

type metVals struct {
	str      string
	strSlice []string
	i        int
	f64      float64
}

type metValTypes int

const (
	str = iota
	strSlice
	i
	f64
)

// makeMetarrCommand combines arguments from both Viper config and model settings.
func makeMetarrCommand(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {

	// Remove non-matching URL-specific meta ops
	validOps := filterMetaOps(cu.ChanURLMetarrArgs.MetaOps, cu.URL)

	fields := []metCmdMapping{

		// Metarr args:
		{
			metarrValue: metVals{i: cu.ChanURLMetarrArgs.Concurrency},
			valType:     i,
			viperKey:    "", // Don't use Tubarr concurrency key, Metarr has more potential resource constraints
			cmdKey:      metkeys.Concurrency,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.Ext},
			valType:     str,
			viperKey:    keys.OutputFiletype,
			cmdKey:      metkeys.Ext,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.FilenameDateTag},
			valType:     str,
			viperKey:    keys.MFilenameDateTag,
			cmdKey:      metkeys.FilenameDateTag,
		},
		{
			metarrValue: metVals{strSlice: cu.ChanURLMetarrArgs.FilenameReplaceSfx},
			valType:     strSlice,
			viperKey:    keys.MFilenameReplaceSuffix,
			cmdKey:      metkeys.FilenameReplaceSfx,
		},
		{
			metarrValue: metVals{f64: cu.ChanURLMetarrArgs.MaxCPU},
			valType:     f64,
			viperKey:    "",
			cmdKey:      metkeys.MaxCPU,
		},
		{
			metarrValue: metVals{strSlice: validOps},
			valType:     strSlice,
			viperKey:    keys.MMetaOps,
			cmdKey:      metkeys.MetaOps,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.MinFreeMem},
			valType:     str,
			viperKey:    keys.MMinFreeMem,
			cmdKey:      metkeys.MinFreeMem,
		},
		{
			metarrValue: metVals{str: parseMetarrOutputDir(v, cu, c, dirParser)},
			valType:     str,
			viperKey:    "", // Fallback logic already exists in parseMetarrOutputDir.
			cmdKey:      metkeys.OutputDir,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.RenameStyle},
			valType:     str,
			viperKey:    keys.MRenameStyle,
			cmdKey:      metkeys.RenameStyle,
		},
		// Transcoding
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.ExtraFFmpegArgs},
			valType:     str,
			viperKey:    keys.MExtraFFmpegArgs,
			cmdKey:      metkeys.ExtraFFmpegArgs,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.UseGPU},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.HWAccel,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.GPUDir},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.GPUDir,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.TranscodeCodec},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeCodec,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.TranscodeQuality},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeQuality,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.TranscodeAudioCodec},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeAudioCodec,
		},
		{
			metarrValue: metVals{str: cu.ChanURLMetarrArgs.TranscodeVideoFilter},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeVideoFilter,
		},
		// Other
		{
			metarrValue: metVals{str: ""},
			valType:     str,
			viperKey:    keys.DebugLevel,
			cmdKey:      metkeys.Debug,
		},
	}

	singlesLen, sliceLen := calcNumElements(fields)

	// Map use to ensure uniqueness
	argMap := make(map[string]string, singlesLen)
	argSlicesMap := make(map[string][]string, sliceLen)

	// Viper slice comma parsing issue workaround, may need to do the same for all strSlice arguments
	argMap[metkeys.VideoFile] = cleanAndWrapCommaPaths(v.VideoPath)

	if v.JSONCustomFile == "" {
		argMap[metkeys.JSONFile] = cleanAndWrapCommaPaths(v.JSONPath)
	} else {
		argMap[metkeys.JSONFile] = cleanAndWrapCommaPaths(v.JSONCustomFile)
	}

	logging.I("Making Metarr argument for video %q and JSON file %q.", argMap[metkeys.VideoFile], argMap[metkeys.JSONFile])

	// Final args
	args := make([]string, 0, singlesLen+sliceLen)

	var metaOW bool
	for _, f := range fields {
		if processField(f, argMap, argSlicesMap) {
			metaOW = true
		}
	}

	for k, v := range argMap {
		args = append(args, k, v)
	}

	for k, v := range argSlicesMap {
		for _, val := range v {
			args = append(args, k, val)
		}
	}

	if metaOW {
		args = append(args, metkeys.MetaOW)
	}

	logging.I("Built Metarr argument list: %v", args)
	return args
}

// filterMetaOps filters valid meta ops for this video and linked channel.
func filterMetaOps(ops []string, videoChanURL string) (validOps []string) {
	logging.D(2, "Filtering meta ops %v", ops)
	for _, op := range ops {
		opChanURL, opPart := validation.CheckForOpURL(op)
		if opChanURL == "" || strings.EqualFold(strings.TrimSpace(opChanURL), strings.TrimSpace(videoChanURL)) {
			logging.D(2, "Meta ops channels match. Op URL %q, Channel URL %q", opChanURL, videoChanURL)
			validOps = append(validOps, opPart)
		} else {
			logging.I("Skipping meta op %q: channel URL mismatch (expected %q, got %q)", op, videoChanURL, opChanURL)
		}
	}
	return validOps
}

// processField processes each field in the argument map.
func processField(f metCmdMapping, argMap map[string]string, argSlicesMap map[string][]string) (metaOW bool) {
	switch f.valType {
	case i:
		if f.metarrValue.i > 0 {
			argMap[f.cmdKey] = strconv.Itoa(f.metarrValue.i)
		} else if f.viperKey != "" && viper.IsSet(f.viperKey) {
			argMap[f.cmdKey] = strconv.Itoa(viper.GetInt(f.viperKey))
		}
	case f64:
		if f.metarrValue.f64 > 0.0 {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", f.metarrValue.f64)
		} else if f.viperKey != "" && viper.IsSet(f.viperKey) {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", viper.GetFloat64(f.viperKey))
		}
	case str:
		if f.metarrValue.str != "" {
			argMap[f.cmdKey] = f.metarrValue.str
		} else if f.viperKey != "" && viper.IsSet(f.viperKey) {
			argMap[f.cmdKey] = viper.GetString(f.viperKey)
		}
	case strSlice:
		if len(f.metarrValue.strSlice) > 0 {
			argSlicesMap[f.cmdKey] = cleanCommaSliceValues(f.metarrValue.strSlice)
		} else if f.viperKey != "" && viper.IsSet(f.viperKey) {
			argSlicesMap[f.cmdKey] = cleanCommaSliceValues(viper.GetStringSlice(f.viperKey))
		}

		// Set Meta Overwrite flag if meta-ops arguments exist
		if f.cmdKey == metkeys.MetaOps {
			elemCount := len(f.metarrValue.strSlice)

			if viper.IsSet(f.viperKey) {
				elemCount += len(viper.GetStringSlice(f.viperKey))
			}

			if elemCount > 0 {
				logging.I("User set meta ops, will set meta overwrite key...")
				return true
			}
		}
	}
	return false
}

// parseMetarrOutputDir parses and returns the output directory.
func parseMetarrOutputDir(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) string {
	var (
		mArgs = cu.ChanURLMetarrArgs
		err   error
	)

	if mArgs.OutputDirMap, err = validation.ValidateMetarrOutputDirs(mArgs.OutputDir, mArgs.URLOutputDirs, c); err != nil {
		logging.E("Could not parse output directory map: %v", err)
	}

	switch {
	// #1 Priority: Explicit Viper flag set
	case viper.IsSet(keys.MoveOnComplete):
		d := viper.GetString(keys.MoveOnComplete)

		parsed, err := dirParser.ParseDirectory(d, v, "Metarr video")
		if err != nil {
			logging.E("Failed to parse directory %q for video with URL %q: %v", d, v.URL, err)
			break
		}

		viper.Set(keys.MoveOnComplete, parsed)
		return parsed

	// #2 Priority: Move operation filter output directory
	case v.MoveOpOutputDir != "":
		parsed, err := dirParser.ParseDirectory(v.MoveOpOutputDir, v, "Metarr video")
		if err != nil {
			logging.E("Failed to parse directory %q for video with URL %q: %v", v.MoveOpOutputDir, v.URL, err)
			break
		}
		return parsed

	// #3 Priority: Channel default output directory
	case mArgs.OutputDirMap[cu.URL] != "":
		parsed, err := dirParser.ParseDirectory(mArgs.OutputDirMap[cu.URL], v, "Metarr video")
		if err != nil {
			logging.E("Failed to parse directory %q for video with URL %q: %v", mArgs.OutputDirMap[cu.URL], v.URL, err)
			break
		}
		return parsed

	// #4 Priority: Use the output directory stored in channel directly
	case mArgs.OutputDir != "":
		parsed, err := dirParser.ParseDirectory(mArgs.OutputDir, v, "Metarr video")
		if err != nil {
			logging.E("Failed to parse directory %q for video with URL %q: %v", mArgs.OutputDir, v.URL, err)
			break
		}
		return parsed
	}

	// Return blank
	return ""
}

// calcNumElements returns the required map sizes.
func calcNumElements(fields []metCmdMapping) (singles, slices int) {
	singleElements := 2 // Start at 2 for VideoFile and JSONFile
	sliceElements := 0
	for _, f := range fields {
		switch f.valType {
		case str, i, f64:
			singleElements += 2 // One key and one value
		case strSlice:
			sliceElements += (len(f.metarrValue.strSlice) * 2) // One key and one value per entry
		}
	}
	return singleElements, sliceElements
}

// cleanAndWrapCommaPaths performs escaping for strings containing commas (can be misinterpreted in slices)
func cleanAndWrapCommaPaths(path string) string {

	if strings.ContainsRune(path, ',') {
		// Escape quotes if needed
		if strings.ContainsRune(path, '"') {
			escaped := strings.ReplaceAll(path, `"`, `\"`)
			if err := os.Rename(path, escaped); err != nil {
				logging.E("Failed to escape quotes in filename %q: %v", path, err)
			} else {
				path = escaped
			}
		}

		// Prefix and suffix with double quotes
		b := strings.Builder{}
		b.Grow(len(path) + 2)
		b.WriteByte('"')
		b.WriteString(path)
		b.WriteByte('"')

		return b.String()
	}

	return path
}

// cleanCommaSliceValues escapes and fixes slice entries containing commas
func cleanCommaSliceValues(slice []string) []string {
	result := make([]string, 0, len(slice))
	for _, val := range slice {
		result = append(result, cleanAndWrapCommaPaths(val))
	}
	return result
}
