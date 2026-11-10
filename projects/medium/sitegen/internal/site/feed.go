package site

import (
	"encoding/xml"
	"maps"
	"slices"
	"time"
)

const feedLimit = 20

type rss struct {
	XMLName     xml.Name  `xml:"rss"`
	Version     string    `xml:"version,attr"`
	Title       string    `xml:"channel>title"` // "a>b" nests the field inside <a>
	Link        string    `xml:"channel>link"`
	Description string    `xml:"channel>description"`
	Items       []rssItem `xml:"channel>item"`
}

type rssItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        string   `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Description string   `xml:"description,omitempty"`
	Categories  []string `xml:"category"`
}

type urlset struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func (s *siteData) abs(url string) string { return s.cfg.BaseURL + url }

func (s *siteData) rss(posts []*Page) ([]byte, error) {
	feed := rss{Version: "2.0", Title: s.cfg.Title, Link: s.abs("/"), Description: s.cfg.Description}
	for _, p := range posts {
		feed.Items = append(feed.Items, rssItem{
			Title:       p.Title,
			Link:        s.abs(p.URL),
			GUID:        s.abs(p.URL),
			PubDate:     p.Date.Format(time.RFC1123Z),
			Description: p.Summary,
			Categories:  p.Tags,
		})
	}
	return marshalXML(feed)
}

func (s *siteData) sitemap() ([]byte, error) {
	set := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: []sitemapURL{{Loc: s.abs("/")}}}
	for _, p := range s.pages {
		u := sitemapURL{Loc: s.abs(p.URL)}
		if p.IsPost {
			u.LastMod = p.Date.Format(time.DateOnly)
		}
		set.URLs = append(set.URLs, u)
	}
	for _, tag := range slices.Sorted(maps.Keys(s.tags)) {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.abs(tagURL(tag))})
	}
	return marshalXML(set)
}

func marshalXML(v any) ([]byte, error) {
	data, err := xml.MarshalIndent(v, "", "  ")
	return append([]byte(xml.Header), data...), err
}
