package models

type ChannelSettings struct {
	CookieSource           string      `json:"cookie_source"`
	CrawlFreq              int         `json:"crawl_freq"`
	Filters                []DLFilters `json:"filters"`
	Retries                int         `json:"download_retries"`
	ExternalDownloader     string      `json:"external_downloader"`
	ExternalDownloaderArgs string      `json:"external_downloader_args"`
	Concurrency            int         `json:"max_concurrency"`
}

type DLFilters struct {
	Field string `json:"filter_field"`
	Type  string `json:"filter_type"`
	Value string `json:"filter_value"`
}

type MetarrArgs struct {
	FilenameReplaceSfx string   `json:"filename_replace_suffix"`
	RenameStyle        string   `json:"rename_style"`
	FileDatePfx        string   `json:"filename_date_prefix"`
	MetaOps            []string `json:"meta_ops"`
}
