// grepish is a small grep:
//
//	grepish -i -n "todo" main.go util.go
//	grepish -r "func main" ./src
//	cat log.txt | grepish ERROR
//
// Exit codes follow real grep: 0 = matched, 1 = no match, 2 = error.
// That makes it scriptable: `if grepish -r TODO .; then ...`
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

type options struct {
	ignoreCase bool
	lineNums   bool
	recursive  bool
}

func main() {
	var opts options
	flag.BoolVar(&opts.ignoreCase, "i", false, "case-insensitive")
	flag.BoolVar(&opts.lineNums, "n", false, "show line numbers")
	flag.BoolVar(&opts.recursive, "r", false, "search directories recursively")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: grepish [-i -n -r] <pattern> [path...]")
		os.Exit(2)
	}

	// Compile ONCE, outside any loop. (?i) bakes case-insensitivity into
	// the pattern instead of lowercasing every line.
	pattern := flag.Arg(0)
	if opts.ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grepish: bad pattern:", err)
		os.Exit(2)
	}

	paths := flag.Args()[1:]
	matched, errCount := searchAll(os.Stdout, re, paths, opts)

	switch {
	case errCount > 0:
		os.Exit(2)
	case !matched:
		os.Exit(1)
	}
}

func searchAll(w io.Writer, re *regexp.Regexp, paths []string, opts options) (matched bool, errCount int) {
	if len(paths) == 0 {
		m, err := search(w, re, os.Stdin, "", opts)
		return m, countErr(err)
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "grepish:", err)
			errCount++
			continue
		}

		if info.IsDir() {
			if !opts.recursive {
				fmt.Fprintf(os.Stderr, "grepish: %s is a directory (use -r)\n", path)
				errCount++
				continue
			}
			err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
				if err != nil || !d.Type().IsRegular() {
					return nil
				}
				m, ferr := searchFile(w, re, p, opts)
				matched = matched || m
				errCount += countErr(ferr)
				return nil
			})
			errCount += countErr(err)
			continue
		}

		m, err := searchFile(w, re, path, opts)
		matched = matched || m
		errCount += countErr(err)
	}
	return matched, errCount
}

func searchFile(w io.Writer, re *regexp.Regexp, path string, opts options) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grepish:", err)
		return false, err
	}
	defer f.Close()
	return search(w, re, f, path, opts)
}

// search prints matching lines from one reader. Matches go to stdout,
// problems to stderr — mixing them would break every pipeline.
func search(w io.Writer, re *regexp.Regexp, r io.Reader, name string, opts options) (bool, error) {
	matched := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for line := 1; sc.Scan(); line++ {
		if !re.Match(sc.Bytes()) {
			continue
		}
		matched = true
		switch {
		case name != "" && opts.lineNums:
			fmt.Fprintf(w, "%s:%d:%s\n", name, line, sc.Text())
		case name != "":
			fmt.Fprintf(w, "%s:%s\n", name, sc.Text())
		case opts.lineNums:
			fmt.Fprintf(w, "%d:%s\n", line, sc.Text())
		default:
			fmt.Fprintln(w, sc.Text())
		}
	}
	return matched, sc.Err()
}

func countErr(err error) int {
	if err != nil {
		return 1
	}
	return 0
}
