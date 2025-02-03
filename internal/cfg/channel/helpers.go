package cfgchannel

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	cfgvalidate "tubarr/internal/cfg/validation"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/regex"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
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
	useGPU             string
	transcodeCodec     string
	transcodeQuality   string
}

// getMetarrArgFns gets and collects the Metarr argument functions for channel updates.
func getMetarrArgFns(c cobraMetarrArgs) (fns []func(*models.MetarrArgs) error, err error) {
	if c.minFreeMem != "" {
		if err := cfgvalidate.ValidateMinFreeMem(c.minFreeMem); err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MinFreeMem = c.minFreeMem
			return nil
		})
	}

	if c.metarrExt != "" {
		if cfgvalidate.ValidateOutputFiletype(c.metarrExt) {
			fns = append(fns, func(m *models.MetarrArgs) error {
				m.Ext = c.metarrExt
				return nil
			})
		}
	}

	if c.renameStyle != "" {
		if err := cfgvalidate.ValidateRenameFlag(c.renameStyle); err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.RenameStyle = c.renameStyle
			return nil
		})
	}

	if c.fileDatePfx != "" {
		if !cfgvalidate.ValidateDateFormat(c.fileDatePfx) {
			return nil, errors.New("invalid Metarr filename date tag format")
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FileDatePfx = c.fileDatePfx
			return nil
		})
	}

	if len(c.filenameReplaceSfx) != 0 {
		valid, err := cfgvalidate.ValidateFilenameSuffixReplace(c.filenameReplaceSfx)
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
		valid, err := cfgvalidate.ValidateMetaOps(c.metaOps)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOps = valid
			return nil
		})
	}

	if c.useGPU != "" {
		validGPU, err := validateGPU(c.useGPU)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.UseGPU = validGPU
			return nil
		})
	}

	if c.transcodeCodec != "" {
		validTranscodeCodec, err := validateTranscodeCodec(c.transcodeCodec)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeCodec = validTranscodeCodec
			return nil
		})
	}

	if c.transcodeQuality != "" {
		validTranscodeQuality, err := validateTranscodeQuality(c.transcodeQuality)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeQuality = validTranscodeQuality
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

// verifyChanRowUpdateValid verifies that your update operation is valid
func verifyChanRowUpdateValid(col, val string) error {
	switch col {
	case "url", "name", "video_directory", "json_directory":
		if val == "" {
			return fmt.Errorf("cannot set %s blank, please use the 'delete' function if you want to remove this channel entirely", col)
		}
	default:
		return errors.New("cannot set a custom value for internal DB elements")
	}
	return nil
}

// verifyChannelOps verifies that the user inputted filters are valid
func verifyChannelOps(ops []string) ([]models.DLFilters, error) {

	var filters = make([]models.DLFilters, 0, len(ops))
	for _, op := range ops {
		split := strings.Split(op, ":")
		if len(split) < 3 {
			return nil, errors.New("please enter filters in the format 'field:filter_type:value' (e.g. 'title:omit:frogs' ignores videos with frogs in the metatitle)")
		}
		switch len(split) {
		case 3:
			switch split[1] {
			case "contains", "omit":
				filters = append(filters, models.DLFilters{
					Field: split[0],
					Type:  split[1],
					Value: split[2],
				})
			default:
				return nil, errors.New("please enter a filter type of either 'contains' or 'omit'")
			}
		case 2:
			switch split[1] {
			case "contains", "omit":
				filters = append(filters, models.DLFilters{
					Field: split[0],
					Type:  split[1],
				})
			default:
				return nil, errors.New("please enter a filter type of either 'contains' or 'omit'")
			}
		default:
			return nil, errors.New("invalid filter. Valid examples: 'title:contains:frogs','date:omit' (contains only metatitles with frogs, and omits downloads including a date metafield)")

		}
	}
	return filters, nil
}

// validateFromToDate validates a date string in yyyymmdd or formatted like "2025y12m31d".
func validateFromToDate(d string) (string, error) {
	d = strings.ToLower(d)
	d = strings.ReplaceAll(d, "-", "")
	d = strings.ReplaceAll(d, " ", "")

	// Handle "today" special case
	if d == "today" {
		return time.Now().Format("20060102"), nil
	}

	// Regex to extract explicitly marked years, months, and days
	re := regex.YearFragmentsCompile()
	matches := re.FindStringSubmatch(d)

	// Default values
	year := strconv.Itoa(time.Now().Year())
	month := "01"
	day := "01"

	// Year
	if matches[1] != "" {
		year = matches[1]
	} else if len(d) == 8 && !strings.ContainsAny(d, "ymd") { // No markers, assume raw format
		year, month, day = d[:4], d[4:6], d[6:8]
	}

	// Month
	if matches[2] != "" {
		if len(matches[2]) == 1 {
			month = "0" + matches[2] // Pad single-digit months
		} else {
			month = matches[2]
		}
	}

	// Day
	if matches[3] != "" {
		if len(matches[3]) == 1 {
			day = "0" + matches[3] // Pad single-digit days
		} else {
			day = matches[3]
		}
	}

	// Validate ranges
	yearInt, _ := strconv.Atoi(year)
	monthInt, _ := strconv.Atoi(month)
	dayInt, _ := strconv.Atoi(day)

	if yearInt < 1000 || yearInt > 9999 {
		return "", fmt.Errorf("invalid year in yyyy-mm-dd date %q: year must be 4 digits", d)
	}
	if monthInt < 1 || monthInt > 12 {
		return "", fmt.Errorf("invalid month in yyyy-mm-dd date %q: month must be between 01-12", d)
	}
	if dayInt < 1 || dayInt > 31 {
		return "", fmt.Errorf("invalid day in yyyy-mm-dd date %q: day must be between 01-31", d)
	}

	output := year + month + day
	logging.D(1, "Made from/to date %q", output)

	return output, nil
}

// validateGPU validates the user input GPU selection.
func validateGPU(g string) (gpu string, err error) {
	g = strings.ToLower(g)
	switch g {
	case "qsv", "intel":
		return "qsv", nil
	case "amd", "radeon", "vaapi":
		return "vaapi", nil
	case "nvidia", "cuda":
		return "cuda", nil
	default:
		return "", fmt.Errorf("entered gpu %q not supported. Tubarr supports Intel, AMD, or Nvidia", g)
	}
}

// validateTranscodeCodec validates the user input codec selection.
func validateTranscodeCodec(c string) (codec string, err error) {
	c = strings.ToLower(c)
	c = strings.ReplaceAll(c, ".", "")
	switch c {
	case "h264", "hevc":
		return c, nil
	case "h265":
		return "hevc", nil
	default:
		return "", fmt.Errorf("entered codec %q not supported. Tubarr supports h264 and HEVC (h265)", c)
	}
}

// validateTranscodeQuality validates the transcode quality preset.
func validateTranscodeQuality(q string) (quality string, err error) {
	q = strings.ToLower(q)
	q = strings.ReplaceAll(q, " ", "")

	switch q {
	case "p1", "p2", "p3", "p4", "p5", "p6", "p7":
		logging.I("Got transcode quality profile %q", q)
		return q, nil
	}

	qNum, err := strconv.Atoi(q)
	if err != nil {
		return "", fmt.Errorf("input should be p1 to p7, validation of transcoder quality failed")
	}

	var qualProf string
	switch {
	case qNum < 0:
		qualProf = "p1"
	case qNum > 7:
		qualProf = "p7"
	default:
		qualProf = "p" + strconv.Itoa(qNum)
	}
	logging.I("Got transcode quality profile %q", qualProf)
	return qualProf, nil
}
