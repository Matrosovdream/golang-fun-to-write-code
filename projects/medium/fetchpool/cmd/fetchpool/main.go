package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"time"

	"fetchpool/internal/crawler"
)

func main() {
	var (
		workers = flag.Int("workers", 8, "number of concurrent workers")
		rps     = flag.Float64("rate", 20, "max requests per second (total)")
		depth   = flag.Int("depth", 2, "crawl depth (crawl mode)")
		file    = flag.String("file", "", "file with URLs, one per line (check mode)")
		anyHost = flag.Bool("any-host", false, "follow links to other hosts (crawl mode)")
	)
	flag.Parse()

	// Ctrl+C cancels the context; every in-flight request sees it because
	// the ctx is threaded through http.NewRequestWithContext.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	opts := []crawler.Option{
		crawler.WithWorkers(*workers),
		crawler.WithRate(*rps),
		crawler.WithMaxDepth(*depth),
	}
	if *anyHost {
		opts = append(opts, crawler.WithAnyHost())
	}
	c := crawler.New(opts...)

	start := time.Now()
	var results []crawler.Result
	var err error

	switch {
	case *file != "":
		urls, readErr := readLines(*file)
		if readErr != nil {
			fatal(readErr)
		}
		var stats *crawler.CheckStats
		results, stats, err = c.CheckAll(ctx, urls)
		defer func() {
			fmt.Printf("\nchecked %d urls in %s — ok %d, broken %d, failed %d\n",
				len(urls), time.Since(start).Round(time.Millisecond),
				stats.OK.Load(), stats.Broken.Load(), stats.Failed.Load())
		}()
	case flag.NArg() == 1:
		results, err = c.Crawl(ctx, flag.Arg(0))
		defer func() {
			fmt.Printf("\ncrawled %d pages in %s\n",
				len(results), time.Since(start).Round(time.Millisecond))
		}()
	default:
		fmt.Fprintln(os.Stderr, "usage: fetchpool [flags] <start-url>   or   fetchpool -file urls.txt")
		flag.PrintDefaults()
		os.Exit(2)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].URL < results[j].URL })
	for _, r := range results {
		switch {
		case r.Err != nil:
			fmt.Printf("FAIL  %-50s %v\n", r.URL, r.Err)
		case r.Status >= 400:
			fmt.Printf("%d   %-50s %s\n", r.Status, r.URL, r.Duration.Round(time.Millisecond))
		default:
			fmt.Printf("%d   %-50s %s (%d links)\n", r.Status, r.URL, r.Duration.Round(time.Millisecond), len(r.Links))
		}
	}
	if err != nil && ctx.Err() == nil {
		fatal(err)
	}
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, sc.Err()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
