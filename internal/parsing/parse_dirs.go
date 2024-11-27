// Package parsing performs parsing operations such as parsing template directives, and URLs.
package parsing

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"tubarr/internal/domain/templates"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

const (
	open          = "{{"
	close         = "}}"
	avgReplaceLen = 32
	templateLen   = len(open) + len(close) + 4
)

type Directory struct {
	C *models.Channel
	V *models.Video
}

func NewDirectoryParser(c *models.Channel, v *models.Video) (parseDir *Directory) {
	return &Directory{
		C: c,
		V: v,
	}
}

// ParseDirPtr directly modifies an input directory string.
func (dp *Directory) ParseDirPtr(input *string) error {
	if input == nil {
		return errors.New("input string is null")
	}

	var err error
	*input, err = dp.ParseDirectory(*input)
	if err != nil {
		return err
	}

	return nil
}

// ParseDirectory returns the absolute directory path with template replacements.
func (dp *Directory) ParseDirectory(dir string) (parsedDir string, err error) {
	if dir == "" {
		return "", errors.New("directory sent in empty")
	}

	parsed := dir
	if strings.Contains(dir, open) {
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

	logging.S(1, "Parsed output directory as %q", parsed)
	return parsed, nil
}

// parseTemplate parses template options inside the directory string.
//
// Returns error if the desired data isn't present, to prevent unexpected results for the user.
func (dp *Directory) parseTemplate(dir string) (string, error) {
	opens := strings.Count(dir, open)
	closes := strings.Count(dir, close)
	if opens != closes {
		return "", fmt.Errorf("mismatched template delimiters: %d opens, %d closes", opens, closes)
	}

	var b strings.Builder
	b.Grow(len(dir) - (opens * templateLen) + (opens * avgReplaceLen)) // Approximate size
	remaining := dir

	for i := 0; i < opens; i++ {
		startIdx := strings.Index(remaining, open)
		if startIdx == -1 {
			return "", errors.New("missing opening delimiter")
		}

		endIdx := strings.Index(remaining, close)
		if endIdx == -1 {
			return "", errors.New("missing closing delimiter")
		}

		// String up to template open
		b.WriteString(remaining[:startIdx])

		// Replacement string
		tag := remaining[startIdx+len(open) : endIdx]
		replacement, err := dp.replace(strings.TrimSpace(tag))
		if err != nil {
			return "", err
		}
		b.WriteString(replacement)

		// String after template close
		remaining = remaining[endIdx+len(close):]
	}

	// Write any remaining text after last template
	b.WriteString(remaining)

	return b.String(), nil
}

// replace makes template replacements in the directory string.
func (dp *Directory) replace(tag string) (string, error) {

	c := dp.C
	v := dp.V

	switch strings.ToLower(tag) {

	case templates.ChannelDomain:
		if c.URL != "" {
			u, err := url.Parse(c.URL)
			if err != nil {
				return "", fmt.Errorf("error parsing base domain from %q", c.URL)
			}
			return u.Host, nil
		}
		return "", errors.New("templating: URL empty")

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

	case templates.ChannelURL:
		if c.URL != "" {
			return c.URL, nil
		}
		return "", errors.New("templating: URL empty")

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

		// Unsupported...
	default:
		return "", fmt.Errorf("tag %q detected as invalid", tag)
	}
}
