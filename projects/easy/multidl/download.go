package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"
)

type Result struct {
	URL     string
	File    string
	Bytes   int64
	Elapsed time.Duration
	Err     error
}

var client = &http.Client{Timeout: 60 * time.Second}

// downloadAll fetches every URL with at most `concurrency` in flight.
// The shape to internalize:
//
//   - one goroutine per URL, counted by a WaitGroup;
//   - a buffered channel as a semaphore caps how many run at once;
//   - results flow back over a channel — no shared slice, no mutex;
//   - a closer goroutine turns "all workers done" into "channel closed",
//     so the collecting loop below is a plain range.
func downloadAll(urls []string, dir string, concurrency int) ([]Result, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	sem := make(chan struct{}, concurrency)
	resultCh := make(chan Result)

	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}        // acquire: blocks while `concurrency` others run
			defer func() { <-sem }() // release
			resultCh <- download(u, dir)
		}()
	}
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var results []Result
	var errs []error
	for r := range resultCh {
		results = append(results, r)
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.URL, r.Err))
		}
	}
	// errors.Join: all failures in one error value, nil when the slice is
	// empty — exactly what a summary wants.
	return results, errors.Join(errs...)
}

func download(rawURL, dir string) Result {
	start := time.Now()
	res := Result{URL: rawURL}

	resp, err := client.Get(rawURL)
	if err != nil {
		res.Err = err
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		res.Err = errors.New(resp.Status)
		return res
	}

	res.File = filepath.Join(dir, filenameFor(rawURL))
	f, err := os.Create(res.File)
	if err != nil {
		res.Err = err
		return res
	}

	res.Bytes, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr // a failed Close on write IS a failed download
	}
	res.Err = err
	res.Elapsed = time.Since(start)
	return res
}

// filenameFor derives a local name from the URL path. Bare hosts fall back
// to <host>-index.html — a plain index.html would collide as soon as two
// bare URLs are downloaded together (found live in the first demo run).
func filenameFor(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "index.html"
	}
	if base := path.Base(u.Path); base != "/" && base != "." {
		return base
	}
	if u.Host != "" {
		return u.Host + "-index.html"
	}
	return "index.html"
}
