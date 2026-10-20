package scraper

import (
	"cmp"
	"net/url"
	"slices"
	"strings"
	"time"
	"tubarr/internal/domain/logger"

	"golang.org/x/net/publicsuffix"
)

// listedVideo holds what a channel listing shows of a video, which may be nothing beyond its URL.
type listedVideo struct {
	Title        string
	ThumbnailURL string
	UploadDate   time.Time
	Duration     int // Seconds.
	Order        int // Position in the channel's listing, as the site lists it.
}

// listedInOrder returns the URLs of listed videos in the order the channel lists them.
func listedInOrder(videos map[string]*listedVideo) []string {
	urls := make([]string, 0, len(videos))
	for u := range videos {
		urls = append(urls, u)
	}
	slices.SortFunc(urls, func(a, b string) int { return cmp.Compare(videos[a].Order, videos[b].Order) })
	return urls
}

// ytDlpEntry is a yt-dlp playlist entry, which may itself be a playlist (e.g. a channel's "Videos" tab).
type ytDlpEntry struct {
	URL        string  `json:"url"`
	Title      string  `json:"title"`
	Duration   float64 `json:"duration"`
	Timestamp  float64 `json:"timestamp"`
	UploadDate string  `json:"upload_date"` // YYYYMMDD.
	Thumbnail  string  `json:"thumbnail"`
	Thumbnails []struct {
		URL string `json:"url"`
	} `json:"thumbnails"`
	Entries []*ytDlpEntry `json:"entries"`
}

// listed returns what the flat playlist entry shows of the video.
func (e *ytDlpEntry) listed() *listedVideo {
	lv := &listedVideo{
		Title:        e.Title,
		ThumbnailURL: e.Thumbnail,
		Duration:     int(e.Duration),
	}
	// yt-dlp orders thumbnails from worst to best.
	if lv.ThumbnailURL == "" && len(e.Thumbnails) > 0 {
		lv.ThumbnailURL = e.Thumbnails[len(e.Thumbnails)-1].URL
	}
	if t, err := time.Parse("20060102", e.UploadDate); err == nil {
		lv.UploadDate = t
	} else if e.Timestamp > 0 {
		lv.UploadDate = time.Unix(int64(e.Timestamp), 0).UTC()
	}
	return lv
}

// videoEntries returns the entry's leaf entries, descending into nested playlists.
func (e *ytDlpEntry) videoEntries() []*ytDlpEntry {
	if len(e.Entries) == 0 {
		return []*ytDlpEntry{e}
	}
	var out []*ytDlpEntry
	for _, sub := range e.Entries {
		if sub != nil {
			out = append(out, sub.videoEntries()...)
		}
	}
	return out
}

const rumble = "rumble.com"

// getBaseDomain returns the base domain for an inputted URL.
func getBaseDomain(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return publicsuffix.EffectiveTLDPlusOne(u.Hostname())
}

// normalizeURL standardizes URLs for comparison by removing protocol and any trailing slashes.
func normalizeURL(inputURL string) string {
	cleanURL := strings.TrimPrefix(inputURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")
	return strings.TrimSuffix(cleanURL, "/")
}

// isValidRumbleVideoURL returns true only for direct video URLs of the
// form rumble.com/v<slug>.html, excluding channel, user, and listing pages.
func isValidRumbleVideoURL(videoURL string) bool {
	parsed, err := url.Parse(videoURL)
	if err != nil {
		return false
	}
	// Valid video paths look like /v79e41g-some-title.html
	// Reject channel pages (/c/...), user pages (/user/...), listing pages (/videos/...)
	path := parsed.Path
	return strings.HasPrefix(path, "/v") &&
		!strings.HasPrefix(path, "/videos") &&
		!strings.HasPrefix(path, "/user/")
}

// removeQueryParams removes query parameters from a URL for cleaner comparison.
func removeQueryParams(inputURL string) string {
	u, err := url.Parse(inputURL)
	if err != nil {
		logger.Pl.E("Failed to parse URL %q for query parameter removal: %v", inputURL, err)
		return inputURL // Return original if parsing fails
	}
	u.RawQuery = "" // Clear query parameters
	return u.String()
}
