package cfgchannel

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	validation "tubarr/internal/cfg/validation"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
		if err := validation.ValidateMinFreeMem(c.minFreeMem); err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MinFreeMem = c.minFreeMem
			return nil
		})
	}

	if c.metarrExt != "" {
		_, err := validation.ValidateOutputFiletype(c.metarrExt)
		if err != nil {
			return nil, err
		}
		c.metarrExt = strings.ToLower(c.metarrExt)
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.Ext = c.metarrExt
			return nil
		})
	}

	if c.renameStyle != "" {
		if err := validation.ValidateRenameFlag(c.renameStyle); err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.RenameStyle = c.renameStyle
			return nil
		})
	}

	if c.fileDatePfx != "" {
		if !validation.ValidateDateFormat(c.fileDatePfx) {
			return nil, errors.New("invalid Metarr filename date tag format")
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FileDatePfx = c.fileDatePfx
			return nil
		})
	}

	if len(c.filenameReplaceSfx) != 0 {
		valid, err := validation.ValidateFilenameSuffixReplace(c.filenameReplaceSfx)
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
		valid, err := validation.ValidateMetaOps(c.metaOps)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOps = valid
			return nil
		})
	}

	if c.useGPU != "" {
		validGPU, _, err := validation.ValidateGPU(c.useGPU, c.gpuDir)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.UseGPU = validGPU
			return nil
		})
	} else if cmd.Flags().Changed(keys.TranscodeGPU) && c.useGPU == "" {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.UseGPU = c.useGPU
			return nil
		})
	}

	if cmd.Flags().Changed(keys.TranscodeGPUDir) {
		fns = append(fns, func(m *models.MetarrArgs) error {

			if c.gpuDir != "" {
				if _, err := os.Stat(c.gpuDir); err != nil {
					switch {
					case os.IsNotExist(err):
						return fmt.Errorf("gpu directory was entered as %v, but path does not exist", c.gpuDir)
					default:
						return fmt.Errorf("error checking GPU directory %v: %w", c.gpuDir, err)
					}
				}
			}
			m.GPUDir = c.gpuDir
			return nil
		})
	}

	if c.transcodeCodec != "" {
		validTranscodeCodec, err := validation.ValidateTranscodeCodec(c.transcodeCodec, c.useGPU)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeCodec = validTranscodeCodec
			return nil
		})
	} else if cmd.Flags().Changed(keys.TranscodeCodec) && c.transcodeCodec == "" {
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeCodec = c.transcodeCodec
			return nil
		})
	}

	if c.transcodeAudioCodec != "" || cmd.Flags().Changed(keys.TranscodeAudioCodec) {
		validTranscodeAudioCodec, err := validation.ValidateTranscodeAudioCodec(c.transcodeAudioCodec)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeAudioCodec = validTranscodeAudioCodec
			return nil
		})
	}

	if c.transcodeQuality != "" || cmd.Flags().Changed(keys.TranscodeQuality) {
		validTranscodeQuality, err := validation.ValidateTranscodeQuality(c.transcodeQuality)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeQuality = validTranscodeQuality
			return nil
		})
	}

	if c.transcodeVideoFilter != "" {
		validTranscodeVideoFilter, err := validation.ValidateTranscodeVideoFilter(c.transcodeVideoFilter)
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
	channelConfigFile      string
	cookieSource           string
	crawlFreq              int
	filters                []string
	filterFile             string
	retries                int
	externalDownloader     string
	externalDownloaderArgs string
	concurrency            int
	maxFilesize            string
	fromDate               string
	toDate                 string
	ytdlpOutputExt         string
}

func getSettingsArgFns(c chanSettings) (fns []func(m *models.ChannelSettings) error, err error) {

	if c.concurrency > 0 {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Concurrency = c.concurrency
			return nil
		})
	}

	if c.channelConfigFile != "" {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ChannelConfigFile = c.channelConfigFile
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

	if len(c.filters) > 0 {
		dlFilters, err := validation.ValidateChannelOps(c.filters)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Filters = dlFilters
			return nil
		})
	}

	if c.fromDate != "" {
		validFromDate, err := validation.ValidateToFromDate(c.fromDate)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.FromDate = validFromDate
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

	if c.retries > 0 {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Retries = c.retries
			return nil
		})
	}

	if c.toDate != "" {
		validToDate, err := validation.ValidateToFromDate(c.toDate)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ToDate = validToDate
			return nil
		})
	}

	if c.ytdlpOutputExt != "" {
		c.ytdlpOutputExt = strings.ToLower(c.ytdlpOutputExt)
		if err := validation.ValidateYtdlpOutputExtension(c.ytdlpOutputExt); err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.YtdlpOutputExt = c.ytdlpOutputExt
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

// parseAuthDetails parses authorization details for a particular channel URL.
func parseAuthDetails(usernames, passwords, loginURLs []string) (map[string]*models.ChannelAccessDetails, error) {
	logging.D(3, "Parsing authorization details...")
	if usernames == nil && passwords == nil && loginURLs == nil {
		logging.D(3, "No authorization details to parse...")
		return nil, nil
	}

	// Initialize the map
	authMap := make(map[string]*models.ChannelAccessDetails)

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
			authMap[uParts[0]] = &models.ChannelAccessDetails{}
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
			authMap[pParts[0]] = &models.ChannelAccessDetails{}
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
			authMap[lParts[0]] = &models.ChannelAccessDetails{}
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

// loadConfigFile loads in the preset configuration file.
func loadConfigFile(file string) error {

	if _, err := validation.ValidateFile(file, false); err != nil {
		return err
	}

	logging.I("Using configuration file %q", file)
	viper.SetConfigFile(file)
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	return nil
}

// getConfigValue normalizes and retrieves values from the config file.
// Supports both kebab-case and snake_case keys.
func getConfigValue[T any](key string) (T, bool) {
	var zero T

	// Try original key first
	if viper.IsSet(key) {
		if val, ok := convertConfigValue[T](viper.Get(key)); ok {
			return val, true
		}
	}

	// Try snake_case version
	snakeKey := strings.ReplaceAll(key, "-", "_")
	if snakeKey != key && viper.IsSet(snakeKey) {
		if val, ok := convertConfigValue[T](viper.Get(snakeKey)); ok {
			return val, true
		}
	}

	// Try kebab-case version
	kebabKey := strings.ReplaceAll(key, "_", "-")
	if kebabKey != key && viper.IsSet(kebabKey) {
		if val, ok := convertConfigValue[T](viper.Get(kebabKey)); ok {
			return val, true
		}
	}

	return zero, false
}

// convertConfigValue handles config entry conversions safely.
func convertConfigValue[T any](v any) (T, bool) {
	var zero T

	// Direct type match
	if val, ok := v.(T); ok {
		return val, true
	}

	// Let Viper handle the conversion - it's already good at this
	switch any(zero).(type) {
	case string:
		if s, ok := v.(string); ok {
			return any(s).(T), true
		}
		return any(fmt.Sprintf("%v", v)).(T), true

	case int:
		if i, ok := v.(int); ok {
			return any(i).(T), true
		}
		if i64, ok := v.(int64); ok {
			return any(int(i64)).(T), true
		}
		if f, ok := v.(float64); ok {
			return any(int(f)).(T), true
		}

	case float64:
		if f, ok := v.(float64); ok {
			return any(f).(T), true
		}
		if i, ok := v.(int); ok {
			return any(float64(i)).(T), true
		}

	case bool:
		if b, ok := v.(bool); ok {
			return any(b).(T), true
		}

	case []string:
		if slice, ok := v.([]string); ok {
			return any(slice).(T), true
		}
		if slice, ok := v.([]any); ok {
			strSlice := make([]string, len(slice))
			for i, item := range slice {
				strSlice[i] = fmt.Sprintf("%v", item)
			}
			return any(strSlice).(T), true
		}
	}

	return zero, false
}
