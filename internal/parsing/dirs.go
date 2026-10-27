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

	// Abs also cleans, collapsing a "//" left by {{output_directory}}'s trailing slash.
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
	opens := strings.Count(dir, openTemplate)
	closes := strings.Count(dir, closeTemplate)

	if opens != closes {
		return "", fmt.Errorf("mismatched template delimiters: %d opens, %d closes", opens, closes)
	}

	var b strings.Builder
	b.Grow(len(dir) - (opens * templateLen) + (opens * avgReplaceLen)) // Approximate size
	remaining := dir

	for range opens {
		startIdx := strings.Index(remaining, openTemplate)
		if startIdx == -1 {
			return "", errors.New("missing opening delimiter")
		}

		endIdx := strings.Index(remaining, closeTemplate)
		if endIdx == -1 {
			return "", errors.New("missing closing delimiter")
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

// FillOutputDirectory replaces each {{output_directory}} tag in a move op's dir with outputDir, reporting whether it had one.
func FillOutputDirectory(dir, outputDir string) (filled string, hasTag bool) {
	var b strings.Builder
	remaining := dir

	for {
		start := strings.Index(remaining, openTemplate)
		if start == -1 {
			break
		}
		end := strings.Index(remaining[start:], closeTemplate)
		if end == -1 {
			break
		}
		end += start

		// Swap the output directory tag, keeping any other tag for ParseDirectory.
		b.WriteString(remaining[:start])
		if name, _ := sharedtemplates.ParseTag(remaining[start+len(openTemplate) : end]); name == sharedtemplates.OutputDirectory {
			b.WriteString(outputDir)
			hasTag = true
		} else {
			b.WriteString(remaining[start : end+len(closeTemplate)])
		}
		remaining = remaining[end+len(closeTemplate):]
	}

	b.WriteString(remaining)
	return b.String(), hasTag
}

// replaceTemplateTags makes template replacements in the directory string.
func (dp *DirectoryParser) replaceTemplateTags(tag string) (string, error) {
	name, directive := sharedtemplates.ParseTag(tag)
	if directive != "" && !sharedtemplates.ValidTag(tag, sharedtemplates.AllTemplatesMap) {
		return "", fmt.Errorf("tag %q detected as invalid, supported tags: %v", tag, sharedtemplates.DescribeTags(sharedtemplates.AllTemplatesMap))
	}

	// Metarr tags are left for Metarr to fill.
	if _, ok := sharedtemplates.MetarrTemplateTags[name]; ok {
		if _, err := exec.LookPath("metarr"); err != nil {
			return "", fmt.Errorf("templating: tag %q detected as metarr tag but metarr $PATH not valid: %w", tag, err)
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

	default:
		return "", fmt.Errorf("tag %q detected as invalid", name)
	}
	return value, nil
}
