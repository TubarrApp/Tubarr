package cfg

import (
	"errors"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

type cobraMetarrArgs struct {
	filenameReplaceSfx []string
	renameStyle        string
	fileDatePfx        string
	metarrExt          string
	metaOps            []string
	outputDir          string
	concurrency        int
	maxCPU             float64
	minFreeMem         string
}

// getMetarrArgFns gets and collects the Metarr argument functions for channel updates.
func getMetarrArgFns(c cobraMetarrArgs) (fns []func(*models.MetarrArgs) error, err error) {
	if c.minFreeMem != "" {
		if err := verifyMinFreeMem(c.minFreeMem); err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MinFreeMem = c.minFreeMem
			return nil
		})
	}

	if c.metarrExt != "" {
		if verifyOutputFiletype(c.metarrExt) {
			fns = append(fns, func(m *models.MetarrArgs) error {
				m.Ext = c.metarrExt
				return nil
			})
		}
	}

	if c.renameStyle != "" {
		if err := validateRenameFlag(c.renameStyle); err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.RenameStyle = c.renameStyle
			return nil
		})
	}

	if c.fileDatePfx != "" {
		if !dateFormat(c.fileDatePfx) {
			return nil, errors.New("invalid Metarr filename date tag format")
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FileDatePfx = c.fileDatePfx
			return nil
		})
	}

	if len(c.filenameReplaceSfx) != 0 {
		valid, err := validateFilenameSuffixReplace(c.filenameReplaceSfx)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameReplaceSfx = valid
			return nil
		})
	}

	if c.outputDir != "" {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.OutputDir = c.outputDir
			return nil
		})
	}

	if len(c.metaOps) > 0 {
		valid, err := validateMetaOps(c.metaOps)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOps = valid
			return nil
		})
	}

	return fns, nil
}

type chanSettings struct {
	cookieSource           string
	crawlFreq              int
	filters                []string
	retries                int
	externalDownloader     string
	externalDownloaderArgs string
	concurrency            int
	maxFilesize            string
}

func getSettingsArgFns(c chanSettings) (fns []func(m *models.ChannelSettings) error, err error) {

	if c.concurrency > 0 {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Concurrency = c.concurrency
			return nil
		})
	}

	if c.cookieSource != "" {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.CookieSource = c.cookieSource
			return nil
		})
	}

	if c.crawlFreq > 0 {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.CrawlFreq = c.crawlFreq
			return nil
		})
	}

	if c.externalDownloader != "" {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ExternalDownloader = c.externalDownloader
			return nil
		})
	}

	if c.externalDownloaderArgs != "" {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ExternalDownloaderArgs = c.externalDownloaderArgs
			return nil
		})
	}

	if c.retries > 0 {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Retries = c.retries
			return nil
		})
	}

	if c.maxFilesize != "" {
		c.maxFilesize, err = validateMaxFilesize(c.maxFilesize)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.MaxFilesize = c.maxFilesize
			return nil
		})
	}

	if len(c.filters) > 0 {
		dlFilters, err := verifyChannelOps(c.filters)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Filters = dlFilters
			return nil
		})
	}

	return fns, nil
}

func validateMaxFilesize(m string) (string, error) {
	m = strings.ToUpper(m)
	switch {
	case strings.HasSuffix(m, "B"), strings.HasSuffix(m, "K"), strings.HasSuffix(m, "M"), strings.HasSuffix(m, "G"):
		return strings.TrimSuffix(m, "B"), nil
	default:
		if _, err := strconv.Atoi(m); err != nil {
			return "", err
		}
	}
	return m, nil
}

// getKeyVal returns a key and value for channel lookup.
func getChanKeyVal(id int, name, url string) (key, val string, err error) {
	switch {
	case id != 0:
		key = consts.QChanID
		val = strconv.Itoa(id)
	case url != "":
		key = consts.QChanURL
		val = url
	case name != "":
		key = consts.QChanName
		val = name
	default:
		return "", "", errors.New("please enter either a channel ID, name, or URL")
	}
	return key, val, nil
}
