package utils

import (
	"strings"

	"github.com/gocolly/colly"
)

// censoredTvEpisodes looks for episode links on Censored.TV channel pages
func censoredTvEpisodes(c *colly.Collector, s []string) []string {

	// Scrape all links that contain "/episode/"
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if strings.Contains(link, "/episode/") {
			s = append(s, link)
		}
	})

	return s
}
