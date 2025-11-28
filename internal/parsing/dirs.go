// Package parsing performs parsing operations such as parsing template directives, and URLs.
package parsing

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtemplates"
)

const (
	openTemplate  = "{{"
	closeTemplate = "}}"
	avgReplaceLen = 32
	templateLen   = len(openTemplate) + len(closeTemplate) + 4
)

// DirectoryParser holds a channel model, used for filling some template tags.
type DirectoryParser struct {
	C       *models.Channel
	Tagging TubarrOrMetarrTags
}

type TubarrOrMetarrTags int

const (
	TubarrTags TubarrOrMetarrTags = iota
	AllTags
)

// NewDirectoryParser returns a directory parser model.
func NewDirectoryParser(c *models.Channel, tagType TubarrOrMetarrTags) (parseDir *DirectoryParser) {
	return &DirectoryParser{
		C:       c,
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

	if !filepath.IsAbs(parsed) {
		if parsed, err = filepath.Abs(parsed); err != nil {
			return parsed, err
		}
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
		if _, ok := sharedtemplates.MetarrTemplateTags[tag]; ok && dp.Tagging == TubarrTags {
			return "", fmt.Errorf("cannot use Metarr tags for %q. Please avoid %v", dir, sharedtemplates.MetarrTemplateTags)
		}

		// Replacement string
		replacement, err := dp.replaceTemplateTags(strings.TrimSpace(tag))
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

// replaceTemplateTags makes template replacements in the directory string.
func (dp *DirectoryParser) replaceTemplateTags(tag string) (string, error) {
	c := dp.C
	tag = strings.ToLower(tag)

	switch tag {
	case sharedtemplates.ChannelID:
		if c != nil && c.ID != 0 {
			return strconv.Itoa(int(c.ID)), nil
		}
		return "", errors.New("templating: channel ID is 0")

	case sharedtemplates.ChannelName:
		if c != nil && c.Name != "" {
			return c.Name, nil
		}
		return "", errors.New("templating: channel name empty")

	default:
		if _, ok := sharedtemplates.MetarrTemplateTags[tag]; ok {
			if _, err := exec.LookPath("metarr"); err != nil {
				return "", fmt.Errorf("templating: tag %q detected as metarr tag but metarr $PATH not valid: %v", tag, err)
			}
			return "{{" + tag + "}}", nil
		}
		return "", fmt.Errorf("tag %q detected as invalid", tag)
	}
}
