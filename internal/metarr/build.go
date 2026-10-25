// Package metarr builds and runs Metarr commands.
package metarr

import (
	"cmp"
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
func withDefaultFilenameSet(v *models.Video, ops []string) []string {
	if len(ops) == 0 && v.Overrides == nil && abstractions.IsSet(keys.MFilenameOps) {
		ops = abstractions.GetStringSlice(keys.MFilenameOps)
	}
	parsed, _, err := sharedparsing.ParseFilenameOps(ops)
	if err != nil {
		return ops // Metarr reports the invalid op.
	}
	if slices.ContainsFunc(parsed, func(op models.FilenameOps) bool { return op.OpType == sharedconsts.OpSet }) {
		return ops
	}
	return append(slices.Clip(ops), defaultFilenameSet)
}

// makeMetarrCommand combines arguments from both Viper config and model settings.
func makeMetarrCommand(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// 'Download As' overrides replace the channel URL's and global ops. Otherwise file ops override DB ops, then apply filtering.
	var metaOps, filenameOps []string
	if v.Overrides != nil {
		metaOps, filenameOps = overrideMetaOps(v, c), overrideFilenameOps(v, c)
	} else {
		metaOps, filenameOps = loadAndMergeMetaOps(v, cu, c, dirParser), loadAndMergeFilenameOps(v, cu, c, dirParser)
		if len(metaOps) == 0 && abstractions.IsSet(keys.MMetaOps) {
			metaOps = abstractions.GetStringSlice(keys.MMetaOps)
		}
	}
	filenameOps = withDefaultFilenameSet(v, filenameOps)

	// 'Download As' args, blank ones using the channel URL's.
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

	// Metarr takes each path whole, so commas and quotes in file names need no escaping.
	metaFile := cmp.Or(v.JSONCustomFile, v.JSONFilePath)
	logger.Pl.I("Making Metarr argument for video %q and JSON file %q.", v.VideoFilePath, metaFile)
	args := []string{metkeys.VideoFile, v.VideoFilePath, metkeys.MetaFile, metaFile}

	// add adds the flag with val, unless val is blank.
	add := func(flag, val string) {
		if val != "" {
			args = append(args, flag, val)
		}
	}
	// addEach adds the flag once per value.
	addEach := func(flag string, vals []string) {
		for _, val := range vals {
			args = append(args, flag, val)
		}
	}

	// Metarr args:
	addEach(metkeys.FilenameOps, filenameOps)
	addEach(metkeys.MetaOps, metaOps)
	if len(metaOps) > 0 {
		logger.Pl.I("User set meta ops, will set meta overwrite key...")
		args = append(args, metkeys.MetaOW)
	}
	if maxCPU := cmp.Or(o.MaxCPU, m.MaxCPU); maxCPU > 0 {
		add(metkeys.MaxCPU, strconv.FormatFloat(maxCPU, 'f', 2, 64))
	}
	add(metkeys.MinFreeMem, cmp.Or(o.MinFreeMem, m.MinFreeMem, configString(keys.MMinFreeMem)))
	add(metkeys.OutputDir, parseMetarrOutputDir(v, cu, parsing.NewDirectoryParser(c, cu, parsing.AllTags)))
	add(metkeys.Ext, cmp.Or(o.OutputExt, m.OutputExt, configString(keys.OutputFiletype)))
	add(metkeys.RenameStyle, cmp.Or(o.RenameStyle, m.RenameStyle, configString(keys.MRenameStyle)))

	// Transcoding.
	add(metkeys.ExtraFFmpegArgs, cmp.Or(o.ExtraFFmpegArgs, m.ExtraFFmpegArgs, configString(keys.MExtraFFmpegArgs)))
	add(metkeys.TranscodeGPU, gpu)
	add(metkeys.TranscodeGPUNode, gpuNode)
	add(metkeys.Thumbnail, thumbnail)
	if stripThumbnail {
		args = append(args, metkeys.StripThumbnail)
	}
	addEach(metkeys.TranscodeVideoCodecs, videoCodecs)
	add(metkeys.TranscodeQuality, cmp.Or(o.TranscodeQuality, m.TranscodeQuality))
	addEach(metkeys.TranscodeAudioCodecs, audioCodecs)

	// Other
	add(metkeys.Debug, configString(keys.DebugLevel))
	// No metadata purge: Tubarr purges once it has read the file, see finishMetafile.
	return args
}

// configString returns the global config value for key if it was set, never a flag's default.
func configString(key string) string {
	if !abstractions.IsSet(key) {
		return ""
	}
	return abstractions.GetString(key)
}

// parseMetarrOutputDir parses and returns the output directory.
func parseMetarrOutputDir(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) string {
	var dir string
	name := "Metarr output directory"
	switch {
	// Download As output directory, chosen for this video.
	case v.Overrides != nil && v.Overrides.OutputDir != "":
		dir, name = v.Overrides.OutputDir, "Download As output directory"
	// #1 Priority: Explicit Viper flag set.
	case abstractions.IsSet(keys.MoveOnComplete):
		dir = abstractions.GetString(keys.MoveOnComplete)
	// #2 Priority: Move operation filter output directory.
	case v.MoveOpOutputDir != "":
		dir = v.MoveOpOutputDir
	// #3 Priority: Use the output directory stored in channel URL directly.
	case cu.ChanURLMetarrArgs.OutputDir != "":
		dir = cu.ChanURLMetarrArgs.OutputDir
	default:
		return ""
	}

	parsed, err := dirParser.ParseDirectory(dir, name)
	if err != nil {
		logger.Pl.E("Failed to parse directory %q for video with URL %q: %v", dir, v.URL, err)
		return ""
	}
	return parsed
}
