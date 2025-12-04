package consts

import "github.com/TubarrApp/gocommon/sharedtags"

// HTMLMetadataRule defines the metadata scraping elements.
type HTMLMetadataRule struct {
	Name     string
	Selector string
	Attr     string
}

// HTMLMetadataQuery holds the site name and metadata rules.
type HTMLMetadataQuery struct {
	Site  string
	Rules []HTMLMetadataRule
}

// HTMLBitchute holds scraping elements for bitchute.com.
var HTMLBitchute = HTMLMetadataQuery{
	Site: "bitchute.com",
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: `meta[itemprop="name"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "span[data-v-3c3cf957]", Attr: "data-v-3c3cf957"},
	},
}

// HTMLCensored holds scraping elements for censored.tv.
var HTMLCensored = HTMLMetadataQuery{
	Site: "censored.tv",
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: "#episode-container .episode-title"},
		{Name: sharedtags.JDescription, Selector: "#about .raised-content"},
		{Name: sharedtags.JReleaseDate, Selector: "#about time"},
		{Name: sharedtags.JDirectVideoURL, Selector: "a.dropdown-item[href$='.mp4']", Attr: "href"},
		{Name: sharedtags.JThumbnailURL, Selector: "video-js[poster]", Attr: "poster"},
	},
}

// HTMLOdysee holds scraping elements for odysee.com.
var HTMLOdysee = HTMLMetadataQuery{
	Site: "odysee.com",
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: `meta[property="og:video:release_date"]`, Attr: "content"},
	},
}

// HTMLRumble holds scraping elements for rumble.com.
var HTMLRumble = HTMLMetadataQuery{
	Site: "rumble.com",
	Rules: []HTMLMetadataRule{
		{Name: sharedtags.JTitle, Selector: "title"},
		{Name: sharedtags.JDescription, Selector: `meta[name="description"]`, Attr: "content"},
		{Name: sharedtags.JDescription, Selector: `meta[property="og:description"]`, Attr: "content"},
		{Name: sharedtags.JReleaseDate, Selector: "time", Attr: "datetime"},
	},
}
