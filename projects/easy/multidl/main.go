// multidl downloads many URLs concurrently.
//
//	multidl -c 4 -dir out urls.txt
//	multidl https://a.com/x.pdf https://b.com/y.jpg
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	var (
		concurrency = flag.Int("c", 4, "max simultaneous downloads")
		dir         = flag.String("dir", ".", "output directory")
	)
	flag.Parse()

	urls, err := collectURLs(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "multidl:", err)
		os.Exit(1)
	}
	if len(urls) == 0 {
		fmt.Fprintln(os.Stderr, "usage: multidl [-c 4] [-dir out] <urls.txt | url url ...>")
		os.Exit(2)
	}

	start := time.Now()
	results, err := downloadAll(urls, *dir, *concurrency)

	var total int64
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("FAIL  %-40s %v\n", r.URL, r.Err)
			continue
		}
		total += r.Bytes
		fmt.Printf("ok    %-40s → %s (%d bytes, %s)\n",
			r.URL, r.File, r.Bytes, r.Elapsed.Round(time.Millisecond))
	}
	fmt.Printf("\n%d/%d succeeded, %d bytes in %s\n",
		len(results)-countFailed(results), len(results), total, time.Since(start).Round(time.Millisecond))

	if err != nil {
		os.Exit(1) // the joined error already printed per line above
	}
}

// collectURLs accepts either one file of URLs or URLs directly as args.
func collectURLs(args []string) ([]string, error) {
	if len(args) == 1 {
		if _, err := os.Stat(args[0]); err == nil {
			f, err := os.Open(args[0])
			if err != nil {
				return nil, err
			}
			defer f.Close()

			var urls []string
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if line := sc.Text(); line != "" {
					urls = append(urls, line)
				}
			}
			return urls, sc.Err()
		}
	}
	return args, nil
}

func countFailed(results []Result) int {
	n := 0
	for _, r := range results {
		if r.Err != nil {
			n++
		}
	}
	return n
}
