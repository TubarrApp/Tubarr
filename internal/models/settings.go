package models

import (
	"cmp"
	"slices"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedmodels"
)

// DomainLimit holds a domain's concurrent download and crawl limits (0 = unlimited).
type DomainLimit struct {
	Download int `json:"download"`
	Crawl    int `json:"crawl"`
}

// Settings are the primary settings for a channel, affecting videos belonging to it.
type Settings struct {
	// Configurations.
	Concurrency int `json:"max_concurrency"`

	// Download-related operations.
	CookiesFromBrowser        string   `json:"cookies_from_browser"`
	CrawlFreq                 int      `json:"crawl_freq"`
	ExternalDownloader        string   `json:"external_downloader"`
	ExternalDownloaderArgs    string   `json:"external_downloader_args"`
	MaxFilesize               string   `json:"max_filesize"`
	Retries                   int      `json:"download_retries"`
	UseGlobalCookies          *bool    `json:"use_global_cookies,omitempty"`
	YtdlpOutputExt            string   `json:"ytdlp_output_ext"`
	YTDLPPreferredVideoCodecs []string `json:"ytdlp_preferred_video_codecs"`
	YTDLPPreferredAudioCodecs []string `json:"ytdlp_preferred_audio_codecs"`

	// Custom args
	ExtraYTDLPVideoArgs string `json:"extra_ytdlp_video_args"`
	ExtraYTDLPMetaArgs  string `json:"extra_ytdlp_meta_args"`
	ExtraYTDLPCrawlArgs string `json:"extra_ytdlp_crawl_args"`

	// Metadata operations.
	Filters              []Filters           `json:"filters"`
	FilterFile           string              `json:"filter_file"`
	MetaFilterMoveOps    []MetaFilterMoveOps `json:"move_ops"`
	MetaFilterMoveOpFile string              `json:"move_ops_file"`
	FromDate             string              `json:"from_date"`
	ToDate               string              `json:"to_date"`

	// JSON and video directories.
	JSONDir  string `json:"json_directory"`
	VideoDir string `json:"video_directory"`

	// Channel toggles.
	Paused       bool  `json:"paused"`
	AutoDownload *bool `json:"auto_download,omitempty"` // Nil inherits (channel URLs) or means false (channels).
}

// MetarrArgs are the arguments used when calling the Metarr external program.
type MetarrArgs struct {
	// Metarr file operations.
	OutputExt               string                `json:"metarr_output_ext"`
	FilenameOps             []FilenameOps         `json:"metarr_filename_ops"`
	FilenameOpsFile         string                `json:"metarr_filename_ops_file"`
	FilteredFilenameOps     []FilteredFilenameOps `json:"metarr_filtered_filename_ops"`
	FilteredFilenameOpsFile string                `json:"metarr_filtered_filename_ops_file"`
	RenameStyle             string                `json:"metarr_rename_style"`

	// Metarr metadata operations.
	MetaOps             []MetaOps         `json:"metarr_meta_ops"`
	MetaOpsFile         string            `json:"metarr_meta_ops_file"`
	FilteredMetaOps     []FilteredMetaOps `json:"metarr_filtered_meta_ops"`
	FilteredMetaOpsFile string            `json:"metarr_filtered_meta_ops_file"`

	// Metarr output directories, and metadata files to delete after processing (all, json or nfo).
	OutputDir string `json:"metarr_output_directory"`
	MetaPurge string `json:"metarr_purge_metafile"`

	// Program operations.
	Concurrency int     `json:"metarr_concurrency"`
	MaxCPU      float64 `json:"metarr_max_cpu_usage"`
	MinFreeMem  string  `json:"metarr_min_free_mem"`

	// FFmpeg transcoding operations.
	TranscodeGPU         string   `json:"metarr_transcode_gpu"`
	TranscodeGPUNode     string   `json:"metarr_transcode_gpu_node"`
	TranscodeVideoFilter string   `json:"metarr_transcode_video_filter"`
	TranscodeVideoCodecs []string `json:"metarr_transcode_video_codecs"`
	TranscodeAudioCodecs []string `json:"metarr_transcode_audio_codecs"`
	TranscodeQuality     string   `json:"metarr_transcode_quality"`
	ExtraFFmpegArgs      string   `json:"metarr_extra_ffmpeg_args"`
}

// ChannelAccessDetails holds details related to authentication and cookies.
type ChannelAccessDetails struct {
	Username,
	Password,
	EncryptedPassword,
	LoginURL,
	ChannelURL,
	CookiePath string
}

// FilteredMetaOps allows meta operation entry based on filter matching.
type FilteredMetaOps = sharedmodels.FilteredMetaOps

// FilteredFilenameOps allows file operation entry based on filter matching.
type FilteredFilenameOps = sharedmodels.FilteredFilenameOps

// Filters are used to filter in or out videos (download, or operations) by metafields.
type Filters = sharedmodels.Filters

// MetaFilterMoveOps are used to set an output directory in Metarr based on matching metadata fields.
type MetaFilterMoveOps struct {
	ChannelURL string `json:"move_url_specific"`
	Field      string `json:"move_op_field"`
	FilterType string `json:"move_op_filter_type"`
	Value      string `json:"move_op_value"`
	OutputDir  string `json:"move_op_output_dir"`
}

// Filter returns the move op's filter. Move ops saved before filter types have none, and were always contains.
func (m MetaFilterMoveOps) Filter() Filters {
	return Filters{
		ChannelURL: m.ChannelURL,
		Field:      m.Field,
		FilterType: cmp.Or(m.FilterType, sharedconsts.OpContains),
		Value:      m.Value,
		MustAny:    sharedconsts.OpMust,
	}
}

// FilenameOps are applied to fields by Metarr.
//
// Aliased to gocommon so Metarr and Tubarr share one grammar and one model.
type FilenameOps = sharedmodels.FilenameOps

// MetaOps are applied to fields by Metarr.
type MetaOps = sharedmodels.MetaOps

// ChildSettingsMatchParent checks if the child Settings are empty/mismatch the parent on each entry.
func ChildSettingsMatchParent(parent *Settings, child *Settings) bool {
	if parent == nil || child == nil {
		return parent == child
	}

	// Numeric comparisons
	if child.Concurrency != 0 && child.Concurrency != parent.Concurrency {
		return false
	}
	if child.CrawlFreq != 0 && child.CrawlFreq != parent.CrawlFreq {
		return false
	}
	if child.Retries != 0 && child.Retries != parent.Retries {
		return false
	}

	// String comparisons
	if child.CookiesFromBrowser != "" && child.CookiesFromBrowser != parent.CookiesFromBrowser {
		return false
	}
	if child.ExternalDownloader != "" && child.ExternalDownloader != parent.ExternalDownloader {
		return false
	}
	if child.ExternalDownloaderArgs != "" && child.ExternalDownloaderArgs != parent.ExternalDownloaderArgs {
		return false
	}
	if child.ExtraYTDLPMetaArgs != "" && child.ExtraYTDLPMetaArgs != parent.ExtraYTDLPMetaArgs {
		return false
	}
	if child.ExtraYTDLPVideoArgs != "" && child.ExtraYTDLPVideoArgs != parent.ExtraYTDLPVideoArgs {
		return false
	}
	if child.ExtraYTDLPCrawlArgs != "" && child.ExtraYTDLPCrawlArgs != parent.ExtraYTDLPCrawlArgs {
		return false
	}
	if child.FilterFile != "" && child.FilterFile != parent.FilterFile {
		return false
	}
	if child.FromDate != "" && child.FromDate != parent.FromDate {
		return false
	}
	if child.JSONDir != "" && child.JSONDir != parent.JSONDir {
		return false
	}
	if child.MaxFilesize != "" && child.MaxFilesize != parent.MaxFilesize {
		return false
	}
	if child.MetaFilterMoveOpFile != "" && child.MetaFilterMoveOpFile != parent.MetaFilterMoveOpFile {
		return false
	}
	if child.ToDate != "" && child.ToDate != parent.ToDate {
		return false
	}
	if child.VideoDir != "" && child.VideoDir != parent.VideoDir {
		return false
	}
	if child.YtdlpOutputExt != "" && child.YtdlpOutputExt != parent.YtdlpOutputExt {
		return false
	}

	// Boolean comparisons (pausing is per URL, never inherited).
	if child.Paused {
		return false
	}
	if child.UseGlobalCookies != nil && (parent.UseGlobalCookies == nil || *parent.UseGlobalCookies != *child.UseGlobalCookies) {
		return false
	}
	if child.AutoDownload != nil && (parent.AutoDownload == nil || *parent.AutoDownload != *child.AutoDownload) {
		return false
	}

	// Slice comparisons
	if len(child.Filters) > 0 && !slices.Equal(parent.Filters, child.Filters) {
		return false
	}
	if len(child.MetaFilterMoveOps) > 0 && !slices.Equal(parent.MetaFilterMoveOps, child.MetaFilterMoveOps) {
		return false
	}
	if len(child.YTDLPPreferredVideoCodecs) > 0 && !slices.Equal(parent.YTDLPPreferredVideoCodecs, child.YTDLPPreferredVideoCodecs) {
		return false
	}
	if len(child.YTDLPPreferredAudioCodecs) > 0 && !slices.Equal(parent.YTDLPPreferredAudioCodecs, child.YTDLPPreferredAudioCodecs) {
		return false
	}
	return true
}

// ChildMetarrArgsMatchParent checks if the child MetarrArgs are empty/mismatch the parent on each entry.
func ChildMetarrArgsMatchParent(parent *MetarrArgs, child *MetarrArgs) bool {
	if parent == nil || child == nil {
		return parent == child
	}

	// String comparisons
	if child.OutputExt != "" && child.OutputExt != parent.OutputExt {
		return false
	}
	if child.FilenameOpsFile != "" && child.FilenameOpsFile != parent.FilenameOpsFile {
		return false
	}
	if child.FilteredFilenameOpsFile != "" && child.FilteredFilenameOpsFile != parent.FilteredFilenameOpsFile {
		return false
	}
	if child.RenameStyle != "" && child.RenameStyle != parent.RenameStyle {
		return false
	}
	if child.MetaOpsFile != "" && child.MetaOpsFile != parent.MetaOpsFile {
		return false
	}
	if child.FilteredMetaOpsFile != "" && child.FilteredMetaOpsFile != parent.FilteredMetaOpsFile {
		return false
	}
	if child.OutputDir != "" && child.OutputDir != parent.OutputDir {
		return false
	}
	if child.MetaPurge != "" && child.MetaPurge != parent.MetaPurge {
		return false
	}
	if child.MinFreeMem != "" && child.MinFreeMem != parent.MinFreeMem {
		return false
	}
	if child.TranscodeGPU != "" && child.TranscodeGPU != parent.TranscodeGPU {
		return false
	}
	if child.TranscodeGPUNode != "" && child.TranscodeGPUNode != parent.TranscodeGPUNode {
		return false
	}
	if child.TranscodeVideoFilter != "" && child.TranscodeVideoFilter != parent.TranscodeVideoFilter {
		return false
	}
	if child.TranscodeQuality != "" && child.TranscodeQuality != parent.TranscodeQuality {
		return false
	}
	if child.ExtraFFmpegArgs != "" && child.ExtraFFmpegArgs != parent.ExtraFFmpegArgs {
		return false
	}

	// Numeric comparisons
	if child.Concurrency != 0 && child.Concurrency != parent.Concurrency {
		return false
	}
	if child.MaxCPU != 0 && child.MaxCPU != parent.MaxCPU {
		return false
	}

	// Slice comparisons
	if len(child.FilenameOps) > 0 && !slices.Equal(parent.FilenameOps, child.FilenameOps) {
		return false
	}
	if len(child.FilteredFilenameOps) > 0 && !slices.EqualFunc(parent.FilteredFilenameOps, child.FilteredFilenameOps, filteredFilenameOpsEqual) {
		return false
	}
	if len(child.MetaOps) > 0 && !slices.Equal(parent.MetaOps, child.MetaOps) {
		return false
	}
	if len(child.FilteredMetaOps) > 0 && !slices.EqualFunc(parent.FilteredMetaOps, child.FilteredMetaOps, filteredMetaOpsEqual) {
		return false
	}
	if len(child.TranscodeVideoCodecs) > 0 && !slices.Equal(parent.TranscodeVideoCodecs, child.TranscodeVideoCodecs) {
		return false
	}
	if len(child.TranscodeAudioCodecs) > 0 && !slices.Equal(parent.TranscodeAudioCodecs, child.TranscodeAudioCodecs) {
		return false
	}
	return true
}

// filteredFilenameOpsEqual reports whether two filtered filename ops have the same filters and ops.
func filteredFilenameOpsEqual(a, b FilteredFilenameOps) bool {
	return a.FiltersMatched == b.FiltersMatched &&
		slices.Equal(a.Filters, b.Filters) &&
		slices.Equal(a.FilenameOps, b.FilenameOps)
}

// filteredMetaOpsEqual reports whether two filtered meta ops have the same filters and ops.
func filteredMetaOpsEqual(a, b FilteredMetaOps) bool {
	return a.FiltersMatched == b.FiltersMatched &&
		slices.Equal(a.Filters, b.Filters) &&
		slices.Equal(a.MetaOps, b.MetaOps)
}

// MetarrArgsAllZero checks if every Metarr Args field is its zero value.
func MetarrArgsAllZero(m *MetarrArgs) bool {
	if m == nil {
		return true
	}

	// Metarr file operations.
	if m.OutputExt != "" {
		return false
	}
	if len(m.FilenameOps) > 0 {
		return false
	}
	if m.FilenameOpsFile != "" {
		return false
	}
	if len(m.FilteredFilenameOps) > 0 {
		return false
	}
	if m.FilteredFilenameOpsFile != "" {
		return false
	}
	if m.RenameStyle != "" {
		return false
	}

	// Metarr metadata operations.
	if len(m.MetaOps) > 0 {
		return false
	}
	if m.MetaOpsFile != "" {
		return false
	}
	if len(m.FilteredMetaOps) > 0 {
		return false
	}
	if m.FilteredMetaOpsFile != "" {
		return false
	}

	// Metarr output directories, and metadata purge.
	if m.MetaPurge != "" {
		return false
	}
	if m.OutputDir != "" {
		return false
	}

	// Program operations.
	if m.Concurrency != 0 {
		return false
	}
	if m.MaxCPU != 0.0 {
		return false
	}
	if m.MinFreeMem != "" {
		return false
	}

	// FFmpeg transcoding operations.
	if m.TranscodeGPU != "" {
		return false
	}
	if m.TranscodeGPUNode != "" {
		return false
	}
	if m.TranscodeVideoFilter != "" {
		return false
	}
	if len(m.TranscodeVideoCodecs) > 0 {
		return false
	}
	if len(m.TranscodeAudioCodecs) > 0 {
		return false
	}
	if m.TranscodeQuality != "" {
		return false
	}
	if m.ExtraFFmpegArgs != "" {
		return false
	}

	return true
}

// SettingsAllZero checks if every Settings field is its zero value.
func SettingsAllZero(s *Settings) bool {
	if s == nil {
		return true
	}

	// Configurations.
	if s.Concurrency != 0 {
		return false
	}

	// Download-related operations.
	if s.CookiesFromBrowser != "" {
		return false
	}
	if s.CrawlFreq != 0 {
		return false
	}
	if s.ExternalDownloader != "" {
		return false
	}
	if s.ExternalDownloaderArgs != "" {
		return false
	}
	if s.MaxFilesize != "" {
		return false
	}
	if s.Retries != 0 {
		return false
	}
	if s.UseGlobalCookies != nil && *s.UseGlobalCookies {
		return false
	}
	if s.YtdlpOutputExt != "" {
		return false
	}
	if len(s.YTDLPPreferredVideoCodecs) > 0 {
		return false
	}
	if len(s.YTDLPPreferredAudioCodecs) > 0 {
		return false
	}

	// Custom args.
	if s.ExtraYTDLPVideoArgs != "" {
		return false
	}
	if s.ExtraYTDLPMetaArgs != "" {
		return false
	}
	if s.ExtraYTDLPCrawlArgs != "" {
		return false
	}

	// Metadata operations.
	if len(s.Filters) > 0 {
		return false
	}
	if s.FilterFile != "" {
		return false
	}
	if len(s.MetaFilterMoveOps) > 0 {
		return false
	}
	if s.MetaFilterMoveOpFile != "" {
		return false
	}
	if s.FromDate != "" {
		return false
	}
	if s.ToDate != "" {
		return false
	}

	// JSON and video directories.
	if s.JSONDir != "" {
		return false
	}
	if s.VideoDir != "" {
		return false
	}

	// Channel toggles.
	if s.Paused {
		return false
	}
	if s.AutoDownload != nil {
		return false
	}

	return true
}
