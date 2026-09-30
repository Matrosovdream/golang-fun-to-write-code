package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Result struct {
	URL      string
	Status   int
	Err      error
	Duration time.Duration
	Links    []string

	depth int // set by the coordinator, only meaningful during a crawl
}

func (r Result) OK() bool { return r.Err == nil && r.Status < 400 }

// fetch does one GET; when extractLinks is set it also parses the HTML body
// for <a href> targets. All timing/limits come from the caller's ctx and the
// crawler's rate limiter.
func (c *Crawler) fetch(ctx context.Context, rawURL string, extractLinks bool) Result {
	// Wait blocks until the shared token bucket allows one more request —
	// this caps the pool's total req/s no matter how many workers run.
	if err := c.limiter.Wait(ctx); err != nil {
		return Result{URL: rawURL, Err: err}
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{URL: rawURL, Err: err}
	}
	req.Header.Set("User-Agent", "fetchpool/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return Result{URL: rawURL, Err: err, Duration: time.Since(start)}
	}
	defer resp.Body.Close()

	res := Result{URL: rawURL, Status: resp.StatusCode, Duration: time.Since(start)}
	if extractLinks && res.OK() && strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		res.Links, res.Err = extractHrefs(resp.Body, resp.Request.URL)
	}
	return res
}

// extractHrefs tokenizes HTML (no full DOM needed) and resolves every href
// against the page URL, so relative links become absolute.
func extractHrefs(body io.Reader, base *url.URL) ([]string, error) {
	var links []string
	seen := make(map[string]struct{})

	z := html.NewTokenizer(body)
	for {
		switch z.Next() {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return links, nil
			}
			return links, fmt.Errorf("parse html: %w", z.Err())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "a" || !hasAttr {
				continue
			}
			for {
				key, val, more := z.TagAttr()
				if string(key) == "href" {
					if u := normalize(base, string(val)); u != "" {
						if _, dup := seen[u]; !dup {
							seen[u] = struct{}{}
							links = append(links, u)
						}
					}
				}
				if !more {
					break
				}
			}
		}
	}
}

func normalize(base *url.URL, href string) string {
	u, err := base.Parse(strings.TrimSpace(href))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Fragment = "" // #section links point at the same page
	return u.String()
}
