// dirsize reports how much space a directory tree uses and what the
// biggest files are.
//
//	dirsize -top 10 ~/Downloads
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	top := flag.Int("top", 10, "how many biggest files to list")
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	sum, err := scan(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dirsize:", err)
		os.Exit(1)
	}

	fmt.Printf("%s: %s in %d files (%d dirs)\n",
		root, human(sum.total), sum.files, sum.dirs)
	if sum.skipped > 0 {
		fmt.Printf("  (%d entries skipped: permission or read errors)\n", sum.skipped)
	}
	fmt.Println("\nbiggest files:")
	for _, f := range sum.biggest(*top) {
		fmt.Printf("  %8s  %s\n", human(f.size), f.path)
	}
}

type fileInfo struct {
	path string
	size int64
}

type summary struct {
	total   int64
	files   int
	dirs    int
	skipped int
	all     []fileInfo
}

// scan walks the tree once. Policy for unreadable entries: count and skip —
// a du-style tool that dies on the first permission error is useless.
func scan(root string) (*summary, error) {
	sum := &summary{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			sum.skipped++
			if d != nil && d.IsDir() {
				return fs.SkipDir // can't descend — skip the subtree, keep walking
			}
			return nil
		}
		if d.IsDir() {
			sum.dirs++
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks, sockets, devices: not "disk usage" here
		}

		info, err := d.Info()
		if err != nil {
			sum.skipped++
			return nil
		}
		sum.files++
		sum.total += info.Size()
		sum.all = append(sum.all, fileInfo{path, info.Size()})
		return nil
	})
	return sum, err
}

func (s *summary) biggest(n int) []fileInfo {
	sort.Slice(s.all, func(i, j int) bool { return s.all[i].size > s.all[j].size })
	if len(s.all) > n {
		return s.all[:n]
	}
	return s.all
}

// human renders byte counts the way people read them: 4.2MB, not 4404019.
func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
