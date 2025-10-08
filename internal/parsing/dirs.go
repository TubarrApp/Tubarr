// Package parsing performs parsing operations such as parsing template directives, and URLs.
package parsing

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"tubarr/internal/domain/templates"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

const (
	openTemplate  = "{{"
	closeTemplate = "}}"
	avgReplaceLen = 32
	templateLen   = len(openTemplate) + len(closeTemplate) + 4
)

// DirectoryParser holds a channel model, used for filling some template tags.
type DirectoryParser struct {
	C *models.Channel
}

// NewDirectoryParser returns a directory parser model.
func NewDirectoryParser(c *models.Channel) (parseDir *DirectoryParser) {
	return &DirectoryParser{
		C: c,
	}
}

// ParseDirectory returns the absolute directory path with template replacements.
func (dp *DirectoryParser) ParseDirectory(dir string, v *models.Video, fileType string) (parsedDir string, err error) {
	if dir == "" {
		return "", errors.New("directory sent in empty")
	}

	parsed := dir
	if strings.Contains(dir, openTemplate) {
		var err error

		parsed, err = dp.parseTemplate(dir, v)
		if err != nil {
			return "", fmt.Errorf("template parsing error: %w", err)
		}
	}

	if !filepath.IsAbs(parsed) {
		if parsed, err = filepath.Abs(parsed); err != nil {
			return parsed, err
		}
	}

	logging.S(1, "Parsed %s file output directory for video %q as %q", fileType, v.URL, parsed)
	return parsed, nil
}

// parseTemplate parses template options inside the directory string.
//
// Returns error if the desired data isn't present, to prevent unexpected results for the user.
func (dp *DirectoryParser) parseTemplate(dir string, v *models.Video) (string, error) {
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

		// Replacement string
		tag := remaining[startIdx+len(openTemplate) : endIdx]
		replacement, err := dp.replaceTemplateTags(strings.TrimSpace(tag), v)
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
func (dp *DirectoryParser) replaceTemplateTags(tag string, v *models.Video) (string, error) {
	c := dp.C

	switch strings.ToLower(tag) {

	case templates.ChannelID:
		if c.ID != 0 {
			return strconv.Itoa(int(c.ID)), nil
		}
		return "", errors.New("templating: channel ID is 0")

	case templates.ChannelName:
		if c.Name != "" {
			return c.Name, nil
		}
		return "", errors.New("templating: channel name empty")

	case templates.VideoID:
		if v.ID != 0 {
			return strconv.Itoa(int(v.ID)), nil
		}
		return "", errors.New("templating: video ID is 0")

	case templates.VideoTitle:
		if v.Title != "" {
			return v.Title, nil
		}
		return "", errors.New("templating: video title is empty")

	case templates.VideoURL:
		if v.URL != "" {
			return v.URL, nil
		}
		return "", errors.New("templating: video URL is empty")

		// Metarr cases:
	case templates.MetAuthor, templates.MetDay, templates.MetDirector,
		templates.MetDomain, templates.MetMonth, templates.MetYear:

		if _, err := exec.LookPath("metarr"); err != nil {
			return "", fmt.Errorf("templating: tag %q detected as metarr tag but metarr $PATH not found", tag)
		}
		return "{{" + tag + "}}", nil

	default:
		return "", fmt.Errorf("tag %q detected as invalid", tag)
	}
}
