package models

// FetchURLModelURLs fetches all URLs from a list of URL models and returns a string slice.
func FetchURLModelURLs(cuModels []*ChannelURL) []string {
	cURLs := make([]string, 0, len(cuModels))
	for _, cu := range cuModels {
		if cu.URL != "" {
			cURLs = append(cURLs, cu.URL)
		}
	}
	return cURLs
}
