package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"logparse/internal/analyze"
	"logparse/internal/parse"
)

func main() {
	var (
		file       = flag.String("file", "", "log file to analyze (required)")
		mode       = flag.String("mode", "chunked", "sequential | chunked | pipeline")
		parser     = flag.String("parser", "fast", "fast | naive")
		workers    = flag.Int("workers", runtime.NumCPU(), "parallel workers")
		top        = flag.Int("top", 5, "top-N entries to print")
		cpuprofile = flag.String("cpuprofile", "", "write CPU profile to file")
	)
	flag.Parse()
	if *file == "" {
		flag.Usage()
		os.Exit(2)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fatal(err)
		}
		defer pprof.StopCPUProfile()
	}

	parseLine := parse.ParseFast
	if *parser == "naive" {
		parseLine = parse.ParseNaive
	}

	start := time.Now()
	var (
		stats *analyze.Stats
		err   error
	)
	switch *mode {
	case "sequential":
		var f *os.File
		if f, err = os.Open(*file); err == nil {
			stats, err = analyze.Sequential(f, parseLine)
			f.Close()
		}
	case "chunked":
		stats, err = analyze.Chunked(*file, *workers, parseLine)
	case "pipeline":
		var f *os.File
		if f, err = os.Open(*file); err == nil {
			stats, err = analyze.Pipeline(f, *workers, parseLine)
			f.Close()
		}
	default:
		fatal(fmt.Errorf("unknown mode %q", *mode))
	}
	if err != nil {
		fatal(err)
	}
	elapsed := time.Since(start)

	report(stats, *top)
	rate := float64(stats.Total) / elapsed.Seconds() / 1e6
	fmt.Printf("\n%s/%s: %d lines in %s (%.1fM lines/s, %d malformed)\n",
		*mode, *parser, stats.Total, elapsed.Round(time.Millisecond), rate, stats.Malformed)
}

// report renders with strings.Builder: one final allocation instead of a
// write per line — the same idea loggen uses with its byte buffer.
func report(s *analyze.Stats, top int) {
	var b strings.Builder

	b.WriteString("== status codes ==\n")
	for _, p := range analyze.TopN(s.ByStatus, top) {
		fmt.Fprintf(&b, "  %3d  %d\n", p.Key, p.Count)
	}
	b.WriteString("== top IPs ==\n")
	for _, p := range analyze.TopN(s.ByIP, top) {
		fmt.Fprintf(&b, "  %-15s %d\n", p.Key, p.Count)
	}
	b.WriteString("== top paths ==\n")
	for _, p := range analyze.TopN(s.ByPath, top) {
		fmt.Fprintf(&b, "  %-20s %d\n", p.Key, p.Count)
	}
	fmt.Fprintf(&b, "== latency ==\n  p50 %.1fms  p95 %.1fms  p99 %.1fms\n",
		s.Percentile(50), s.Percentile(95), s.Percentile(99))

	os.Stdout.WriteString(b.String())
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
