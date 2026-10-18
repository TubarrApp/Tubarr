package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// channelResponse is a channel as listed in the web UI.
type channelResponse struct {
	ID                   int64                `json:"id"`
	Name                 string               `json:"name"`
	URLs                 []string             `json:"urls"`
	LastScan             time.Time            `json:"last_scan"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
	ChannelConfigFile    string               `json:"channel_config_file"`
	NewVideoNotification bool                 `json:"new_video_notification"`
	LoadError            string               `json:"load_error,omitempty"` // Why it won't run until its settings are fixed.
	Settings             *settingsResponse    `json:"settings,omitempty"`
	Metarr               *metarrArgsResponse  `json:"metarr,omitempty"`
	URLModels            []channelURLResponse `json:"url_models,omitempty"`
}

// decodeJSON decodes a JSON request body into v, refusing unknown keys.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// loadError returns a channel's load error as text, or "" if it loaded cleanly.
func loadError(c *models.Channel) string {
	if c.LoadErr == nil {
		return ""
	}
	return c.LoadErr.Error()
}

// channelURLResponse is a channel URL as listed in the web UI.
type channelURLResponse struct {
	ID       int64               `json:"id"`
	URL      string              `json:"url"`
	Username string              `json:"username"`
	LoginURL string              `json:"login_url"`
	Settings *settingsResponse   `json:"settings,omitempty"`
	Metarr   *metarrArgsResponse `json:"metarr,omitempty"`
}

// channelDetailResponse is a channel as loaded into the edit form.
type channelDetailResponse struct {
	ID                int64                          `json:"id"`
	Name              string                         `json:"name"`
	URLs              []string                       `json:"urls"`
	LastScan          time.Time                      `json:"last_scan"`
	CreatedAt         time.Time                      `json:"created_at"`
	UpdatedAt         time.Time                      `json:"updated_at"`
	ChannelConfigFile string                         `json:"channel_config_file"`
	LoadError         string                         `json:"load_error,omitempty"`
	Settings          *settingsResponse              `json:"settings,omitempty"`
	Metarr            *metarrArgsResponse            `json:"metarr,omitempty"`
	AuthDetails       []authDetailResponse           `json:"auth_details"`
	URLSettings       map[string]urlSettingsResponse `json:"url_settings"`
	Notifications     []string                       `json:"notifications,omitempty"` // One line each, as the form takes them.
}

// authDetailResponse is a channel URL's login, without its password.
type authDetailResponse struct {
	ChannelURL  string `json:"channel_url"`
	Username    string `json:"username"`
	HasPassword bool   `json:"has_password"`
	LoginURL    string `json:"login_url"`
}

// urlSettingsResponse is a channel URL's custom settings.
type urlSettingsResponse struct {
	Settings *settingsResponse   `json:"settings,omitempty"`
	Metarr   *metarrArgsResponse `json:"metarr,omitempty"`
}

// metarrArgsResponse is MetarrArgs as sent to the web UI, with operations as newline-separated text. Unset fields are left out.
type metarrArgsResponse struct {
	OutputDir               string   `json:"metarr_output_directory,omitempty"`
	MetaPurge               string   `json:"metarr_purge_metafile,omitempty"`
	OutputExt               string   `json:"metarr_output_ext,omitempty"`
	RenameStyle             string   `json:"metarr_rename_style,omitempty"`
	Concurrency             int      `json:"metarr_concurrency,omitempty"`
	MaxCPU                  *float64 `json:"metarr_max_cpu_usage,omitempty"`
	MinFreeMem              string   `json:"metarr_min_free_mem,omitempty"`
	GPU                     string   `json:"metarr_gpu,omitempty"`
	GPUNode                 string   `json:"metarr_transcode_gpu_node,omitempty"`
	VideoCodecs             []string `json:"metarr_transcode_video_transcode_codecs,omitempty"`
	AudioCodecs             []string `json:"metarr_transcode_audio_codecs,omitempty"`
	TranscodeQuality        string   `json:"metarr_transcode_quality,omitempty"`
	TranscodeVideoFilter    string   `json:"metarr_transcode_video_filter,omitempty"`
	ExtraFFmpegArgs         string   `json:"metarr_extra_ffmpeg_args,omitempty"`
	FilenameOpsFile         string   `json:"metarr_filename_ops_file,omitempty"`
	FilteredFilenameOpsFile string   `json:"metarr_filtered_filename_ops_file,omitempty"`
	MetaOpsFile             string   `json:"metarr_meta_ops_file,omitempty"`
	FilteredMetaOpsFile     string   `json:"metarr_filtered_meta_ops_file,omitempty"`
	MetaOps                 string   `json:"metarr_meta_ops,omitempty"`
	FilenameOps             string   `json:"metarr_filename_ops,omitempty"`
	FilteredMetaOps         string   `json:"metarr_filtered_meta_ops,omitempty"`
	FilteredFilenameOps     string   `json:"metarr_filtered_filename_ops,omitempty"`
}

// newMetarrArgsResponse converts MetarrArgs for the web UI.
func newMetarrArgsResponse(m *models.MetarrArgs) *metarrArgsResponse {
	if m == nil {
		return nil
	}
	r := &metarrArgsResponse{
		OutputDir:               m.OutputDir,
		MetaPurge:               m.MetaPurge,
		OutputExt:               m.OutputExt,
		RenameStyle:             m.RenameStyle,
		Concurrency:             max(m.Concurrency, 0),
		MinFreeMem:              m.MinFreeMem,
		GPU:                     m.TranscodeGPU,
		GPUNode:                 m.TranscodeGPUNode,
		VideoCodecs:             m.TranscodeVideoCodecs,
		AudioCodecs:             m.TranscodeAudioCodecs,
		TranscodeQuality:        m.TranscodeQuality,
		TranscodeVideoFilter:    m.TranscodeVideoFilter,
		ExtraFFmpegArgs:         m.ExtraFFmpegArgs,
		FilenameOpsFile:         m.FilenameOpsFile,
		FilteredFilenameOpsFile: m.FilteredFilenameOpsFile,
		MetaOpsFile:             m.MetaOpsFile,
		FilteredMetaOpsFile:     m.FilteredMetaOpsFile,
		MetaOps:                 strings.Join(sharedparsing.FormatMetaOps(m.MetaOps, "", true), "\n"),
		FilenameOps:             strings.Join(sharedparsing.FormatFilenameOps(m.FilenameOps, "", true), "\n"),
	}
	if m.MaxCPU >= 0 {
		r.MaxCPU = &m.MaxCPU
	}
	filteredMeta := make([]string, 0, len(m.FilteredMetaOps))
	for i := range m.FilteredMetaOps {
		filteredMeta = append(filteredMeta, models.FilteredMetaOpsToSlice(m.FilteredMetaOps[i])...)
	}
	r.FilteredMetaOps = strings.Join(filteredMeta, "\n")
	filteredFilename := make([]string, 0, len(m.FilteredFilenameOps))
	for i := range m.FilteredFilenameOps {
		filteredFilename = append(filteredFilename, models.FilteredFilenameOpsToSlice(m.FilteredFilenameOps[i])...)
	}
	r.FilteredFilenameOps = strings.Join(filteredFilename, "\n")
	return r
}

// settingsResponse is Settings as sent to the web UI, with filters and move ops as newline-separated text. Unset fields are left out.
type settingsResponse struct {
	VideoDir               string   `json:"video_directory,omitempty"`
	JSONDir                string   `json:"json_directory,omitempty"`
	MaxConcurrency         int      `json:"max_concurrency,omitempty"`
	CrawlFreq              int      `json:"crawl_freq,omitempty"`
	DownloadRetries        int      `json:"download_retries,omitempty"`
	MaxFilesize            string   `json:"max_filesize,omitempty"`
	FromDate               string   `json:"from_date,omitempty"`
	ToDate                 string   `json:"to_date,omitempty"`
	FilterFile             string   `json:"filter_file,omitempty"`
	Filters                string   `json:"filters,omitempty"`
	YtdlpOutputExt         string   `json:"ytdlp_output_ext,omitempty"`
	CookiesFromBrowser     string   `json:"cookies_from_browser,omitempty"`
	ExternalDownloader     string   `json:"external_downloader,omitempty"`
	ExternalDownloaderArgs string   `json:"external_downloader_args,omitempty"`
	ExtraYTDLPVideoArgs    string   `json:"extra_ytdlp_video_args,omitempty"`
	ExtraYTDLPMetaArgs     string   `json:"extra_ytdlp_meta_args,omitempty"`
	ExtraYTDLPCrawlArgs    string   `json:"extra_ytdlp_crawl_args,omitempty"`
	MoveOpsFile            string   `json:"move_ops_file,omitempty"`
	MoveOps                string   `json:"move_ops,omitempty"`
	UseGlobalCookies       *bool    `json:"use_global_cookies,omitempty"`
	Paused                 bool     `json:"paused,omitempty"`
	AutoDownload           *bool    `json:"auto_download,omitempty"`
	PreferredVideoCodecs   []string `json:"ytdlp_preferred_video_codecs,omitempty"`
	PreferredAudioCodecs   []string `json:"ytdlp_preferred_audio_codecs,omitempty"`
}

// newSettingsResponse converts Settings for the web UI.
func newSettingsResponse(s *models.Settings) *settingsResponse {
	if s == nil {
		return nil
	}
	return &settingsResponse{
		VideoDir:               s.VideoDir,
		JSONDir:                s.JSONDir,
		MaxConcurrency:         max(s.Concurrency, 0),
		CrawlFreq:              max(s.CrawlFreq, 0),
		DownloadRetries:        max(s.Retries, 0),
		MaxFilesize:            s.MaxFilesize,
		FromDate:               s.FromDate,
		ToDate:                 s.ToDate,
		FilterFile:             s.FilterFile,
		Filters:                strings.Join(models.FiltersArrayToSlice(s.Filters, true), "\n"),
		YtdlpOutputExt:         s.YtdlpOutputExt,
		CookiesFromBrowser:     s.CookiesFromBrowser,
		ExternalDownloader:     s.ExternalDownloader,
		ExternalDownloaderArgs: s.ExternalDownloaderArgs,
		ExtraYTDLPVideoArgs:    s.ExtraYTDLPVideoArgs,
		ExtraYTDLPMetaArgs:     s.ExtraYTDLPMetaArgs,
		ExtraYTDLPCrawlArgs:    s.ExtraYTDLPCrawlArgs,
		MoveOpsFile:            s.MetaFilterMoveOpFile,
		MoveOps:                strings.Join(models.MetaFilterMoveOpsArrayToSlice(s.MetaFilterMoveOps), "\n"),
		UseGlobalCookies:       s.UseGlobalCookies,
		Paused:                 s.Paused,
		AutoDownload:           s.AutoDownload,
		PreferredVideoCodecs:   s.YTDLPPreferredVideoCodecs,
		PreferredAudioCodecs:   s.YTDLPPreferredAudioCodecs,
	}
}

// splitAuthDetails splits the auth_details form value into one JSON object per entry.
//
// The web UI sends a JSON array; space-separated objects are still accepted, though they break on values with spaces.
func splitAuthDetails(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") {
		return strings.Fields(s), nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(s), &entries); err != nil {
		return nil, fmt.Errorf("invalid auth_details JSON: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, string(e))
	}
	return out, nil
}

// parseNotificationLines parses the notifications form value, one per line, checking scoped ones name one of urls.
func parseNotificationLines(s string, urls []string) ([]*models.Notification, error) {
	notifications, err := parsing.ParseNotifications(splitNonEmptyLines(s))
	if err != nil {
		return nil, err
	}
	for _, n := range notifications {
		if n.ChannelURL != "" && !slices.ContainsFunc(urls, func(u string) bool {
			return sharedparsing.NormalizeChannelURL(u) == sharedparsing.NormalizeChannelURL(n.ChannelURL)
		}) {
			return nil, fmt.Errorf("notification %q is for channel URL %q, which this channel doesn't have", n.Name, n.ChannelURL)
		}
	}
	return notifications, nil
}

// notificationEscaper escapes notification fields for parsing.ParseNotifications, leaving colons so URLs stay readable.
var notificationEscaper = strings.NewReplacer(`\`, `\\`, `|`, `\|`)

// notificationLine renders a notification as parseNotificationLines reads it.
func notificationLine(n *models.Notification) string {
	esc := notificationEscaper.Replace
	switch {
	case n.ChannelURL != "":
		return esc(n.ChannelURL) + "|" + esc(n.NotifyURL) + "|" + esc(n.Name)
	case n.Name != n.NotifyURL:
		return esc(n.NotifyURL) + "|" + esc(n.Name)
	default:
		return esc(n.NotifyURL)
	}
}

// formConfigFile returns the channel_config_file form value, checking the file exists.
func formConfigFile(r *http.Request) (string, error) {
	path := strings.TrimSpace(r.FormValue("channel_config_file"))
	if path == "" {
		return "", nil
	}
	if _, _, err := sharedvalidation.ValidateFile(path, false, sharedtemplates.NoTemplateTags); err != nil {
		return "", fmt.Errorf("invalid channel config file: %w", err)
	}
	return path, nil
}

// urlSettingsInput is the per-URL settings JSON sent by the web UI, keyed by channel URL.
type urlSettingsInput struct {
	Settings *settingsInput   `json:"settings"`
	Metarr   *metarrArgsInput `json:"metarr"`
}

// settingsInput holds the per-URL Settings the web UI sent. Nil fields were not sent.
type settingsInput struct {
	CookiesFromBrowser        *string  `json:"cookies_from_browser"`
	ExternalDownloader        *string  `json:"external_downloader"`
	ExternalDownloaderArgs    *string  `json:"external_downloader_args"`
	ExtraYTDLPVideoArgs       *string  `json:"extra_ytdlp_video_args"`
	ExtraYTDLPMetaArgs        *string  `json:"extra_ytdlp_meta_args"`
	ExtraYTDLPCrawlArgs       *string  `json:"extra_ytdlp_crawl_args"`
	FilterFile                *string  `json:"filter_file"`
	MoveOpsFile               *string  `json:"move_ops_file"`
	JSONDir                   *string  `json:"json_directory"`
	VideoDir                  *string  `json:"video_directory"`
	MaxFilesize               *string  `json:"max_filesize"`
	YtdlpOutputExt            *string  `json:"ytdlp_output_ext"`
	FromDate                  *string  `json:"from_date"`
	ToDate                    *string  `json:"to_date"`
	MaxConcurrency            *int     `json:"max_concurrency"`
	CrawlFreq                 *int     `json:"crawl_freq"`
	DownloadRetries           *int     `json:"download_retries"`
	UseGlobalCookies          *bool    `json:"use_global_cookies"`
	Paused                    *bool    `json:"paused"`
	AutoDownload              *bool    `json:"auto_download"`
	Filters                   *string  `json:"filters"`
	MoveOps                   *string  `json:"move_ops"`
	YTDLPPreferredVideoCodecs []string `json:"ytdlp_preferred_video_codecs"`
	YTDLPPreferredAudioCodecs []string `json:"ytdlp_preferred_audio_codecs"`
}

// metarrArgsInput holds the per-URL MetarrArgs the web UI sent. Nil fields were not sent.
type metarrArgsInput struct {
	OutputExt               *string  `json:"metarr_output_ext"`
	FilenameOpsFile         *string  `json:"metarr_filename_ops_file"`
	FilteredFilenameOpsFile *string  `json:"metarr_filtered_filename_ops_file"`
	MetaOpsFile             *string  `json:"metarr_meta_ops_file"`
	FilteredMetaOpsFile     *string  `json:"metarr_filtered_meta_ops_file"`
	ExtraFFmpegArgs         *string  `json:"metarr_extra_ffmpeg_args"`
	RenameStyle             *string  `json:"metarr_rename_style"`
	MinFreeMem              *string  `json:"metarr_min_free_mem"`
	GPU                     *string  `json:"metarr_gpu"`
	GPUNode                 *string  `json:"metarr_transcode_gpu_node"`
	OutputDir               *string  `json:"metarr_output_directory"`
	MetaPurge               *string  `json:"metarr_purge_metafile"`
	TranscodeVideoFilter    *string  `json:"metarr_transcode_video_filter"`
	VideoCodecs             *string  `json:"metarr_transcode_video_transcode_codecs"` // Newline-separated, as in the main form.
	AudioCodecs             *string  `json:"metarr_transcode_audio_codecs"`
	TranscodeQuality        *string  `json:"metarr_transcode_quality"`
	Concurrency             *int     `json:"metarr_concurrency"`
	MaxCPU                  *float64 `json:"metarr_max_cpu_usage"`
	FilenameOps             *string  `json:"metarr_filename_ops"`
	FilteredFilenameOps     *string  `json:"metarr_filtered_filename_ops"`
	MetaOps                 *string  `json:"metarr_meta_ops"`
	FilteredMetaOps         *string  `json:"metarr_filtered_meta_ops"`
}

// parseURLSettings parses the per-URL settings JSON from the frontend.
func parseURLSettings(raw string) (map[string]urlSettings, error) {
	parsed := make(map[string]urlSettings)
	if raw == "" {
		return parsed, nil
	}
	var input map[string]urlSettingsInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, fmt.Errorf("invalid url_settings JSON: %w", err)
	}
	for channelURL, in := range input {
		var (
			out urlSettings
			err error
		)
		if in.Settings != nil {
			nilWhitespaceStrings(in.Settings)
			if out.Settings, err = parseSettingsInput(in.Settings); err != nil {
				return nil, fmt.Errorf("invalid Settings for URL %q: %w", channelURL, err)
			}
		}
		if in.Metarr != nil {
			nilWhitespaceStrings(in.Metarr)
			if out.Metarr, err = parseMetarrArgsInput(in.Metarr); err != nil {
				return nil, fmt.Errorf("invalid Metarr Args for URL %q: %w", channelURL, err)
			}
		}
		parsed[channelURL] = out
	}
	return parsed, nil
}

// urlSettings are a channel URL's parsed custom settings.
type urlSettings struct {
	Settings *models.Settings
	Metarr   *models.MetarrArgs
}

// parseSettingsInput validates per-URL Settings from the frontend.
func parseSettingsInput(in *settingsInput) (*models.Settings, error) {
	settings := &models.Settings{}

	// Strings not needing validation.
	setString(&settings.CookiesFromBrowser, in.CookiesFromBrowser)
	setString(&settings.ExternalDownloader, in.ExternalDownloader)
	setString(&settings.ExternalDownloaderArgs, in.ExternalDownloaderArgs)
	setString(&settings.ExtraYTDLPVideoArgs, in.ExtraYTDLPVideoArgs)
	setString(&settings.ExtraYTDLPMetaArgs, in.ExtraYTDLPMetaArgs)
	setString(&settings.ExtraYTDLPCrawlArgs, in.ExtraYTDLPCrawlArgs)

	if v := in.FilterFile; v != nil {
		if _, _, err := sharedvalidation.ValidateFile(*v, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return nil, err
		}
		settings.FilterFile = *v
	}
	if v := in.MoveOpsFile; v != nil {
		if _, _, err := sharedvalidation.ValidateFile(*v, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return nil, err
		}
		settings.MetaFilterMoveOpFile = *v
	}
	if v := in.JSONDir; v != nil {
		if err := validation.CheckDirectory(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return nil, err
		}
		settings.JSONDir = *v
	}
	if v := in.VideoDir; v != nil {
		if err := validation.CheckDirectory(*v, sharedtemplates.TubarrTemplateTags); err != nil {
			return nil, err
		}
		settings.VideoDir = *v
	}
	if v := in.MaxFilesize; v != nil {
		validFilesize, err := validation.ValidateMaxFilesize(*v)
		if err != nil {
			return nil, err
		}
		settings.MaxFilesize = validFilesize
	}
	if v := in.YtdlpOutputExt; v != nil {
		if err := validation.ValidateYtdlpOutputExtension(*v); err != nil {
			return nil, err
		}
		settings.YtdlpOutputExt = *v
	}
	if v := in.FromDate; v != nil {
		validDate, err := validation.ValidateToFromDate(*v)
		if err != nil {
			return nil, err
		}
		settings.FromDate = validDate
	}
	if v := in.ToDate; v != nil {
		validDate, err := validation.ValidateToFromDate(*v)
		if err != nil {
			return nil, err
		}
		settings.ToDate = validDate
	}

	if v := in.MaxConcurrency; v != nil {
		settings.Concurrency = max(*v, 0)
	}
	if v := in.CrawlFreq; v != nil {
		settings.CrawlFreq = max(*v, 0)
	}
	if v := in.DownloadRetries; v != nil {
		settings.Retries = max(*v, 0)
	}

	settings.UseGlobalCookies = in.UseGlobalCookies
	settings.AutoDownload = in.AutoDownload
	if in.Paused != nil {
		settings.Paused = *in.Paused
	}

	// Models (newline-separated, not space-separated).
	if v := in.Filters; v != nil && *v != "" {
		filters, err := parsing.ParseFilterOps(splitNonEmptyLines(*v), true)
		if err != nil {
			return nil, err
		}
		settings.Filters = filters
	}
	if v := in.MoveOps; v != nil && *v != "" {
		moveOps, err := parsing.ParseMetaFilterMoveOps(splitNonEmptyLines(*v))
		if err != nil {
			return nil, err
		}
		settings.MetaFilterMoveOps = moveOps
	}

	// Codec preferences.
	if in.YTDLPPreferredVideoCodecs != nil {
		validCodecs, err := validation.ValidatePreferredVideoCodecs(in.YTDLPPreferredVideoCodecs)
		if err != nil {
			return nil, err
		}
		settings.YTDLPPreferredVideoCodecs = validCodecs
	}
	if in.YTDLPPreferredAudioCodecs != nil {
		validCodecs, err := validation.ValidatePreferredAudioCodecs(in.YTDLPPreferredAudioCodecs)
		if err != nil {
			return nil, err
		}
		settings.YTDLPPreferredAudioCodecs = validCodecs
	}

	return settings, nil
}

// setString sets *dst to *v if v was sent.
func setString(dst *string, v *string) {
	if v != nil {
		*dst = *v
	}
}

// nilWhitespaceStrings unsets a per-URL input struct's string fields holding only whitespace, so they inherit.
func nilWhitespaceStrings(in any) {
	v := reflect.ValueOf(in).Elem() // The struct the pointer points to.
	for i := range v.NumField() {
		f := v.Field(i)
		if f.Type() != reflect.TypeFor[*string]() {
			continue // Only string fields.
		}
		if f.IsNil() {
			continue // Not sent.
		}
		if strings.TrimSpace(f.Elem().String()) == "" {
			f.SetZero() // Sent blank, so unset it.
		}
	}
}

// blankWhitespaceValues clears form values holding only whitespace, so they read as blank fields.
func blankWhitespaceValues(r *http.Request) {
	for _, vals := range r.Form {
		for i, v := range vals {
			if strings.TrimSpace(v) == "" {
				vals[i] = ""
			}
		}
	}
}

// parseMetarrArgsInput validates per-URL MetarrArgs from the frontend.
func parseMetarrArgsInput(in *metarrArgsInput) (*models.MetarrArgs, error) {
	metarr := &models.MetarrArgs{}

	// Strings not needing validation.
	setString(&metarr.OutputExt, in.OutputExt)
	setString(&metarr.ExtraFFmpegArgs, in.ExtraFFmpegArgs)
	setString(&metarr.TranscodeVideoFilter, in.TranscodeVideoFilter)

	for _, f := range []struct {
		dst *string
		v   *string
	}{
		{&metarr.FilenameOpsFile, in.FilenameOpsFile},
		{&metarr.FilteredFilenameOpsFile, in.FilteredFilenameOpsFile},
		{&metarr.MetaOpsFile, in.MetaOpsFile},
		{&metarr.FilteredMetaOpsFile, in.FilteredMetaOpsFile},
	} {
		if f.v == nil {
			continue
		}
		if _, _, err := sharedvalidation.ValidateFile(*f.v, false, sharedtemplates.TubarrTemplateTags); err != nil {
			return nil, err
		}
		*f.dst = *f.v
	}
	if v := in.RenameStyle; v != nil {
		if err := validation.ValidateRenameFlag(*v); err != nil {
			return nil, err
		}
		metarr.RenameStyle = *v
	}
	if v := in.MinFreeMem; v != nil {
		if _, err := sharedvalidation.ValidateMinFreeMem(*v); err != nil {
			return nil, err
		}
		metarr.MinFreeMem = *v
	}
	if v := in.GPU; v != nil {
		validGPU, err := validation.ValidateGPUAcceleration(*v)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeGPU = validGPU
	}
	if v := in.GPUNode; v != nil {
		validGPUNode, err := sharedvalidation.ValidateAccelTypeDeviceNode(metarr.TranscodeGPU, *v)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeGPUNode = validGPUNode
	}
	if v := in.OutputDir; v != nil {
		if err := validation.CheckDirectory(*v, sharedtemplates.AllTemplatesMap); err != nil {
			return nil, err
		}
		metarr.OutputDir = *v
	}
	if v := in.MetaPurge; v != nil {
		var err error
		if metarr.MetaPurge, err = validation.ValidatePurgeMetafiles(*v); err != nil {
			return nil, err
		}
	}
	if v := in.VideoCodecs; v != nil {
		validPairs, err := validation.ValidateVideoTranscodeCodecSlice(splitNonEmptyLines(*v), metarr.TranscodeGPU)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeVideoCodecs = validPairs
	}
	if v := in.AudioCodecs; v != nil {
		validPairs, err := validation.ValidateAudioTranscodeCodecSlice(splitNonEmptyLines(*v))
		if err != nil {
			return nil, err
		}
		metarr.TranscodeAudioCodecs = validPairs
	}
	if v := in.TranscodeQuality; v != nil {
		validQuality, err := sharedvalidation.ValidateTranscodeQuality(*v)
		if err != nil {
			return nil, err
		}
		metarr.TranscodeQuality = validQuality
	}
	if v := in.Concurrency; v != nil {
		metarr.Concurrency = max(*v, 0)
	}
	if v := in.MaxCPU; v != nil {
		metarr.MaxCPU = *v
	}

	// Models (newline-separated, not space-separated).
	var err error
	if v := in.FilenameOps; v != nil && *v != "" {
		if metarr.FilenameOps, err = parsing.ParseFilenameOps(splitOpLines(*v)); err != nil {
			return nil, err
		}
	}
	if v := in.FilteredFilenameOps; v != nil && *v != "" {
		if metarr.FilteredFilenameOps, err = parsing.ParseFilteredFilenameOps(splitOpLines(*v)); err != nil {
			return nil, err
		}
	}
	if v := in.MetaOps; v != nil && *v != "" {
		if metarr.MetaOps, err = parsing.ParseMetaOps(splitOpLines(*v)); err != nil {
			return nil, err
		}
	}
	if v := in.FilteredMetaOps; v != nil && *v != "" {
		if metarr.FilteredMetaOps, err = parsing.ParseFilteredMetaOps(splitOpLines(*v)); err != nil {
			return nil, err
		}
	}

	return metarr, nil
}

// getSettingsStrings retrieves Settings model strings, converting where needed.
func getSettingsStrings(w http.ResponseWriter, r *http.Request) *models.Settings {
	// -- Initialize --
	// Strings not needing validation
	cookiesFromBrowser := r.FormValue(jsonkeys.SettingsCookiesFromBrowser)
	externalDownloader := r.FormValue(jsonkeys.SettingsExternalDownloader)
	externalDownloaderArgs := r.FormValue(jsonkeys.SettingsExternalDownloaderArgs)
	extraYtdlpVideoArgs := r.FormValue(jsonkeys.SettingsExtraYtdlpVideoArgs)
	extraYtdlpMetaArgs := r.FormValue(jsonkeys.SettingsExtraYtdlpMetaArgs)
	extraYtdlpCrawlArgs := r.FormValue(jsonkeys.SettingsExtraYtdlpCrawlArgs)
	filterFile := r.FormValue(jsonkeys.SettingsFilterFile)
	moveOpFile := r.FormValue(jsonkeys.SettingsMoveOpsFile)

	// Strings needing validation
	jDir := r.FormValue(jsonkeys.SettingsJSONDirectory)
	vDir := r.FormValue(jsonkeys.SettingsVideoDirectory)
	maxFilesizeStr := r.FormValue(jsonkeys.SettingsMaxFilesize)
	ytdlpOutExt := r.FormValue(jsonkeys.SettingsYtdlpOutputExt)
	fromDateStr := r.FormValue(jsonkeys.SettingsFromDate)
	toDateStr := r.FormValue(jsonkeys.SettingsToDate)

	// Integers
	maxConcurrencyStr := r.FormValue(jsonkeys.SettingsMaxConcurrency)
	crawlFreqStr := r.FormValue(jsonkeys.SettingsCrawlFreq)
	retriesStr := r.FormValue(jsonkeys.SettingsDownloadRetries)

	// Bools
	useGlobalCookiesStr := r.FormValue(jsonkeys.SettingsUseGlobalCookies)
	pausedStr := r.FormValue(jsonkeys.SettingsPaused)
	autoDownloadStr := r.FormValue(jsonkeys.SettingsAutoDownload)

	// Models
	filtersStr := r.FormValue(jsonkeys.SettingsFilters)
	moveOpsStr := r.FormValue(jsonkeys.SettingsMoveOps)

	// Codec preferences
	ytdlpPreferredVideoCodecsStr := r.FormValue(jsonkeys.SettingsYTDLPPreferredVideoCodecs)
	ytdlpPreferredAudioCodecsStr := r.FormValue(jsonkeys.SettingsYTDLPPreferredAudioCodecs)

	// -- Validation --
	// Strings
	if vDir == "" {
		http.Error(w, "video directory is required", http.StatusBadRequest)
		return nil
	}
	if err := validation.CheckDirectory(vDir, sharedtemplates.TubarrTemplateTags); err != nil {
		http.Error(w, fmt.Sprintf("video directory %q is invalid: %v", vDir, err), http.StatusBadRequest)
		return nil
	}
	if jDir == "" {
		jDir = vDir // Crawls need a JSON directory, so blank means the video directory, as when adding a channel.
	}
	if err := validation.CheckDirectory(jDir, sharedtemplates.TubarrTemplateTags); err != nil {
		http.Error(w, fmt.Sprintf("JSON directory %q is invalid: %v", jDir, err), http.StatusBadRequest)
		return nil
	}
	maxFilesize, err := validation.ValidateMaxFilesize(maxFilesizeStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("max filesize %q is invalid: %v", maxFilesizeStr, err), http.StatusBadRequest)
		return nil
	}
	if err := validation.ValidateYtdlpOutputExtension(ytdlpOutExt); err != nil {
		http.Error(w, fmt.Sprintf("invalid YTDLP output extension %q: %v", ytdlpOutExt, err), http.StatusBadRequest)
		return nil
	}
	toDate, err := validation.ValidateToFromDate(toDateStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert to date string %q: %v", toDateStr, err), http.StatusBadRequest)
		return nil
	}
	fromDate, err := validation.ValidateToFromDate(fromDateStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to convert from date string %q: %v", fromDateStr, err), http.StatusBadRequest)
		return nil
	}

	// Integers
	maxConcurrency := 0
	if maxConcurrencyStr != "" {
		maxConcurrency, err = strconv.Atoi(maxConcurrencyStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert max concurrency string %q: %v", maxConcurrencyStr, err), http.StatusBadRequest)
			return nil
		}
	}
	maxConcurrency = max(maxConcurrency, 0)
	crawlFreq := 0
	if crawlFreqStr != "" {
		crawlFreq, err = strconv.Atoi(crawlFreqStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert crawl frequency string %q: %v", crawlFreqStr, err), http.StatusBadRequest)
			return nil
		}
	}
	retries := 0
	if retriesStr != "" {
		retries, err = strconv.Atoi(retriesStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert retries string %q: %v", retriesStr, err), http.StatusBadRequest)
			return nil
		}
	}

	// Bools
	useGlobalCookies := (useGlobalCookiesStr == "true")
	paused := (pausedStr == "true")
	var autoDownload *bool // Unset means false.
	if autoDownloadStr != "" {
		b := autoDownloadStr == "true"
		autoDownload = &b
	}

	// Model conversions (newline-separated, not space-separated)
	filters, err := parsing.ParseFilterOps(splitNonEmptyLines(filtersStr), true)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid download filters %q: %v", filtersStr, err), http.StatusBadRequest)
		return nil
	}
	moveOps, err := parsing.ParseMetaFilterMoveOps(splitNonEmptyLines(moveOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid move ops %q: %v", moveOpsStr, err), http.StatusBadRequest)
		return nil
	}

	// Codec preferences.
	ytdlpPreferredVideoCodecs, err := validation.ValidatePreferredVideoCodecs(splitNonEmptyLines(ytdlpPreferredVideoCodecsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid preferred video codecs %q: %v", ytdlpPreferredVideoCodecsStr, err), http.StatusBadRequest)
		return nil
	}
	ytdlpPreferredAudioCodecs, err := validation.ValidatePreferredAudioCodecs(splitNonEmptyLines(ytdlpPreferredAudioCodecsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid preferred audio codecs %q: %v", ytdlpPreferredAudioCodecsStr, err), http.StatusBadRequest)
		return nil
	}

	// Return Settings.
	return &models.Settings{
		JSONDir:  jDir,
		VideoDir: vDir,

		Concurrency:               maxConcurrency,
		CookiesFromBrowser:        cookiesFromBrowser,
		CrawlFreq:                 crawlFreq,
		ExternalDownloader:        externalDownloader,
		ExternalDownloaderArgs:    externalDownloaderArgs,
		MaxFilesize:               maxFilesize,
		Retries:                   retries,
		UseGlobalCookies:          &useGlobalCookies,
		Paused:                    paused,
		AutoDownload:              autoDownload,
		YtdlpOutputExt:            ytdlpOutExt,
		ExtraYTDLPVideoArgs:       extraYtdlpVideoArgs,
		ExtraYTDLPMetaArgs:        extraYtdlpMetaArgs,
		ExtraYTDLPCrawlArgs:       extraYtdlpCrawlArgs,
		YTDLPPreferredVideoCodecs: ytdlpPreferredVideoCodecs,
		YTDLPPreferredAudioCodecs: ytdlpPreferredAudioCodecs,

		Filters:              filters,
		FilterFile:           filterFile,
		MetaFilterMoveOps:    moveOps,
		MetaFilterMoveOpFile: moveOpFile,

		FromDate: fromDate,
		ToDate:   toDate,
	}
}

// getMetarrArgsStrings retrieves MetarrArgs model strings, converting where needed.
func getMetarrArgsStrings(w http.ResponseWriter, r *http.Request) *models.MetarrArgs {
	// -- Initialize --
	// Strings not needing validation.
	outExt := r.FormValue(jsonkeys.MetarrOutputExt)
	filenameOpsFile := r.FormValue(jsonkeys.MetarrFilenameOpsFile)
	filteredFilenameOpsFile := r.FormValue(jsonkeys.MetarrFilteredFilenameOpsFile)
	metaOpsFile := r.FormValue(jsonkeys.MetarrMetaOpsFile)
	filteredMetaOpsFile := r.FormValue(jsonkeys.MetarrFilteredMetaOpsFile)
	extraFFmpegArgs := r.FormValue(jsonkeys.MetarrExtraFFmpegArgs)

	// Strings requiring validation.
	renameStyle := r.FormValue(jsonkeys.MetarrRenameStyle)
	minFreeMem := r.FormValue(jsonkeys.MetarrMinFreeMem)
	transcodeGPUStr := r.FormValue(jsonkeys.MetarrGPU)
	transcodeGPUNodeStr := r.FormValue(jsonkeys.MetarrGPUNode)
	outputDir := r.FormValue(jsonkeys.MetarrOutputDirectory)
	metaPurgeStr := r.FormValue(jsonkeys.MetarrMetaPurge)
	transcodeVideoFilterStr := r.FormValue(jsonkeys.MetarrTranscodeVideoFilter)
	transcodeCodecStr := r.FormValue(jsonkeys.MetarrVideoCodecs)
	transcodeAudioCodecStr := r.FormValue(jsonkeys.MetarrAudioCodecs)
	transcodeQualityStr := r.FormValue(jsonkeys.MetarrTranscodeQuality)

	// Ints.
	maxConcurrencyStr := r.FormValue(jsonkeys.MetarrConcurrency)
	maxCPUStr := r.FormValue(jsonkeys.MetarrMaxCPU)

	// Models.
	filenameOpsStr := r.FormValue(jsonkeys.MetarrFilenameOps)
	filteredFilenameOpsStr := r.FormValue(jsonkeys.MetarrFilteredFilenameOps)
	filteredMetaOpsStr := r.FormValue(jsonkeys.MetarrFilteredMetaOps)
	metaOpsStr := r.FormValue(jsonkeys.MetarrMetaOps)

	// -- Validation --
	//Strings.
	if err := validation.ValidateRenameFlag(renameStyle); err != nil {
		http.Error(w, fmt.Sprintf("invalid rename style %q: %v", renameStyle, err), http.StatusBadRequest)
		return nil
	}
	if _, err := sharedvalidation.ValidateMinFreeMem(minFreeMem); err != nil {
		http.Error(w, fmt.Sprintf("invalid min free mem %q: %v", minFreeMem, err), http.StatusBadRequest)
		return nil
	}
	transcodeGPU, err := validation.ValidateGPUAcceleration(transcodeGPUStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid GPU type %q: %v", transcodeGPUStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeGPUNode, err := sharedvalidation.ValidateAccelTypeDeviceNode(transcodeGPU, transcodeGPUNodeStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid GPU node path %q for acceleration type %q: %v", transcodeGPUNodeStr, transcodeGPU, err), http.StatusBadRequest)
		return nil
	}
	transcodeVideoFilter, err := validation.ValidateTranscodeVideoFilter(transcodeVideoFilterStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video filter string %q: %v", transcodeVideoFilterStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeVideoCodecs, err := validation.ValidateVideoTranscodeCodecSlice(splitNonEmptyLines(transcodeCodecStr), transcodeGPU)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid video codec string %q: %v", transcodeCodecStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeAudioCodecs, err := validation.ValidateAudioTranscodeCodecSlice(splitNonEmptyLines(transcodeAudioCodecStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid audio codec string %q: %v", transcodeAudioCodecStr, err), http.StatusBadRequest)
		return nil
	}
	transcodeQuality, err := sharedvalidation.ValidateTranscodeQuality(transcodeQualityStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid transcode quality string %q: %v", transcodeQualityStr, err), http.StatusBadRequest)
		return nil
	}
	if err := validation.CheckDirectory(outputDir, sharedtemplates.AllTemplatesMap); err != nil {
		http.Error(w, fmt.Sprintf("cannot get output directories. Input string %q. Error: %v", outputDir, err), http.StatusBadRequest)
		return nil
	}
	metaPurge, err := validation.ValidatePurgeMetafiles(metaPurgeStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}

	// Integers & floats.
	maxConcurrency := 0
	if maxConcurrencyStr != "" {
		maxConcurrency, err = strconv.Atoi(maxConcurrencyStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert max concurrency string %q: %v", maxConcurrencyStr, err), http.StatusBadRequest)
			return nil
		}
		maxConcurrency = max(maxConcurrency, 0)
	}

	maxCPU := 0.00
	if maxCPUStr != "" {
		maxCPU, err = strconv.ParseFloat(maxCPUStr, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to convert max CPU limit string %q: %v", maxCPUStr, err), http.StatusBadRequest)
			return nil
		}
	}

	// Models (newline-separated, not space-separated).
	filenameOps, err := parsing.ParseFilenameOps(splitOpLines(filenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filename ops %q: %v", filenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredFilenameOps, err := parsing.ParseFilteredFilenameOps(splitOpLines(filteredFilenameOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filtered filename ops %q: %v", filteredFilenameOpsStr, err), http.StatusBadRequest)
		return nil
	}
	metaOps, err := parsing.ParseMetaOps(splitOpLines(metaOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid meta ops %q: %v", metaOpsStr, err), http.StatusBadRequest)
		return nil
	}
	filteredMetaOps, err := parsing.ParseFilteredMetaOps(splitOpLines(filteredMetaOpsStr))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid filtered meta ops %q: %v", filteredMetaOpsStr, err), http.StatusBadRequest)
		return nil
	}

	// Return MetarrArgs.
	return &models.MetarrArgs{
		OutputExt:               outExt,
		FilenameOps:             filenameOps,
		FilenameOpsFile:         filenameOpsFile,
		FilteredFilenameOps:     filteredFilenameOps,
		FilteredFilenameOpsFile: filteredFilenameOpsFile,
		RenameStyle:             renameStyle,
		MetaOps:                 metaOps,
		MetaOpsFile:             metaOpsFile,
		FilteredMetaOps:         filteredMetaOps,
		FilteredMetaOpsFile:     filteredMetaOpsFile,
		OutputDir:               outputDir,
		MetaPurge:               metaPurge,
		Concurrency:             maxConcurrency,
		MaxCPU:                  maxCPU,
		MinFreeMem:              minFreeMem,
		TranscodeGPU:            transcodeGPU,
		TranscodeGPUNode:        transcodeGPUNode,
		TranscodeVideoFilter:    transcodeVideoFilter,
		TranscodeVideoCodecs:    transcodeVideoCodecs,
		TranscodeAudioCodecs:    transcodeAudioCodecs,
		TranscodeQuality:        transcodeQuality,
		ExtraFFmpegArgs:         extraFFmpegArgs,
	}
}

// splitOpLines splits operation input into non-blank lines, keeping trailing spaces
// since an operation's value may end in one.
func splitOpLines(s string) []string {
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = file.TrimOpLine(line); strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

// parseOverrideLines parses the Download As dialog's ops into a video's overrides.
func parseOverrideLines(l *models.OverrideLines) (o *models.VideoOverrides, err error) {
	o = &models.VideoOverrides{
		Filename:  strings.TrimSpace(l.Filename),
		VideoDir:  strings.TrimSpace(l.VideoDir),
		JSONDir:   strings.TrimSpace(l.JSONDir),
		OutputDir: strings.TrimSpace(l.OutputDir),
	}
	if err := validation.CheckDirectory(o.VideoDir, sharedtemplates.TubarrTemplateTags); err != nil {
		return nil, fmt.Errorf("invalid video directory %q: %w", o.VideoDir, err)
	}
	if err := validation.CheckDirectory(o.JSONDir, sharedtemplates.TubarrTemplateTags); err != nil {
		return nil, fmt.Errorf("invalid JSON directory %q: %w", o.JSONDir, err)
	}
	if err := validation.CheckDirectory(o.OutputDir, sharedtemplates.AllTemplatesMap); err != nil {
		return nil, fmt.Errorf("invalid output directory %q: %w", o.OutputDir, err)
	}
	if o.MetaOps, err = parsing.ParseMetaOps(splitOpLines(l.MetaOps)); err != nil {
		return nil, fmt.Errorf("invalid meta ops: %w", err)
	}
	if o.FilteredMetaOps, err = parsing.ParseFilteredMetaOps(splitOpLines(l.FilteredMetaOps)); err != nil {
		return nil, fmt.Errorf("invalid filtered meta ops: %w", err)
	}
	if o.FilenameOps, err = parsing.ParseFilenameOps(splitOpLines(l.FilenameOps)); err != nil {
		return nil, fmt.Errorf("invalid filename ops: %w", err)
	}
	if o.FilteredFilenameOps, err = parsing.ParseFilteredFilenameOps(splitOpLines(l.FilteredFilenameOps)); err != nil {
		return nil, fmt.Errorf("invalid filtered filename ops: %w", err)
	}
	return o, nil
}

// splitNonEmptyLines splits a string by newlines and filters out empty lines.
func splitNonEmptyLines(s string) []string {
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))

	// Normalize lines and append non-empty ones.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
