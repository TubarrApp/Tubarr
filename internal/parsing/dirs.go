// Package parsing performs parsing operations such as parsing template directives, and URLs.
package parsing

import (
	"cmp"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/TubarrApp/gocommon/sharedtemplates"
)

const (
	openTemplate  = "{{"
	closeTemplate = "}}"
	avgReplaceLen = 32
	templateLen   = len(openTemplate) + len(closeTemplate) + 4
)

// DirectoryParser holds the channel and channel URL, used for filling channel template tags.
type DirectoryParser struct {
	C       *models.Channel
	CU      *models.ChannelURL
	Tagging TubarrOrMetarrTags
}

// TubarrOrMetarrTags determines the desired tag type.
type TubarrOrMetarrTags int

// TubarrOrMetarrTags enums.
const (
	TubarrTags TubarrOrMetarrTags = iota
	AllTags
)

// NewDirectoryParser returns a directory parser model.
func NewDirectoryParser(c *models.Channel, cu *models.ChannelURL, tagType TubarrOrMetarrTags) (parseDir *DirectoryParser) {
	return &DirectoryParser{
		C:       c,
		CU:      cu,
		Tagging: tagType,
	}
}

// ParseDirectory returns the absolute directory path with template replacements.
func (dp *DirectoryParser) ParseDirectory(dir string, fileType string) (parsedDir string, err error) {
	if dir == "" {
		return "", errors.New("directory sent in empty")
	}

	parsed := dir
	if strings.Contains(dir, openTemplate) {
		var err error

		parsed, err = dp.parseTemplate(dir)
		if err != nil {
			return "", fmt.Errorf("template parsing error: %w", err)
		}
	}

	// Abs also cleans, collapsing a "//" left by {{channel_url_output_directory}}'s trailing slash.
	if parsed, err = filepath.Abs(parsed); err != nil {
		return parsed, err
	}

	logger.Pl.I("Parsed %s file output directory as %q", fileType, parsed)
	return parsed, nil
}

// parseTemplate parses template options inside the directory string.
//
// Returns error if the desired data isn't present, to prevent unexpected results for the user.
func (dp *DirectoryParser) parseTemplate(dir string) (string, error) {
	if dp == nil {
		return dir, fmt.Errorf("directory parser sent in nil, not parsing template for %q", dir)
	}

	// Count opens and closes (number of likely template elements.)
	opens := strings.Count(dir, openTemplate)
	closes := strings.Count(dir, closeTemplate)

	// Mismatch in 'opens' and 'closes', likely syntax mistake.
	if opens != closes {
		return "", fmt.Errorf("mismatched template delimiters: %d opens, %d closes", opens, closes)
	}

	var b strings.Builder
	b.Grow(len(dir) - (opens * templateLen) + (opens * avgReplaceLen)) // Approximate size
	remaining := dir

	for range opens {
		// Retrieve index of {{ and }}
		startIdx := strings.Index(remaining, openTemplate)
		if startIdx == -1 {
			return "", errors.New("missing opening delimiter")
		}
		endIdx := strings.Index(remaining, closeTemplate)
		if endIdx == -1 {
			return "", errors.New("missing closing delimiter")
		}
		if endIdx < startIdx {
			return "", fmt.Errorf("closing delimiter comes before the opening delimiter in %q", dir)
		}

		// String up to template open
		b.WriteString(remaining[:startIdx])

		// Get tag part, check tag is not invalid.
		tag := remaining[startIdx+len(openTemplate) : endIdx]
		if name, _ := sharedtemplates.ParseTag(tag); dp.Tagging == TubarrTags {
			if _, ok := sharedtemplates.MetarrTemplateTags[name]; ok {
				return "", fmt.Errorf("cannot use Metarr tags for %q. Please avoid %v", dir, sharedtemplates.DescribeTags(sharedtemplates.MetarrTemplateTags))
			}
		}

		// Replacement string
		replacement, err := dp.replaceTemplateTags(tag)
		if err != nil {
			return "", err
		}
		b.WriteString(replacement)

		// String after template close
		remaining = remaining[endIdx+len(closeTemplate):]
	}

	// Write any remaining text after last template
	b.WriteString(remaining)

	return b.String(), nil
}

// FillChannelOutputDirectory fills {{channel_output_directory}} in a merged channel's Metarr output directory from its video directory.
func FillChannelOutputDirectory(c *models.Channel) {
	s := c.ChanSettings
	m := c.ChanMetarrArgs
	if s == nil || m == nil {
		return
	}
	m.OutputDir, _ = replaceOutputDirectoryTags(m.OutputDir, s.VideoDir, "")
}

// FillChannelURLOutputDirectory fills the output directory tags in a merged channel URL's Metarr output directory.
func FillChannelURLOutputDirectory(cu *models.ChannelURL, c *models.Channel) {
	s := cu.ChanURLSettings
	m := cu.ChanURLMetarrArgs
	if s == nil || m == nil {
		return
	}
	m.OutputDir, _ = replaceOutputDirectoryTags(m.OutputDir, outputOrVideoDir(c.ChanMetarrArgs, c.ChanSettings), s.VideoDir)
}

// FillOutputDirectoryTags fills a move op or Download As directory, reporting whether every output directory tag in it had a directory to fill from.
func FillOutputDirectoryTags(dir string, cu *models.ChannelURL, c *models.Channel) (filled string, allFilled bool) {
	var channelDir, channelURLDir string
	if c != nil {
		channelDir = outputOrVideoDir(c.ChanMetarrArgs, c.ChanSettings)
	}
	if cu != nil {
		channelURLDir = outputOrVideoDir(cu.ChanURLMetarrArgs, cu.ChanURLSettings)
	}
	return replaceOutputDirectoryTags(dir, channelDir, channelURLDir)
}

// outputOrVideoDir returns the Metarr output directory if there is one, else the video directory.
func outputOrVideoDir(m *models.MetarrArgs, s *models.Settings) string {
	var outputDir, videoDir string
	if m != nil {
		outputDir = m.OutputDir
	}
	if s != nil {
		videoDir = s.VideoDir
	}
	return cmp.Or(outputDir, videoDir)
}

// replaceOutputDirectoryTags swaps each output directory tag in dir for its directory, reporting false if any had none and stayed in place.
func replaceOutputDirectoryTags(dir, channelDir, channelURLDir string) (filled string, allFilled bool) {
	var b strings.Builder
	allFilled = true

	for {
		before, remaining, found := strings.Cut(dir, openTemplate)
		b.WriteString(before)
		if !found {
			break
		}

		tag, after, found := strings.Cut(remaining, closeTemplate)
		if !found {
			b.WriteString(openTemplate)
			b.WriteString(remaining)
			break
		}

		name, _ := sharedtemplates.ParseTag(tag)
		switch {
		case name == sharedtemplates.ChannelOutputDirectory && channelDir != "":
			b.WriteString(channelDir)

		case name == sharedtemplates.ChannelURLOutputDirectory && channelURLDir != "":
			b.WriteString(channelURLDir)

		default:
			if name == sharedtemplates.ChannelOutputDirectory || name == sharedtemplates.ChannelURLOutputDirectory {
				allFilled = false
			}
			b.WriteString(openTemplate)
			b.WriteString(tag)
			b.WriteString(closeTemplate)
		}
		dir = after
	}

	return b.String(), allFilled
}

// replaceTemplateTags makes template replacements in the directory string.
func (dp *DirectoryParser) replaceTemplateTags(tag string) (string, error) {
	name, directive := sharedtemplates.ParseTag(tag)
	if directive != "" && !sharedtemplates.ValidTag(tag, sharedtemplates.AllTemplatesMap) {
		return "", fmt.Errorf("tag %q detected as invalid, supported tags: %v", tag, sharedtemplates.DescribeTags(sharedtemplates.AllTemplatesMap))
	}

	// Metarr tags are left for Metarr to fill.
	if _, ok := sharedtemplates.MetarrTemplateTags[name]; ok {
		if _, err := exec.LookPath(sharedconsts.ProgramMetarr); err != nil {
			return "", fmt.Errorf("templating: tag %q detected as Metarr tag but Metarr $PATH not valid: %w", tag, err)
		}
		if directive != "" {
			name += ":" + directive
		}
		return "{{" + name + "}}", nil
	}

	value, err := channelTagValue(name, dp.C, dp.CU)
	if err != nil {
		return "", err
	}

	// One directory level, e.g. "AC/DC" can't nest and ".." can't climb out.
	if value = sharedparsing.SanitizePathComponent(value, sharedconsts.MaxNameBytes); value == "" {
		return "", fmt.Errorf("templating: %q is empty once made safe for a path", tag)
	}
	return value, nil
}

// channelTagValue returns the channel's value for a channel tag, e.g. its name for "channel_name".
func channelTagValue(name string, c *models.Channel, cu *models.ChannelURL) (string, error) {
	var value string
	switch name {
	case sharedtemplates.ChannelID:
		if c == nil { // Removed c.ID == 0 error. Unexpected, but 0 is a valid character for a rename.
			return "", errors.New("templating: channel sent in nil")
		}
		value = strconv.FormatInt(c.ID, 10)

	case sharedtemplates.ChannelName:
		if c == nil || c.Name == "" {
			return "", errors.New("templating: channel name empty")
		}
		value = c.Name

	case sharedtemplates.ChannelURL:
		if cu == nil || cu.URL == "" {
			return "", errors.New("templating: channel URL empty")
		}
		value = sharedparsing.StripURLScheme(cu.URL)

	// The URL's name, or the URL itself if it has none.
	case sharedtemplates.ChannelURLName:
		if cu == nil || cu.URL == "" {
			return "", errors.New("templating: channel URL empty")
		}
		value = cmp.Or(cu.Name, sharedparsing.StripURLScheme(cu.URL))

	// Only left in place when there's no directory to fill it from.
	case sharedtemplates.ChannelOutputDirectory, sharedtemplates.ChannelURLOutputDirectory:
		return "", fmt.Errorf("templating: no directory to fill %q from", name)

	default:
		return "", fmt.Errorf("tag %q detected as invalid", name)
	}
	return value, nil
}
