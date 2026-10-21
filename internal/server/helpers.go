package server

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/auth"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/domain/logger"
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

// writeJSON sends v as a JSON response with status, logging a failure to encode it unless the client left.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil && r.Context().Err() == nil {
		logger.Pl.E("Failed to encode response to %s: %v", r.URL.Path, err)
	}
}

// writeText sends msg as a plain text response with status, logging a failure to write it.
func writeText(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	if _, err := io.WriteString(w, msg); err != nil {
		logger.Pl.E("Failed to write response message: %v", err)
	}
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
	ConfigFile        *configFileResponse            `json:"config_file,omitempty"`
}

// configFileResponse lists the settings a channel's config file sets, which replace the saved ones each run.
type configFileResponse struct {
	Settings    []string            `json:"settings"`               // The channel's.
	URLSettings map[string][]string `json:"url_settings,omitempty"` // Each URL's, by URL.
	Error       string              `json:"error,omitempty"`        // Why the file couldn't be read.
}

// newConfigFileResponse reads the settings the config file at path sets.
func newConfigFileResponse(path string) *configFileResponse {
	input, err := parsing.LoadChannelInputFile(path)
	if err != nil {
		return &configFileResponse{Error: err.Error()}
	}
	resp := &configFileResponse{
		Settings:    append(parsing.GivenKeys(&input.SettingsInput), parsing.GivenKeys(&input.MetarrArgsInput)...),
		URLSettings: map[string][]string{},
	}
	for u, override := range input.URLSettings {
		var given []string
		if override.Settings != nil {
			given = parsing.GivenKeys(override.Settings)
		}
		if override.Metarr != nil {
			given = append(given, parsing.GivenKeys(override.Metarr)...)
		}
		if len(given) > 0 {
			resp.URLSettings[u] = given
		}
	}
	return resp
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
			return nil, fmt.Errorf("a notification is for channel URL %q, which this channel doesn't have", n.ChannelURL)
		}
	}
	return notifications, nil
}

// notificationEscaper escapes notification fields for parsing.ParseNotifications, leaving colons so URLs stay readable.
var notificationEscaper = strings.NewReplacer(`\`, `\\`, `|`, `\|`)

// notificationLine renders a notification as parseNotificationLines reads it.
func notificationLine(n *models.Notification) string {
	esc := notificationEscaper.Replace
	if n.ChannelURL != "" {
		return esc(n.ChannelURL) + "|" + esc(n.NotifyURL)
	}
	return esc(n.NotifyURL)
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

// channelForm is the add or edit channel form, parsed and validated.
type channelForm struct {
	name          string
	urls          []string
	renames       map[string]string // Saved URL to its new address, for URLs edited in place.
	authMap       map[string]*models.ChannelAccessDetails
	urlSettings   map[string]urlSettings
	notifications []*models.Notification
	configFile    string
	settings      *models.Settings
	metarr        *models.MetarrArgs
}

// parseChannelForm parses and validates the add or edit channel form.
func parseChannelForm(r *http.Request) (*channelForm, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form data: %w", err)
	}
	blankWhitespaceValues(r)

	f := &channelForm{name: r.FormValue("name"), urls: strings.Fields(r.FormValue("urls"))}
	for _, u := range f.urls {
		if err := validation.ValidateChannelURL(u); err != nil {
			return nil, err
		}
	}
	var err error
	if f.settings, err = settingsFromForm(r); err != nil {
		return nil, err
	}
	if f.metarr, err = metarrArgsFromForm(r); err != nil {
		return nil, err
	}
	authDetails, err := splitAuthDetails(r.FormValue("auth_details"))
	if err != nil {
		return nil, err
	}
	if f.authMap, err = auth.ParseAuthDetails(r.FormValue("username"), r.FormValue("password"), r.FormValue("login_url"), authDetails, f.urls, false); err != nil {
		return nil, fmt.Errorf("invalid authentication details: %w", err)
	}
	if f.urlSettings, err = parseURLSettings(r.FormValue("url_settings")); err != nil {
		return nil, err
	}
	if f.notifications, err = parseNotificationLines(r.FormValue("notifications"), f.urls); err != nil {
		return nil, err
	}
	if f.configFile, err = formConfigFile(r); err != nil {
		return nil, err
	}
	if v := r.FormValue("url_renames"); v != "" {
		if err := json.Unmarshal([]byte(v), &f.renames); err != nil {
			return nil, fmt.Errorf("invalid URL renames: %w", err)
		}
		for from, to := range f.renames {
			if !slices.Contains(f.urls, to) {
				return nil, fmt.Errorf("channel URL %q is renamed to %q, which isn't one of the channel's URLs", from, to)
			}
		}
	}
	return f, nil
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
			out.Settings = &models.Settings{}
			if err := parsing.ApplySettingsInput(out.Settings, in.Settings.input()); err != nil {
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

// syncChannelURLs makes the channel's URLs match the edit form, keeping the manual downloads URL.
func syncChannelURLs(cs contracts.ChannelStore, channelID int64, existing []*models.ChannelURL, f *channelForm, now time.Time) error {
	existingURLMap := make(map[string]*models.ChannelURL, len(existing))
	for _, urlModel := range existing {
		existingURLMap[urlModel.URL] = urlModel
	}

	// URLs edited in place keep their row, so their settings and videos stay with them.
	for from, to := range f.renames {
		urlModel, exists := existingURLMap[from]
		if _, taken := existingURLMap[to]; !exists || urlModel.IsManual || taken {
			continue
		}
		if err := cs.RenameChannelURL(urlModel.ID, to); err != nil {
			return err
		}
		delete(existingURLMap, from)
		urlModel.URL = to
		existingURLMap[to] = urlModel
	}

	// Delete URLs no longer listed. The manual downloads URL is never listed in the edit form, so it's kept.
	for existingURL, urlModel := range existingURLMap {
		if !urlModel.IsManual && !slices.Contains(f.urls, existingURL) {
			if err := cs.DeleteChannelURL(urlModel.ID); err != nil {
				return fmt.Errorf("failed to delete URL %q: %w", existingURL, err)
			}
		}
	}

	// Add or update listed URLs.
	for _, u := range f.urls {
		var parsedUsername, parsedPassword, parsedLoginURL string
		if a, exists := f.authMap[u]; exists {
			parsedUsername, parsedPassword, parsedLoginURL = a.Username, a.Password, a.LoginURL
		}
		urlSettings := f.urlSettings[u]

		if existingURLModel, exists := existingURLMap[u]; exists {
			// A blank password keeps the saved one, as passwords aren't sent to the edit page. Clearing the ciphertext
			// makes a new password get encrypted, and a cleared login get stored blank.
			if parsedPassword != "" || parsedUsername == "" {
				existingURLModel.Password = parsedPassword
				existingURLModel.EncryptedPassword = ""
			}
			existingURLModel.Username = parsedUsername
			existingURLModel.LoginURL = parsedLoginURL
			existingURLModel.UpdatedAt = now
			existingURLModel.ChanURLSettings = urlSettings.Settings
			existingURLModel.ChanURLMetarrArgs = urlSettings.Metarr

			if err := cs.UpdateChannelURLSettings(existingURLModel); err != nil {
				return fmt.Errorf("failed to update URL %q: %w", u, err)
			}
			continue
		}

		chanURL := &models.ChannelURL{
			URL:               u,
			Username:          parsedUsername,
			Password:          parsedPassword,
			LoginURL:          parsedLoginURL,
			ChanURLSettings:   urlSettings.Settings,
			ChanURLMetarrArgs: urlSettings.Metarr,
			LastScan:          now,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if _, err := cs.AddChannelURL(channelID, chanURL, false); err != nil {
			return fmt.Errorf("failed to add URL %q: %w", u, err)
		}
	}
	return nil
}

// replaceNotifications deletes and re-sets notifications fresh.
func replaceNotifications(cs contracts.ChannelStore, channelID int64, f *channelForm) error {
	// Replace notifications.
	existingNotifications, err := cs.GetNotifyURLs(channelID)
	if err != nil {
		return fmt.Errorf("failed to fetch existing notifications: %w", err)
	}
	existingNotificationURLs := make([]string, 0, len(existingNotifications))
	for _, n := range existingNotifications {
		if n.NotifyURL != "" {
			existingNotificationURLs = append(existingNotificationURLs, n.NotifyURL)
		}
	}
	if len(existingNotificationURLs) > 0 {
		if err := cs.DeleteNotifyURLs(channelID, existingNotificationURLs); err != nil {
			return fmt.Errorf("failed to delete existing notifications: %w", err)
		}
	}
	if len(f.notifications) > 0 {
		if err := cs.AddNotifyURLs(channelID, f.notifications); err != nil {
			return fmt.Errorf("failed to add notification URLs: %w", err)
		}
	}

	return nil
}

// urlSettings are a channel URL's parsed custom settings.
type urlSettings struct {
	Settings *models.Settings
	Metarr   *models.MetarrArgs
}

// input returns the per-URL Settings sent, as settings input.
func (in *settingsInput) input() *models.SettingsInput {
	return &models.SettingsInput{
		VideoDir:                  in.VideoDir,
		JSONDir:                   in.JSONDir,
		CookiesFromBrowser:        in.CookiesFromBrowser,
		ExternalDownloader:        in.ExternalDownloader,
		ExternalDownloaderArgs:    in.ExternalDownloaderArgs,
		MaxFilesize:               in.MaxFilesize,
		YTDLPOutputExt:            in.YtdlpOutputExt,
		FromDate:                  in.FromDate,
		ToDate:                    in.ToDate,
		YTDLPPreferredVideoCodecs: listInput(in.YTDLPPreferredVideoCodecs),
		YTDLPPreferredAudioCodecs: listInput(in.YTDLPPreferredAudioCodecs),
		UseGlobalCookies:          in.UseGlobalCookies,
		AutoDownload:              in.AutoDownload,
		DLFilters:                 linesInput(in.Filters, splitNonEmptyLines),
		DLFilterFile:              in.FilterFile,
		MoveOps:                   linesInput(in.MoveOps, splitNonEmptyLines),
		MoveOpFile:                in.MoveOpsFile,
		ExtraYTDLPVideoArgs:       in.ExtraYTDLPVideoArgs,
		ExtraYTDLPMetaArgs:        in.ExtraYTDLPMetaArgs,
		ExtraYTDLPCrawlArgs:       in.ExtraYTDLPCrawlArgs,
		CrawlFreq:                 in.CrawlFreq,
		Concurrency:               in.MaxConcurrency,
		Retries:                   in.DownloadRetries,
		Pause:                     in.Paused,
	}
}

// input returns the MetarrArgs sent, as Metarr args input.
func (in *metarrArgsInput) input() *models.MetarrArgsInput {
	return &models.MetarrArgsInput{
		OutDir:                  in.OutputDir,
		MetaPurge:               in.MetaPurge,
		MetarrExt:               in.OutputExt,
		RenameStyle:             in.RenameStyle,
		MetaOpsFile:             in.MetaOpsFile,
		FilteredMetaOpsFile:     in.FilteredMetaOpsFile,
		FilenameOpsFile:         in.FilenameOpsFile,
		FilteredFilenameOpsFile: in.FilteredFilenameOpsFile,
		MetaOps:                 linesInput(in.MetaOps, splitOpLines),
		FilenameOps:             linesInput(in.FilenameOps, splitOpLines),
		FilteredMetaOps:         linesInput(in.FilteredMetaOps, splitOpLines),
		FilteredFilenameOps:     linesInput(in.FilteredFilenameOps, splitOpLines),
		TranscodeGPU:            in.GPU,
		TranscodeGPUNode:        in.GPUNode,
		TranscodeQuality:        in.TranscodeQuality,
		TranscodeVideoFilter:    in.TranscodeVideoFilter,
		TranscodeVideoCodec:     linesInput(in.VideoCodecs, splitNonEmptyLines),
		TranscodeAudioCodec:     linesInput(in.AudioCodecs, splitNonEmptyLines),
		MinFreeMem:              in.MinFreeMem,
		MaxCPU:                  in.MaxCPU,
		MetarrConcurrency:       in.Concurrency,
		ExtraFFmpegArgs:         in.ExtraFFmpegArgs,
	}
}

// listInput returns a list sent, or nil if it wasn't sent.
func listInput(v []string) *[]string {
	if v == nil {
		return nil
	}
	return &v
}

// linesInput returns a value sent split into lines by split, or nil if it wasn't sent.
func linesInput(v *string, split func(string) []string) *[]string {
	if v == nil {
		return nil
	}
	lines := split(*v)
	return &lines
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

// parseMetarrArgsInput validates MetarrArgs from the frontend (a URL's, or a video's in Download As).
func parseMetarrArgsInput(in *metarrArgsInput) (*models.MetarrArgs, error) {
	m := &models.MetarrArgs{}
	if err := parsing.ApplyMetarrArgsInput(m, in.input()); err != nil {
		return nil, err
	}
	return m, nil
}

// formInt returns the form value key as an int, or 0 if it's blank.
func formInt(r *http.Request, key string) (int, error) {
	if v := r.FormValue(key); v != "" {
		return strconv.Atoi(v)
	}
	return 0, nil
}

// formString returns the form value key, as settings input. The channel form sends every field, so a blank one clears it.
func formString(r *http.Request, key string) *string {
	v := r.FormValue(key)
	return &v
}

// settingsFromForm parses and validates the channel form's Settings.
func settingsFromForm(r *http.Request) (*models.Settings, error) {
	concurrency, err := formInt(r, jsonkeys.SettingsMaxConcurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert max concurrency: %w", err)
	}
	crawlFreq, err := formInt(r, jsonkeys.SettingsCrawlFreq)
	if err != nil {
		return nil, fmt.Errorf("failed to convert crawl frequency: %w", err)
	}
	retries, err := formInt(r, jsonkeys.SettingsDownloadRetries)
	if err != nil {
		return nil, fmt.Errorf("failed to convert retries: %w", err)
	}
	useGlobalCookies := r.FormValue(jsonkeys.SettingsUseGlobalCookies) == "true"
	paused := r.FormValue(jsonkeys.SettingsPaused) == "true"

	in := &models.SettingsInput{
		VideoDir:                  formString(r, jsonkeys.SettingsVideoDirectory),
		JSONDir:                   formString(r, jsonkeys.SettingsJSONDirectory),
		CookiesFromBrowser:        formString(r, jsonkeys.SettingsCookiesFromBrowser),
		ExternalDownloader:        formString(r, jsonkeys.SettingsExternalDownloader),
		ExternalDownloaderArgs:    formString(r, jsonkeys.SettingsExternalDownloaderArgs),
		MaxFilesize:               formString(r, jsonkeys.SettingsMaxFilesize),
		YTDLPOutputExt:            formString(r, jsonkeys.SettingsYtdlpOutputExt),
		FromDate:                  formString(r, jsonkeys.SettingsFromDate),
		ToDate:                    formString(r, jsonkeys.SettingsToDate),
		YTDLPPreferredVideoCodecs: linesInput(formString(r, jsonkeys.SettingsYTDLPPreferredVideoCodecs), splitNonEmptyLines),
		YTDLPPreferredAudioCodecs: linesInput(formString(r, jsonkeys.SettingsYTDLPPreferredAudioCodecs), splitNonEmptyLines),
		UseGlobalCookies:          &useGlobalCookies,
		DLFilters:                 linesInput(formString(r, jsonkeys.SettingsFilters), splitNonEmptyLines),
		DLFilterFile:              formString(r, jsonkeys.SettingsFilterFile),
		MoveOps:                   linesInput(formString(r, jsonkeys.SettingsMoveOps), splitNonEmptyLines),
		MoveOpFile:                formString(r, jsonkeys.SettingsMoveOpsFile),
		ExtraYTDLPVideoArgs:       formString(r, jsonkeys.SettingsExtraYtdlpVideoArgs),
		ExtraYTDLPMetaArgs:        formString(r, jsonkeys.SettingsExtraYtdlpMetaArgs),
		ExtraYTDLPCrawlArgs:       formString(r, jsonkeys.SettingsExtraYtdlpCrawlArgs),
		CrawlFreq:                 &crawlFreq,
		Concurrency:               &concurrency,
		Retries:                   &retries,
		Pause:                     &paused,
	}
	if v := r.FormValue(jsonkeys.SettingsAutoDownload); v != "" { // Unset means false.
		autoDownload := v == "true"
		in.AutoDownload = &autoDownload
	}

	s := &models.Settings{}
	if err := parsing.ApplyChannelSettingsInput(s, in); err != nil {
		return nil, err
	}
	return s, nil
}

// metarrArgsFromForm parses and validates the channel form's MetarrArgs.
func metarrArgsFromForm(r *http.Request) (*models.MetarrArgs, error) {
	concurrency, err := formInt(r, jsonkeys.MetarrConcurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert max concurrency: %w", err)
	}
	var maxCPU float64
	if v := r.FormValue(jsonkeys.MetarrMaxCPU); v != "" {
		if maxCPU, err = strconv.ParseFloat(v, 64); err != nil {
			return nil, fmt.Errorf("failed to convert max CPU limit string %q: %w", v, err)
		}
	}

	in := &models.MetarrArgsInput{
		OutDir:                  formString(r, jsonkeys.MetarrOutputDirectory),
		MetaPurge:               formString(r, jsonkeys.MetarrMetaPurge),
		MetarrExt:               formString(r, jsonkeys.MetarrOutputExt),
		RenameStyle:             formString(r, jsonkeys.MetarrRenameStyle),
		MetaOpsFile:             formString(r, jsonkeys.MetarrMetaOpsFile),
		FilteredMetaOpsFile:     formString(r, jsonkeys.MetarrFilteredMetaOpsFile),
		FilenameOpsFile:         formString(r, jsonkeys.MetarrFilenameOpsFile),
		FilteredFilenameOpsFile: formString(r, jsonkeys.MetarrFilteredFilenameOpsFile),
		MetaOps:                 linesInput(formString(r, jsonkeys.MetarrMetaOps), splitOpLines),
		FilenameOps:             linesInput(formString(r, jsonkeys.MetarrFilenameOps), splitOpLines),
		FilteredMetaOps:         linesInput(formString(r, jsonkeys.MetarrFilteredMetaOps), splitOpLines),
		FilteredFilenameOps:     linesInput(formString(r, jsonkeys.MetarrFilteredFilenameOps), splitOpLines),
		TranscodeGPU:            formString(r, jsonkeys.MetarrGPU),
		TranscodeGPUNode:        formString(r, jsonkeys.MetarrGPUNode),
		TranscodeQuality:        formString(r, jsonkeys.MetarrTranscodeQuality),
		TranscodeVideoFilter:    formString(r, jsonkeys.MetarrTranscodeVideoFilter),
		TranscodeVideoCodec:     linesInput(formString(r, jsonkeys.MetarrVideoCodecs), splitNonEmptyLines),
		TranscodeAudioCodec:     linesInput(formString(r, jsonkeys.MetarrAudioCodecs), splitNonEmptyLines),
		MinFreeMem:              formString(r, jsonkeys.MetarrMinFreeMem),
		MaxCPU:                  &maxCPU,
		MetarrConcurrency:       &concurrency,
		ExtraFFmpegArgs:         formString(r, jsonkeys.MetarrExtraFFmpegArgs),
	}

	m := &models.MetarrArgs{}
	if err := parsing.ApplyMetarrArgsInput(m, in); err != nil {
		return nil, err
	}
	return m, nil
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

// parseOverrideLines parses the Download As dialog's ops and other Metarr args into a video's overrides.
func parseOverrideLines(l *models.OverrideLines, m *metarrArgsInput) (o *models.VideoOverrides, err error) {
	o = &models.VideoOverrides{
		Filename:       strings.TrimSpace(l.Filename),
		VideoDir:       strings.TrimSpace(l.VideoDir),
		JSONDir:        strings.TrimSpace(l.JSONDir),
		Thumbnail:      strings.TrimSpace(l.Thumbnail),
		StripThumbnail: l.StripThumbnail,
	}
	if err := validation.ValidateThumbnail(o.Thumbnail, o.StripThumbnail); err != nil {
		return nil, err
	}
	if m != nil {
		args, err := parseMetarrArgsInput(m)
		if err != nil {
			return nil, fmt.Errorf("invalid post-processing options: %w", err)
		}
		o.MetarrArgs = *args
	}
	o.OutputDir = strings.TrimSpace(l.OutputDir)
	// Downloads need these to exist, so say so now rather than fail the download.
	if err := validation.CheckDirectory(o.VideoDir, sharedtemplates.TubarrTemplateTags); err != nil {
		return nil, fmt.Errorf("invalid video directory %q: %w", o.VideoDir, err)
	}
	if err := validation.CheckDestination(o.VideoDir); err != nil {
		return nil, fmt.Errorf("invalid video directory %q: %w", o.VideoDir, err)
	}
	if err := validation.CheckDirectory(o.JSONDir, sharedtemplates.TubarrTemplateTags); err != nil {
		return nil, fmt.Errorf("invalid JSON directory %q: %w", o.JSONDir, err)
	}
	if err := validation.CheckDestination(o.JSONDir); err != nil {
		return nil, fmt.Errorf("invalid JSON directory %q: %w", o.JSONDir, err)
	}
	if err := validation.CheckDirectory(o.OutputDir, sharedtemplates.AllTemplatesMap); err != nil {
		return nil, fmt.Errorf("invalid output directory %q: %w", o.OutputDir, err)
	}
	if err := validation.CheckDestination(o.OutputDir); err != nil {
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
	// Checked as saved settings are, so they're normalized the same way. It's used straight away, so every file and
	// folder it names must be there.
	if err := validation.ValidateMetarrArgsModel(&o.MetarrArgs, true); err != nil {
		return nil, fmt.Errorf("invalid post-processing options: %w", err)
	}
	return o, nil
}

// checkOverridesFit returns an error if Download As options, with blank ones using the video's channel URL's as Metarr
// gets them, can't work together.
func checkOverridesFit(cs contracts.ChannelStore, c *models.Channel, channelURLID int64, o *models.VideoOverrides) error {
	m := o.MetarrArgs
	if cu, err := app.ManualChannelURL(cs, c, channelURLID); err == nil && cu.ChanURLMetarrArgs != nil {
		u := cu.ChanURLMetarrArgs
		m.OutputExt = cmp.Or(m.OutputExt, u.OutputExt)
		m.TranscodeGPU = cmp.Or(m.TranscodeGPU, u.TranscodeGPU)
		m.TranscodeGPUNode = cmp.Or(m.TranscodeGPUNode, u.TranscodeGPUNode)
		if len(m.TranscodeVideoCodecs) == 0 {
			m.TranscodeVideoCodecs = u.TranscodeVideoCodecs
		}
		if len(m.TranscodeAudioCodecs) == 0 {
			m.TranscodeAudioCodecs = u.TranscodeAudioCodecs
		}
	}
	if err := validation.ValidateMetarrArgsCombination(&m); err != nil {
		return fmt.Errorf("invalid post-processing options: %w", err)
	}
	return nil
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
