package cfgchannel

import (
	"errors"
	"fmt"
	"os"
	"slices"
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
	filenameDateTag      string
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

	f := cmd.Flags()

	// Min free memory
	if f.Changed(keys.MMinFreeMem) {
		if c.minFreeMem != "" {
			if err := validation.ValidateMinFreeMem(c.minFreeMem); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MinFreeMem = c.minFreeMem
			return nil
		})
	}

	// Metarr final video output extension (e.g. 'mp4')
	if f.Changed(keys.MExt) {
		if c.metarrExt != "" {
			_, err := validation.ValidateOutputFiletype(c.metarrExt)
			if err != nil {
				return nil, err
			}
		}
		c.metarrExt = strings.ToLower(c.metarrExt)
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.Ext = c.metarrExt
			return nil
		})
	}

	// Rename style (e.g. 'spaces')
	if f.Changed(keys.MRenameStyle) {
		if c.renameStyle != "" {
			if err := validation.ValidateRenameFlag(c.renameStyle); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.RenameStyle = c.renameStyle
			return nil
		})
	}

	// Filename date tag
	if f.Changed(keys.MFilenameDateTag) {
		if c.filenameDateTag != "" {
			if !validation.ValidateDateFormat(c.filenameDateTag) {
				return nil, errors.New("invalid Metarr filename date tag format")
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameDateTag = c.filenameDateTag
			return nil
		})
	}

	// Filename replace suffix (e.g. '_1' to '')
	if f.Changed(keys.MFilenameReplaceSuffix) {
		valid := c.filenameReplaceSfx

		if len(c.filenameReplaceSfx) > 0 {
			valid, err = validation.ValidateFilenameSuffixReplace(c.filenameReplaceSfx)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.FilenameReplaceSfx = valid
			return nil
		})
	}

	// Output directory
	if f.Changed(keys.MOutputDir) {
		if c.outputDir != "" {
			if _, err = validation.ValidateDirectory(c.outputDir, false); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.OutputDir = c.outputDir
			return nil
		})
	}

	// Meta operations (e.g. 'all-credits:set:author')
	if f.Changed(keys.MMetaOps) {
		valid := c.metaOps

		if len(c.metaOps) > 0 {
			valid, err = validation.ValidateMetaOps(c.metaOps)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.MetaOps = valid
			return nil
		})
	}

	// Use GPU for transcoding
	if f.Changed(keys.TranscodeGPU) {
		validGPU := c.useGPU

		if c.useGPU != "" {
			validGPU, _, err = validation.ValidateGPU(c.useGPU, c.gpuDir)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.UseGPU = validGPU
			return nil
		})
	}

	// Transcoding GPU directory
	if f.Changed(keys.TranscodeGPUDir) {
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

	// Video codec
	if f.Changed(keys.TranscodeCodec) {
		validTranscodeCodec := c.transcodeCodec

		if c.transcodeCodec != "" {
			validTranscodeCodec, err = validation.ValidateTranscodeCodec(c.transcodeCodec, c.useGPU)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeCodec = validTranscodeCodec
			return nil
		})
	}

	// Audio codec
	if f.Changed(keys.TranscodeAudioCodec) {
		validTranscodeAudioCodec := c.transcodeAudioCodec

		if c.transcodeAudioCodec != "" {
			validTranscodeAudioCodec, err = validation.ValidateTranscodeAudioCodec(c.transcodeAudioCodec)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeAudioCodec = validTranscodeAudioCodec
			return nil
		})
	}

	// Transcode quality
	if f.Changed(keys.TranscodeQuality) {
		validTranscodeQuality := c.transcodeQuality

		if c.transcodeQuality != "" {
			validTranscodeQuality, err = validation.ValidateTranscodeQuality(c.transcodeQuality)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(m *models.MetarrArgs) error {
			m.TranscodeQuality = validTranscodeQuality
			return nil
		})
	}

	// Transcode video filter
	if f.Changed(keys.TranscodeVideoFilter) {
		validTranscodeVideoFilter := c.transcodeVideoFilter

		if c.transcodeVideoFilter != "" {
			validTranscodeVideoFilter, err = validation.ValidateTranscodeVideoFilter(c.transcodeVideoFilter)
			if err != nil {
				return nil, err
			}
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
	concurrency            int
	cookieSource           string
	crawlFreq              int
	externalDownloader     string
	externalDownloaderArgs string
	filters                []string
	filterFile             string
	fromDate               string
	jsonDir                string
	maxFilesize            string
	moveOps                []string
	moveOpsFile            string
	paused                 bool
	retries                int
	toDate                 string
	videoDir               string
	useGlobalCookies       bool
	ytdlpOutputExt         string
}

// getSettingsArgsFns creates the functions to send in to update the database with new values.
func getSettingsArgFns(cmd *cobra.Command, c chanSettings) (fns []func(m *models.ChannelSettings) error, err error) {

	f := cmd.Flags()

	// Concurrency
	if f.Changed(keys.Concurrency) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Concurrency = max(c.concurrency, 1)
			return nil
		})
	}

	// Channel config file location
	if f.Changed(keys.ChannelConfigFile) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ChannelConfigFile = c.channelConfigFile
			return nil
		})
	}

	// Cookie source
	if f.Changed(keys.CookieSource) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.CookieSource = c.cookieSource
			return nil
		})
	}

	// Crawl frequency
	if f.Changed(keys.CrawlFreq) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.CrawlFreq = max(c.crawlFreq, 1)
			return nil
		})
	}

	// External downloader
	if f.Changed(keys.ExternalDownloader) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ExternalDownloader = c.externalDownloader
			return nil
		})
	}

	// External downloader arguments
	if f.Changed(keys.ExternalDownloaderArgs) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ExternalDownloaderArgs = c.externalDownloaderArgs
			return nil
		})
	}

	// Filter ops ('field:contains:frogs:must')
	if f.Changed(keys.FilterOpsInput) {
		dlFilters, err := validation.ValidateFilterOps(c.filters)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Filters = dlFilters
			return nil
		})
	}

	// Move ops ('field:value:output directory')
	if f.Changed(keys.MoveOps) {
		moveOperations, err := validation.ValidateMoveOps(c.moveOps)
		if err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.MoveOps = moveOperations
			return nil
		})
	}

	// From date
	if f.Changed(keys.FromDate) {
		var validFromDate string

		if c.fromDate != "" {
			validFromDate, err = validation.ValidateToFromDate(c.fromDate)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.FromDate = validFromDate
			return nil
		})
	}

	// JSON directory
	if f.Changed(keys.JSONDir) {
		if c.jsonDir == "" {
			if c.videoDir != "" {
				c.jsonDir = c.videoDir
			} else {
				return nil, fmt.Errorf("json directory cannot be empty. Attempted to default to video directory but video directory is also empty")
			}
		}
		if _, err = validation.ValidateDirectory(c.jsonDir, false); err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.JSONDir = c.jsonDir
			return nil
		})
	}

	// Max download filesize
	if f.Changed(keys.MaxFilesize) {
		if c.maxFilesize != "" {
			c.maxFilesize, err = validation.ValidateMaxFilesize(c.maxFilesize)
			if err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.MaxFilesize = c.maxFilesize
			return nil
		})
	}

	// Pause channel
	if f.Changed(keys.Pause) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Paused = c.paused
			return nil
		})
	}

	// Download retry amount
	if f.Changed(keys.DLRetries) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.Retries = c.retries
			return nil
		})
	}

	// To date
	if f.Changed(keys.ToDate) {
		var validToDate string

		if c.toDate != "" {
			validToDate, err = validation.ValidateToFromDate(c.toDate)
			if err != nil {
				return nil, err
			}
		}

		fns = append(fns, func(s *models.ChannelSettings) error {
			s.ToDate = validToDate
			return nil
		})
	}

	// Video directory
	if f.Changed(keys.VideoDir) {
		if c.videoDir == "" {
			return nil, fmt.Errorf("video directory cannot be empty")
		}

		if _, err = validation.ValidateDirectory(c.videoDir, false); err != nil {
			return nil, err
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.VideoDir = c.videoDir
			return nil
		})
	}

	// Use global cookies
	if f.Changed(keys.UseGlobalCookies) {
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.UseGlobalCookies = c.useGlobalCookies
			return nil
		})
	}

	// YT-DLP output filetype for 'merge-output-format'
	if f.Changed(keys.YtdlpOutputExt) {
		if c.ytdlpOutputExt != "" {
			c.ytdlpOutputExt = strings.ToLower(c.ytdlpOutputExt)
			if err := validation.ValidateYtdlpOutputExtension(c.ytdlpOutputExt); err != nil {
				return nil, err
			}
		}
		fns = append(fns, func(s *models.ChannelSettings) error {
			s.YtdlpOutputExt = c.ytdlpOutputExt
			return nil
		})
	}

	return fns, nil
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
		return "", "", errors.New("please enter either a channel ID or channel name")
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

// parseAuthDetails parses authorization details for channel URLs.
func parseAuthDetails(u, p, l string, a, cURLs []string, deleteAll bool) (map[string]*models.ChannelAccessDetails, error) {

	var (
		authMap = make(map[string]*models.ChannelAccessDetails, len(cURLs))
	)

	// Should delete all?
	if deleteAll {
		for _, cURL := range cURLs {
			authMap[cURL] = &models.ChannelAccessDetails{
				Username: "",
				Password: "",
				LoginURL: "",
			}
		}
		logging.I("Deleted authentication details for channel URLs: %v", cURLs)
		return authMap, nil
	}

	// Proceed
	if len(a) == 0 && (u == "" || l == "") {
		logging.D(3, "No authorization details to parse...")
		return nil, nil
	}

	// For full auth strings with ONE channel URL
	if len(a) > 0 && len(cURLs) == 1 {
		for _, details := range a {

			// Split
			d := strings.Split(details, "|")

			// Validate
			if len(d) < 3 || len(d) > 4 {
				return nil, fmt.Errorf("authentication details should be in format 'username|password|login URL'")
			}

			if len(d) == 4 {
				if d[0] != cURLs[0] {
					return nil, fmt.Errorf("failsafe for user error: entered authentication channel URL as %q but channel's actual URL is %q. Aborting in case of mistake", d[0], cURLs[0])
				}
			}

			// Add to model
			authMap[cURLs[0]] = &models.ChannelAccessDetails{
				Username: d[0],
				Password: d[1],
				LoginURL: d[2],
			}
		}
		return authMap, nil
	}

	// For full auth strings with multiple channel URLs
	if len(a) > 0 && len(cURLs) > 1 {
		for _, details := range a {

			// Split
			d := strings.Split(details, "|")

			// Validate length
			if len(d) != 4 {
				if len(d) == 3 {
					return nil, fmt.Errorf("channel has multiple URLs, authentication details must be in format 'channel URL|username|password|login URL'")
				}
				return nil, fmt.Errorf("authentication details should be in format 'username|password|login URL'")
			}

			// Validate URL exists for channel
			var validURL bool
			if slices.Contains(cURLs, d[0]) {
				validURL = true
			}

			if !validURL {
				return nil, fmt.Errorf("channel URL %q in authentication string does not exist for channel with URLs %v", d[0], cURLs)
			}

			// Add to model
			authMap[d[0]] = &models.ChannelAccessDetails{
				Username: d[1],
				Password: d[2],
				LoginURL: d[3],
			}
		}
		return authMap, nil
	}

	// Continuation means no full authentication strings were input,
	// but username and login URL are not empty (checked earlier)
	for _, cURL := range cURLs {
		authMap[cURL] = &models.ChannelAccessDetails{
			Username: u,
			Password: p,
			LoginURL: l,
		}
	}
	return authMap, nil
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
