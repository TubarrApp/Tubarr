package metarr

import (
	"fmt"
	"slices"
	"strings"
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// ChannelURLOverrideLines returns the directories and ops a channel URL's videos use, as the defaults for a video's
// Download As overrides. Filtered ops are listed with their filters, as matching needs metadata, and the output
// directory leaves out move ops for the same reason.
func ChannelURLOverrideLines(cu *models.ChannelURL, dirParser *parsing.DirectoryParser) (lines models.OverrideLines, err error) {
	// Channel URL ops from every level, as Metarr is given them.
	metaOps, err := channelURLMetaOps(cu, dirParser)
	if err != nil {
		return lines, err
	}
	filenameOps, err := channelURLFilenameOps(cu, dirParser)
	if err != nil {
		return lines, err
	}
	filteredMetaOps, err := metadata.ChannelURLFilteredMetaOps(cu, dirParser)
	if err != nil {
		return lines, err
	}
	filteredFilenameOps, err := metadata.ChannelURLFilteredFilenameOps(cu, dirParser)
	if err != nil {
		return lines, err
	}

	return models.OverrideLines{
		VideoDir:            cu.ChanURLSettings.VideoDir,
		JSONDir:             cu.ChanURLSettings.JSONDir,
		OutputDir:           cu.ChanURLMetarrArgs.OutputDir,
		MetaOps:             strings.Join(getDedupedMetaOpStrings(metaOps), "\n"),
		FilteredMetaOps:     strings.Join(filteredMetaOpLines(slices.Concat(filteredMetaOps...)), "\n"),
		FilenameOps:         strings.Join(getDedupedFilenameOpStrings(filenameOps), "\n"),
		FilteredFilenameOps: strings.Join(filteredFilenameOpLines(slices.Concat(filteredFilenameOps...)), "\n"),
	}, nil
}

// ChannelURLOpLines returns the channel URL's ops as the Download As dialog's op boxes are filled with them.
func ChannelURLOpLines(cu *models.ChannelURL, dirParser *parsing.DirectoryParser) (*models.OpLines, error) {
	lines, err := ChannelURLOverrideLines(cu, dirParser)
	if err != nil {
		return nil, err
	}
	return &models.OpLines{
		MetaOps:             lines.MetaOps,
		FilteredMetaOps:     lines.FilteredMetaOps,
		FilenameOps:         lines.FilenameOps,
		FilteredFilenameOps: lines.FilteredFilenameOps,
	}, nil
}

// FollowChannelURLOps refreshes the channel URL's lines in a video's saved ops, from its ops and ops files as they are
// now, keeping the lines added or deleted for the video.
func FollowChannelURLOps(o *models.VideoOverrides, cu *models.ChannelURL, dirParser *parsing.DirectoryParser) error {
	if o == nil || o.ChannelURLOps == nil {
		return nil
	}
	now, err := ChannelURLOpLines(cu, dirParser)
	if err != nil {
		return err
	}
	saved := VideoOverrideLines(o)
	was := o.ChannelURLOps

	// Each box, parsed as the dialog's are.
	if o.MetaOps, err = parsing.ParseMetaOps(followedLines(saved.MetaOps, was.MetaOps, now.MetaOps)); err != nil {
		return fmt.Errorf("invalid meta ops: %w", err)
	}
	if o.FilteredMetaOps, err = parsing.ParseFilteredMetaOps(followedLines(saved.FilteredMetaOps, was.FilteredMetaOps, now.FilteredMetaOps)); err != nil {
		return fmt.Errorf("invalid filtered meta ops: %w", err)
	}
	if o.FilenameOps, err = parsing.ParseFilenameOps(followedLines(saved.FilenameOps, was.FilenameOps, now.FilenameOps)); err != nil {
		return fmt.Errorf("invalid filename ops: %w", err)
	}
	if o.FilteredFilenameOps, err = parsing.ParseFilteredFilenameOps(followedLines(saved.FilteredFilenameOps, was.FilteredFilenameOps, now.FilteredFilenameOps)); err != nil {
		return fmt.Errorf("invalid filtered filename ops: %w", err)
	}

	// The ops now go by the channel URL's current ones.
	o.ChannelURLOps = now
	return nil
}

// followedLines returns a box's saved lines with the channel URL's old ones swapped for its current ones, keeping the
// lines added or deleted for the video.
func followedLines(saved, was, now string) []string {
	// A box left unchanged is the channel URL's, in its order.
	if saved == was {
		return opLines(now)
	}
	wasLines := opLines(was)
	nowLines := opLines(now)

	// The channel URL's lines it no longer has go, and its new ones are added.
	lines := slices.DeleteFunc(opLines(saved), func(line string) bool {
		return slices.Contains(wasLines, line) && !slices.Contains(nowLines, line)
	})
	for _, line := range nowLines {
		if !slices.Contains(wasLines, line) && !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	return lines
}

// opLines splits an op box into its lines.
func opLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// FileOverrideLines returns the ops in the file at path for the Download As dialog's box (e.g. "meta_ops").
func FileOverrideLines(box, path string, dirParser *parsing.DirectoryParser) ([]string, error) {
	switch box {
	case "meta_ops":
		ops, err := LoadMetaOpsFromFile(path, dirParser)
		return getDedupedMetaOpStrings(ops), err
	case "filtered_meta_ops":
		ops, err := metadata.LoadFilteredMetaOpsFromFile(path, dirParser)
		return filteredMetaOpLines(ops), err
	case "filename_ops":
		ops, err := LoadFilenameOpsFromFile(path, dirParser)
		return getDedupedFilenameOpStrings(ops), err
	case "filtered_filename_ops":
		ops, err := metadata.LoadFilteredFilenameOpsFromFile(path, dirParser)
		return filteredFilenameOpLines(ops), err
	}
	return nil, fmt.Errorf("unknown ops box %q", box)
}

// VideoOverrideLines returns a video's stored Download As overrides as lines, to fill the dialog with them again.
func VideoOverrideLines(o *models.VideoOverrides) models.OverrideLines {
	return models.OverrideLines{
		Filename:                  o.Filename,
		VideoDir:                  o.VideoDir,
		JSONDir:                   o.JSONDir,
		OutputDir:                 o.OutputDir,
		MetaOps:                   strings.Join(getDedupedMetaOpStrings(o.MetaOps), "\n"),
		FilteredMetaOps:           strings.Join(filteredMetaOpLines(o.FilteredMetaOps), "\n"),
		FilenameOps:               strings.Join(getDedupedFilenameOpStrings(o.FilenameOps), "\n"),
		FilteredFilenameOps:       strings.Join(filteredFilenameOpLines(o.FilteredFilenameOps), "\n"),
		Thumbnail:                 o.Thumbnail,
		StripThumbnail:            o.StripThumbnail,
		YtdlpOutputExt:            o.YtdlpOutputExt,
		MaxFilesize:               o.MaxFilesize,
		MaxResolution:             o.MaxResolution,
		ExtraYTDLPVideoArgs:       o.ExtraYTDLPVideoArgs,
		YTDLPPreferredVideoCodecs: strings.Join(o.YTDLPPreferredVideoCodecs, "\n"),
		YTDLPPreferredAudioCodecs: strings.Join(o.YTDLPPreferredAudioCodecs, "\n"),
	}
}

// filteredMetaOpLines returns the filtered meta ops as lines, one per entry with all its filters and ops.
func filteredMetaOpLines(ops []models.FilteredMetaOps) []string {
	lines := make([]string, 0, len(ops))
	for _, fmo := range ops {
		lines = append(lines, models.FilteredMetaOpsToString(fmo))
	}
	return lines
}

// filteredFilenameOpLines returns the filtered filename ops as lines, one per entry with all its filters and ops.
func filteredFilenameOpLines(ops []models.FilteredFilenameOps) []string {
	lines := make([]string, 0, len(ops))
	for _, ffo := range ops {
		lines = append(lines, models.FilteredFilenameOpsToString(ffo))
	}
	return lines
}
