// fetchcli is a tiny curl:
//
//	fetchcli https://go.dev
//	fetchcli -v -timeout 5s -o page.html https://go.dev
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"
)

func main() {
	var (
		out     = flag.String("o", "", "write body to file instead of stdout")
		timeout = flag.Duration("timeout", 10*time.Second, "give up after this long")
		verbose = flag.Bool("v", false, "print status and headers to stderr")
	)
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: fetchcli [-v] [-o file] [-timeout 5s] <url>")
		os.Exit(2)
	}

	// Never bare http.Get: the default client has NO timeout, and a hung
	// server would hang this program forever.
	client := &http.Client{Timeout: *timeout}

	var dst io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		dst = f
	}

	n, err := fetch(dst, os.Stderr, client, flag.Arg(0), *verbose)
	if err != nil {
		fatal(err)
	}
	if *out != "" {
		fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", n, *out)
	}
}

// fetch GETs url and streams the body to dst. Body handling is the lesson:
// always close it, and io.Copy streams chunk by chunk — a 10GB file never
// sits in memory the way io.ReadAll would put it there.
func fetch(dst, errw io.Writer, client *http.Client, url string, verbose bool) (int64, error) {
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if verbose {
		// resp.Request is the FINAL request — after redirects, which the
		// client followed for us.
		fmt.Fprintf(errw, "> GET %s\n< %s %s\n", resp.Request.URL, resp.Proto, resp.Status)
		for _, k := range sortedHeaderKeys(resp.Header) {
			fmt.Fprintf(errw, "< %s: %s\n", k, resp.Header.Get(k))
		}
		fmt.Fprintln(errw)
	}

	if resp.StatusCode >= 400 {
		// Drain a little so the connection can be reused, then report.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return 0, errors.New("server returned " + resp.Status)
	}
	return io.Copy(dst, resp.Body)
}

func sortedHeaderKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fetchcli:", err)
	os.Exit(1)
}
