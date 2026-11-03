// Package metarr builds and runs Metarr commands.
package metarr

import (
	"cmp"
	"strconv"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/metkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// makeMetarrCommand combines arguments from both Viper config and model settings.
func makeMetarrCommand(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) []string {
	// 'Download As' overrides replace the channel URL's ops. Otherwise file ops override DB ops, then apply filtering.
	var metaOps, filenameOps []string
	if v.Overrides != nil {
		metaOps, filenameOps = overrideMetaOps(v, c), overrideFilenameOps(v, c)
	} else {
		metaOps, filenameOps = loadAndMergeMetaOps(v, cu, c, dirParser), loadAndMergeFilenameOps(v, cu, c, dirParser)
	}

	// Fill Tubarr's tags in the ops, leaving the rest for Metarr.
	metaOps = parsing.FillChannelTags(metaOps, c, cu)
	filenameOps = parsing.FillChannelTags(filenameOps, c, cu)

	// 'Download As' args, blank ones using the channel URL's, except these boxes, which replace its as ops do.
	var o models.MetarrArgs
	m := cu.ChanURLMetarrArgs
	videoCodecs := m.TranscodeVideoCodecs
	audioCodecs := m.TranscodeAudioCodecs
	videoArgs := m.TranscodeVideoArgs
	extraArgs := m.ExtraFFmpegArgs
	if v.Overrides != nil {
		o = v.Overrides.MetarrArgs
		videoCodecs = o.TranscodeVideoCodecs
		audioCodecs = o.TranscodeAudioCodecs
		videoArgs = o.TranscodeVideoArgs
		extraArgs = o.ExtraFFmpegArgs
	}
	gpu, gpuNode := cmp.Or(o.TranscodeGPU, m.TranscodeGPU), cmp.Or(o.TranscodeGPUNode, m.TranscodeGPUNode)
	if gpu == validation.GPUNone {
		gpu, gpuNode = "", ""
	}

	// 'Same as input' sends no extension, so Metarr keeps the video's.
	ext := cmp.Or(o.OutputExt, m.OutputExt)
	if ext == sharedconsts.ExtSame {
		ext = ""
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
		logger.Pl.D(3, "User set meta ops, will set meta overwrite key...")
		args = append(args, metkeys.MetaOW)
	}
	if maxCPU := cmp.Or(o.MaxCPU, m.MaxCPU); maxCPU > 0 {
		add(metkeys.MaxCPU, strconv.FormatFloat(maxCPU, 'f', 2, 64))
	}
	add(metkeys.MinFreeMem, cmp.Or(o.MinFreeMem, m.MinFreeMem))
	add(metkeys.OutputDir, parseMetarrOutputDir(v, cu, parsing.NewDirectoryParser(c, cu, parsing.AllTags)))
	add(metkeys.Ext, ext)
	add(metkeys.RenameStyle, cmp.Or(o.RenameStyle, m.RenameStyle))

	// Metarr keeps the original video unless told otherwise, and Tubarr deletes it unless 'Download As' or the channel keeps it.
	if purge := cmp.Or(o.PurgeOriginalVideo, m.PurgeOriginalVideo); purge == nil || *purge {
		args = append(args, metkeys.PurgeOriginalVideo)
	}

	// Metarr writes no NFO file unless told to. An explicit No in 'Download As' beats the channel URL's Yes.
	if writeNFO := cmp.Or(o.WriteNFO, m.WriteNFO); writeNFO != nil && *writeNFO {
		args = append(args, metkeys.WriteNFO)
	}

	// Transcoding.
	add(metkeys.ExtraFFmpegArgs, extraArgs)
	add(metkeys.TranscodeGPU, gpu)
	add(metkeys.TranscodeGPUNode, gpuNode)
	add(metkeys.Thumbnail, thumbnail)
	if stripThumbnail {
		args = append(args, metkeys.StripThumbnail)
	}
	addEach(metkeys.TranscodeVideoCodecs, videoCodecs)
	addEach(metkeys.TranscodeVideoArgs, videoArgs)
	addEach(metkeys.TranscodeAudioCodecs, audioCodecs)

	// Other.
	// (Send Tubarr's current debug level to Metarr, so it can log more verbosely if needed).
	if dLevel := logging.Level(); dLevel > 0 {
		add(metkeys.Debug, strconv.Itoa(dLevel))
	}

	// No metadata purge: Tubarr purges once it has read the file, see finishMetafile.
	return args
}

// // configString returns the global config value for key if it was set, never a flag's default.
// func configString(key string) string {
// 	if !abstractions.IsSet(key) {
// 		return ""
// 	}
// 	return abstractions.GetString(key)
// }

// parseMetarrOutputDir parses and returns the output directory.
func parseMetarrOutputDir(v *models.Video, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) string {
	var dir string
	name := "Metarr output directory"
	switch {
	// Download As output directory, chosen for this video.
	case v.Overrides != nil && v.Overrides.OutputDir != "":
		dir, name = v.Overrides.OutputDir, "'Download As...' output directory"
	// #1 Priority: Move operation filter output directory.
	case v.MoveOpOutputDir != "":
		dir = v.MoveOpOutputDir
	// #2 Priority: Use the output directory stored in channel URL directly.
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
