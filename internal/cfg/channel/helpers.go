package cfgchannel

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	cfgvalidate "tubarr/internal/cfg/validation"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/regex"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
)

type cobraMetarrArgs struct {
	filenameReplaceSfx   []string
	renameStyle          string
	fileDatePfx          string
	metarrExt            string
	metaOps              []string
	outputDir            string
	concurrency          int
	maxCPU               float64
	minFreeMem           string
	useGPU               string
	gpuDir               string
	transcodeCodec       string
	transcodeAudioCodec  string
	transcodeQuality     string
	transcodeVideoFilter string
}

// getMetarrArgFns gets and collects the Metarr argument functions for channel updates.
func getMetarrArgFns(cmd *cobra.Command, c cobraMetarrArgs) (fns []func(*models.MetarrArgs) error, err error) {
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
			c.metarrExt = strings.ToLower(c.metarrExt)
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

	if c.useGPU != "" || cmd.Flags().Changed(keys.TranscodeGPU) {
		validGPU, _, err := validateGPU(c.useGPU, c.gpuDir)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.UseGPU = validGPU
			return nil
		})
	}

	if c.transcodeCodec != "" || cmd.Flags().Changed(keys.TranscodeCodec) {
		validTranscodeCodec, err := validateTranscodeCodec(c.transcodeCodec, c.useGPU)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeCodec = validTranscodeCodec
			return nil
		})
	}

	if c.transcodeAudioCodec != "" || cmd.Flags().Changed(keys.TranscodeAudioCodec) {
		validTranscodeAudioCodec, err := validateTranscodeAudioCodec(c.transcodeAudioCodec)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeAudioCodec = validTranscodeAudioCodec
			return nil
		})
	}

	if c.transcodeQuality != "" || cmd.Flags().Changed(keys.TranscodeQuality) {
		validTranscodeQuality, err := validateTranscodeQuality(c.transcodeQuality)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeQuality = validTranscodeQuality
			return nil
		})
	}

	if c.transcodeVideoFilter != "" {
		validTranscodeVideoFilter, err := validateTranscodeVideoFilter(c.transcodeVideoFilter)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeVideoFilter = validTranscodeVideoFilter
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
	fromDate               string
	toDate                 string
	outputExt              string
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

	if c.fromDate != "" {
		validFromDate, err := validateToFromDate(c.fromDate)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.FromDate = validFromDate
			return nil
		})
	}

	if c.toDate != "" {
		validToDate, err := validateToFromDate(c.toDate)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ToDate = validToDate
			return nil
		})
	}

	if c.outputExt != "" {
		c.outputExt = strings.ToLower(c.outputExt)
		err := validateOutputExtension(c.outputExt)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.OutputExt = c.outputExt
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
func getChanKeyVal(id int, name string) (key, val string, err error) {
	switch {
	case id != 0:
		key = consts.QChanID
		val = strconv.Itoa(id)
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
	case "name", "video_directory", "json_directory":
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

// validateToFromDate validates a date string in yyyymmdd or formatted like "2025y12m31d".
func validateToFromDate(d string) (string, error) {
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

// verifyTranscodeAudioCodec verifies the audio codec to use for transcode/encode operations.
func validateTranscodeAudioCodec(a string) (audioCodec string, err error) {
	a = strings.ToLower(a)
	switch a {
	case "aac", "copy":
		return a, nil
	case "":
		return "", nil
	default:
		return "", fmt.Errorf("audio codec flag %q is not currently implemented in this program, aborting", a)
	}
}

// validateGPU validates the user input GPU selection.
func validateGPU(g, devDir string) (gpu, gpuDir string, err error) {
	g = strings.ToLower(g)
	switch g {
	case "qsv", "intel":
		return "qsv", devDir, nil
	case "amd", "radeon", "vaapi":

		_, err := os.Stat(devDir)
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("driver location %q does not appear to exist?", devDir)
		}

		return "vaapi", devDir, nil
	case "nvidia", "cuda":
		return "cuda", devDir, nil
	case "auto", "automatic", "automated":
		return "auto", devDir, nil
	case "":
		return "", "", nil
	default:
		return "", devDir, fmt.Errorf("entered GPU %q not supported. Tubarr supports Auto, Intel, AMD, or Nvidia", g)
	}
}

// validateOutputExtension validates that the output extension is valid.
func validateOutputExtension(e string) error {
	e = strings.ToLower(e)
	switch e {
	case "avi", "flv", "mkv", "mov", "mp4", "webm":
		return nil
	default:
		return fmt.Errorf("output extension %v is invalid or not supported", e)
	}
}

// validateTranscodeCodec validates the user input codec selection.
func validateTranscodeCodec(c string, accel string) (codec string, err error) {
	c = strings.ToLower(c)
	c = strings.ReplaceAll(c, ".", "")
	switch c {
	case "h264", "hevc":
		return c, nil
	case "h265":
		return "hevc", nil
	case "":
		if accel == "" {
			return "", nil
		} else {
			return "", fmt.Errorf("entered codec %q not supported with acceleration type %q. Tubarr supports h264 and HEVC (h265)", c, accel)
		}
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

// hyphenateYyyyMmDd simply hyphenates yyyy-mm-dd date values for display.
func hyphenateYyyyMmDd(d string) string {
	d = strings.ReplaceAll(d, " ", "")
	d = strings.ReplaceAll(d, "-", "")

	if len(d) < 8 {
		return d
	}

	b := strings.Builder{}
	b.Grow(10)

	b.WriteString(d[0:4])
	b.WriteByte('-')
	b.WriteString(d[4:6])
	b.WriteByte('-')
	b.WriteString(d[6:8])

	return b.String()
}

// validateTranscodeVideoFilter validates the transcode video filter preset.
func validateTranscodeVideoFilter(q string) (vf string, err error) {
	return q, nil
}

// parseAuthDetails parses authorization details for a particular channel URL.
func parseAuthDetails(usernames, passwords, loginURLs []string) (map[string]*models.ChanURLAuthDetails, error) {
	logging.I("Parsing authorization details...")
	if usernames == nil && passwords == nil && loginURLs == nil {
		logging.I("No authorization details to parse...")
		return nil, nil
	}

	// Initialize the map
	authMap := make(map[string]*models.ChanURLAuthDetails)

	// Process usernames
	for _, u := range usernames {
		if !strings.Contains(u, " ") {
			return nil, fmt.Errorf("must input auth username as 'URL username' with a space between the two. %q is invalid", u)
		}

		uParts := strings.Split(u, " ")
		if len(uParts) != 2 {
			return nil, fmt.Errorf("too many space separated elements in the username %q", u)
		}

		// Initialize the struct if it doesn't exist
		if authMap[uParts[0]] == nil {
			authMap[uParts[0]] = &models.ChanURLAuthDetails{}
		}
		authMap[uParts[0]].Username = uParts[1]
	}

	// Process passwords
	for _, p := range passwords {
		if !strings.Contains(p, " ") {
			return nil, fmt.Errorf("must input auth password as 'URL password' with a space between the two")
		}

		pParts := strings.Split(p, " ")
		if len(pParts) != 2 {
			return nil, fmt.Errorf("too many space separated elements in the URL password entry")
		}

		// Initialize the struct if it doesn't exist
		if authMap[pParts[0]] == nil {
			authMap[pParts[0]] = &models.ChanURLAuthDetails{}
		}
		authMap[pParts[0]].Password = pParts[1]
	}

	// Process login URLs
	for _, l := range loginURLs {
		if !strings.Contains(l, " ") {
			return nil, fmt.Errorf("must input auth login URL as 'channelURL loginURL' with a space between the two. %q is invalid", l)
		}

		lParts := strings.Split(l, " ")
		if len(lParts) != 2 {
			return nil, fmt.Errorf("too many space separated elements in the channel URL's login URL entry: %q", l)
		}

		// Initialize the struct if it doesn't exist
		if authMap[lParts[0]] == nil {
			authMap[lParts[0]] = &models.ChanURLAuthDetails{}
		}
		authMap[lParts[0]].LoginURL = lParts[1]
	}

	// Validate that all required fields are present
	for chanURL, details := range authMap {
		if chanURL == "" {
			continue
		}

		var count int
		if details.LoginURL != "" {
			count++
		}
		if details.Username != "" {
			count++
		}
		if details.Password != "" {
			count++
		}

		if count != 3 {
			return nil, fmt.Errorf("every channel URL must have a username, password, and login URL")
		}
	}

	if len(authMap) == 0 {
		logging.I("No auth details added")
		return nil, nil
	}

	return authMap, nil
}
