package site

import (
	"encoding/xml"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The structs here are deliberately separate from the ones in feed.go: the
// test reads the feed the way a reader would, not the way the writer wrote it.
type feedDoc struct {
	Version string `xml:"version,attr"`
	Channel struct {
		Title string `xml:"title"`
		Link  string `xml:"link"`
		Items []struct {
			Title      string   `xml:"title"`
			Link       string   `xml:"link"`
			GUID       string   `xml:"guid"`
			PubDate    string   `xml:"pubDate"`
			Categories []string `xml:"category"`
		} `xml:"item"`
	} `xml:"channel"`
}

func TestRSSFeed(t *testing.T) {
	src := baseSite()
	src["content/posts/amp.md"] = post("Tom & <Jerry>", "2026-03-20", "web")
	out, _ := build(t, src)

	var feed feedDoc
	require.NoError(t, xml.Unmarshal([]byte(read(t, out, "feed.xml")), &feed))
	require.Equal(t, "2.0", feed.Version)
	require.Equal(t, "Test Site", feed.Channel.Title)
	require.Equal(t, "https://example.com/", feed.Channel.Link)

	var titles, links []string
	for _, it := range feed.Channel.Items {
		titles = append(titles, it.Title)
		links = append(links, it.Link)
		require.Equal(t, it.Link, it.GUID)
	}
	require.Equal(t, []string{"Tom & <Jerry>", "Gamma", "Beta", "Alpha"}, titles, "newest first, draft excluded, markup round-trips")
	require.Equal(t, "https://example.com/posts/amp/", links[0])

	items := feed.Channel.Items
	for i, want := range []string{"2026-03-20", "2026-03-05", "2026-02-20", "2026-01-10"} {
		got, err := time.Parse(time.RFC1123Z, items[i].PubDate)
		require.NoError(t, err)
		require.Equal(t, want, got.Format(time.DateOnly))
	}
	require.Equal(t, []string{"go", "web"}, items[3].Categories)
}

func TestRSSFeedIsCapped(t *testing.T) {
	src := baseSite()
	for i := range 30 {
		src[fmt.Sprintf("content/posts/p%02d.md", i)] = post(fmt.Sprintf("P%02d", i), fmt.Sprintf("2027-01-%02d", i+1))
	}
	out, _ := build(t, src)

	var feed feedDoc
	require.NoError(t, xml.Unmarshal([]byte(read(t, out, "feed.xml")), &feed))
	require.Len(t, feed.Channel.Items, feedLimit)
	require.Equal(t, "P29", feed.Channel.Items[0].Title)
}

func TestSitemap(t *testing.T) {
	out, _ := build(t, baseSite())

	var set struct {
		NS   string `xml:"xmlns,attr"`
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	require.NoError(t, xml.Unmarshal([]byte(read(t, out, "sitemap.xml")), &set))
	require.Equal(t, "http://www.sitemaps.org/schemas/sitemap/0.9", set.NS)

	lastmod := map[string]string{}
	var locs []string
	for _, u := range set.URLs {
		locs = append(locs, u.Loc)
		lastmod[u.Loc] = u.LastMod
	}
	require.Equal(t, []string{
		"https://example.com/",
		"https://example.com/about/",
		"https://example.com/posts/alpha/",
		"https://example.com/posts/beta/",
		"https://example.com/posts/gamma/",
		"https://example.com/tags/go/",
		"https://example.com/tags/web/",
	}, locs)
	require.Equal(t, "2026-02-20", lastmod["https://example.com/posts/beta/"])
	require.Empty(t, lastmod["https://example.com/about/"])
}
