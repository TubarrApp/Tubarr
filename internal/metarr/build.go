// Package metarr builds and runs Metarr commands.
package metarr

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/metkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/TubarrApp/gocommon/sharedtags"
)

// defaultFilenameSet renames finished videos to their full title. Downloads are named "title [id]", keeping apart
// videos with the same (or a non-Latin) title.
const defaultFilenameSet = sharedconsts.OpSet + ":{{" + sharedtags.JFulltitle + "}}"

// withDefaultFilenameSet returns the filename ops to run: the video's overrides or channel URL's, else the global ones,
// with defaultFilenameSet added when none of them sets the name.
func withDefaultFilenameSet(v *models.Video, cu *models.ChannelURL, ops []string) []string {
	if len(ops) == 0 && v.Overrides == nil && abstractions.IsSet(keys.MFilenameOps) {
		ops = abstractions.GetStringSlice(keys.MFilenameOps)
	}
	parsed, err := parsing.ParseFilenameOps(ops)
	if err != nil {
		return ops // Metarr reports the invalid op.
	}
	if slices.ContainsFunc(parsed, func(op models.FilenameOps) bool {
		return op.OpType == sharedconsts.OpSet && sharedparsing.AppliesToChannel(op.ChannelURL, cu.URL)
	}) {
		return ops
	}
	return append(slices.Clip(ops), defaultFilenameSet)
}

// metCmdMapping holds mapping data for building Metarr commands.
//
// valType: the type of value being processed (e.g. string, int, bool).
// metarrValue: holds the value set for the type (e.g. "/path/to/file").
// viperKey: is the Viper configuration key to check if no value is set in metarrValue (e.g. 'metarr-filename-ops').
// cmdKey: is the Metarr command-line argument key (e.g. '--video-file').
type metCmdMapping struct {
	valType     metValTypes
	metarrValue metVals
	viperKey    string
	cmdKey      string
}

// metVals holds possible Metarr argument values.
type metVals struct {
	str      string
	strSlice []string
	i        int
	f64      float64
	b        bool
}

// metValTypes defines the type of Metarr argument value.
type metValTypes int

const (
	str = iota
	strSlice
	i
	f64
	b
)

// makeMetarrCommand combines arguments from both Viper config and model settings.
func makeMetarrCommand(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// Download As overrides replace the channel URL's and global ops. Otherwise file ops override DB ops, then apply filtering.
	var validMetaOps, validFilenameOps []string
	metaOpsKey, filenameOpsKey := keys.MMetaOps, keys.MFilenameOps
	if v.Overrides != nil {
		validMetaOps, validFilenameOps = overrideMetaOps(v, c), overrideFilenameOps(v, c)
		metaOpsKey, filenameOpsKey = "", ""
	} else {
		validMetaOps, validFilenameOps = loadAndMergeMetaOps(v, cu, c, dirParser), loadAndMergeFilenameOps(v, cu, c, dirParser)
	}
	validFilenameOps = withDefaultFilenameSet(v, cu, validFilenameOps)

	// Download As args, blank ones using the channel URL's.
	var o models.MetarrArgs
	if v.Overrides != nil {
		o = v.Overrides.MetarrArgs
	}
	m := cu.ChanURLMetarrArgs
	videoCodecs, audioCodecs := m.TranscodeVideoCodecs, m.TranscodeAudioCodecs
	if len(o.TranscodeVideoCodecs) > 0 {
		videoCodecs = o.TranscodeVideoCodecs
	}
	if len(o.TranscodeAudioCodecs) > 0 {
		audioCodecs = o.TranscodeAudioCodecs
	}
	gpu, gpuNode := cmp.Or(o.TranscodeGPU, m.TranscodeGPU), cmp.Or(o.TranscodeGPUNode, m.TranscodeGPUNode)
	if gpu == validation.GPUNone {
		gpu, gpuNode = "", ""
	}
	var thumbnail string
	var stripThumbnail bool
	if v.Overrides != nil {
		thumbnail, stripThumbnail = v.Overrides.Thumbnail, v.Overrides.StripThumbnail
	}

	mOutDirParser := parsing.NewDirectoryParser(c, cu, parsing.AllTags)
	// Define fields to process.
	fields := []metCmdMapping{
		// Metarr args:
		{
			metarrValue: metVals{strSlice: validFilenameOps},
			valType:     strSlice,
			viperKey:    filenameOpsKey,
			cmdKey:      metkeys.FilenameOps,
		},
		{
			metarrValue: metVals{f64: cmp.Or(o.MaxCPU, m.MaxCPU)},
			valType:     f64,
			viperKey:    "",
			cmdKey:      metkeys.MaxCPU,
		},
		{
			metarrValue: metVals{strSlice: validMetaOps},
			valType:     strSlice,
			viperKey:    metaOpsKey,
			cmdKey:      metkeys.MetaOps,
		},
		{
			metarrValue: metVals{str: cmp.Or(o.MinFreeMem, m.MinFreeMem)},
			valType:     str,
			viperKey:    keys.MMinFreeMem,
			cmdKey:      metkeys.MinFreeMem,
		},
		{
			metarrValue: metVals{str: parseMetarrOutputDir(v, cu, mOutDirParser)},
			valType:     str,
			viperKey:    "", // Fallback logic already exists in parseMetarrOutputDir.
			cmdKey:      metkeys.OutputDir,
		},
		{
			metarrValue: metVals{str: cmp.Or(o.OutputExt, m.OutputExt)},
			valType:     str,
			viperKey:    keys.OutputFiletype,
			cmdKey:      metkeys.Ext,
		},
		{
			metarrValue: metVals{str: cmp.Or(o.RenameStyle, m.RenameStyle)},
			valType:     str,
			viperKey:    keys.MRenameStyle,
			cmdKey:      metkeys.RenameStyle,
		},
		// Transcoding.
		{
			metarrValue: metVals{str: cmp.Or(o.ExtraFFmpegArgs, m.ExtraFFmpegArgs)},
			valType:     str,
			viperKey:    keys.MExtraFFmpegArgs,
			cmdKey:      metkeys.ExtraFFmpegArgs,
		},
		{
			metarrValue: metVals{str: gpu},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeGPU,
		},
		{
			metarrValue: metVals{str: gpuNode},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeGPUNode,
		},
		{
			metarrValue: metVals{str: thumbnail},
			valType:     str,
			cmdKey:      metkeys.Thumbnail,
		},
		{
			metarrValue: metVals{b: stripThumbnail},
			valType:     b,
			cmdKey:      metkeys.StripThumbnail,
		},
		{
			metarrValue: metVals{strSlice: videoCodecs},
			valType:     strSlice,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeVideoCodecs,
		},
		{
			metarrValue: metVals{str: cmp.Or(o.TranscodeQuality, m.TranscodeQuality)},
			valType:     str,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeQuality,
		},
		{
			metarrValue: metVals{strSlice: audioCodecs},
			valType:     strSlice,
			viperKey:    "",
			cmdKey:      metkeys.TranscodeAudioCodecs,
		},
		{
			metarrValue: metVals{str: cmp.Or(o.TranscodeVideoFilter, m.TranscodeVideoFilter)},
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
		// No metadata purge: Tubarr purges once it has read the file, see finishMetafile.
	}
	singlesLen, sliceLen, flagLen := calcNumElements(fields)

	// Map use to ensure uniqueness.
	argMap := make(map[string]string, singlesLen)
	argSlicesMap := make(map[string][]string, sliceLen)
	argFlags := make([]string, 0, flagLen)

	// Metarr takes each path whole, so commas and quotes in file names need no escaping.
	argMap[metkeys.VideoFile] = v.VideoFilePath

	if v.JSONCustomFile == "" {
		argMap[metkeys.MetaFile] = v.JSONFilePath
	} else {
		argMap[metkeys.MetaFile] = v.JSONCustomFile
	}

	// Build argument list.
	logger.Pl.I("Making Metarr argument for video %q and JSON file %q.", argMap[metkeys.VideoFile], argMap[metkeys.MetaFile])
	args := make([]string, 0, singlesLen+sliceLen)

	var metaOW bool
	// Process each field.
	for _, f := range fields {
		if processField(f, argMap, argSlicesMap, &argFlags) {
			metaOW = true
		}
	}
	if metaOW {
		args = append(args, metkeys.MetaOW)
	}

	// Combine all arguments from map.
	for k, v := range argMap {
		args = append(args, k, v)
	}

	// Combine all slice arguments from map.
	for k, v := range argSlicesMap {
		for _, val := range v {
			args = append(args, k, val)
		}
	}

	// Flags.
	args = append(args, argFlags...)

	return args
}

// processField processes each field in the argument map.
func processField(f metCmdMapping, argMap map[string]string, argSlicesMap map[string][]string, argFlags *[]string) (metaOW bool) {
	switch f.valType {
	case b: // Boolean flag: Set directly into the flags slice.
		if f.metarrValue.b {
			*argFlags = append(*argFlags, f.cmdKey)
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) && abstractions.GetBool(f.viperKey) {
			*argFlags = append(*argFlags, f.cmdKey)
		}
	case i:
		if f.metarrValue.i > 0 {
			argMap[f.cmdKey] = strconv.Itoa(f.metarrValue.i)
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			argMap[f.cmdKey] = strconv.Itoa(abstractions.GetInt(f.viperKey))
		}
	case f64:
		if f.metarrValue.f64 > 0.0 {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", f.metarrValue.f64)
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			argMap[f.cmdKey] = fmt.Sprintf("%.2f", abstractions.GetFloat64(f.viperKey))
		}
	case str:
		if f.metarrValue.str != "" {
			argMap[f.cmdKey] = f.metarrValue.str
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			viperVal := abstractions.GetString(f.viperKey)
			if viperVal != "" {
				argMap[f.cmdKey] = viperVal
			}
		}
	case strSlice:
		if len(f.metarrValue.strSlice) > 0 {
			argSlicesMap[f.cmdKey] = f.metarrValue.strSlice
		} else if f.viperKey != "" && abstractions.IsSet(f.viperKey) {
			viperSlice := abstractions.GetStringSlice(f.viperKey)
			if len(viperSlice) > 0 {
				argSlicesMap[f.cmdKey] = viperSlice
			}
		}
		// Set Meta Overwrite flag if meta-ops arguments exist.
		if f.cmdKey == metkeys.MetaOps {
			elemCount := len(f.metarrValue.strSlice)

			if abstractions.IsSet(f.viperKey) {
				elemCount += len(abstractions.GetStringSlice(f.viperKey))
			}

			if elemCount > 0 {
				logger.Pl.I("User set meta ops, will set meta overwrite key...")
				return true
			}
		}
	}
	return false
}

// parseMetarrOutputDir parses and returns the output directory.
func parseMetarrOutputDir(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) string {
	mArgs := cu.ChanURLMetarrArgs

	switch {
	// Download As output directory, chosen for this video.
	case v.Overrides != nil && v.Overrides.OutputDir != "":
		parsed, err := dirParser.ParseDirectory(v.Overrides.OutputDir, "Download As output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", v.Overrides.OutputDir, v.URL, err)
			break
		}
		return parsed

	// #1 Priority: Explicit Viper flag set.
	case abstractions.IsSet(keys.MoveOnComplete):
		d := abstractions.GetString(keys.MoveOnComplete)

		parsed, err := dirParser.ParseDirectory(d, "Metarr output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", d, v.URL, err)
			break
		}
		return parsed

	// #2 Priority: Move operation filter output directory.
	case v.MoveOpOutputDir != "":
		parsed, err := dirParser.ParseDirectory(v.MoveOpOutputDir, "Metarr output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", v.MoveOpOutputDir, v.URL, err)
			break
		}
		return parsed

	// #3 Priority: Use the output directory stored in channel URL directly.
	case mArgs.OutputDir != "":
		parsed, err := dirParser.ParseDirectory(mArgs.OutputDir, "Metarr output directory")
		if err != nil {
			logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", mArgs.OutputDir, v.URL, err)
			break
		}
		return parsed
	}
	// Return blank
	return ""
}

// calcNumElements returns the required map sizes.
func calcNumElements(fields []metCmdMapping) (singles, slices, flags int) {
	singleElements := 2 // Start at 2 for VideoFile and JSONFile.
	sliceElements := 0
	flagElements := 0
	for _, f := range fields {
		switch f.valType {
		case str, i, f64:
			singleElements += 2 // One key and one value.
		case strSlice:
			sliceElements += (len(f.metarrValue.strSlice) * 2) // One key and one value per entry.
		case b:
			flagElements++ // Flag only.
		}
	}
	return singleElements, sliceElements, flagElements
}
