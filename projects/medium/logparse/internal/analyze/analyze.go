package analyze

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"

	"logparse/internal/parse"
)

type ParseFunc func(string) (parse.Line, error)

// Sequential is the baseline: one goroutine, one pass, bufio does the
// buffering. Everything else in this file tries to beat it.
func Sequential(r io.Reader, parseLine ParseFunc) (*Stats, error) {
	stats := NewStats()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		l, err := parseLine(sc.Text())
		if err != nil {
			stats.Malformed++
			continue
		}
		stats.Record(l)
	}
	return stats, sc.Err()
}

// Chunked splits the file into ~equal byte ranges (aligned to line breaks)
// and gives each worker its own range and its own *Stats: no channels in the
// hot path, no locks at all, one merge at the end. This is usually the
// fastest strategy for "aggregate a big file".
func Chunked(path string, workers int, parseLine ParseFunc) (*Stats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	size := mustSize(f)

	bounds, err := chunkBounds(f, size, workers)
	if err != nil {
		return nil, err
	}

	results := make([]*Stats, len(bounds)-1)
	var wg sync.WaitGroup
	errs := make([]error, len(bounds)-1)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = analyzeRange(path, bounds[i], bounds[i+1], parseLine)
		}()
	}
	wg.Wait()

	total := NewStats()
	for i, s := range results {
		if errs[i] != nil {
			return nil, errs[i]
		}
		total.Merge(s)
	}
	return total, nil
}

// chunkBounds returns worker+1 offsets, each (except 0) sitting just after a
// newline, so no line is ever split between two workers.
func chunkBounds(f *os.File, size int64, workers int) ([]int64, error) {
	bounds := []int64{0}
	buf := make([]byte, 1)
	for i := 1; i < workers; i++ {
		off := size * int64(i) / int64(workers)
		for off < size {
			if _, err := f.ReadAt(buf, off); err != nil {
				return nil, err
			}
			off++
			if buf[0] == '\n' {
				break
			}
		}
		bounds = append(bounds, off)
	}
	return append(bounds, size), nil
}

func analyzeRange(path string, from, to int64, parseLine ParseFunc) (*Stats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, err
	}
	return Sequential(io.LimitReader(f, to-from), parseLine)
}

// batch is what flows through the Pipeline mode's channel: a reusable slab
// of lines. sync.Pool recycles the slabs so a million-line file doesn't
// allocate a million batch slices.
type batch struct {
	lines []string
}

var batchPool = sync.Pool{
	New: func() any { return &batch{lines: make([]string, 0, 1024)} },
}

// Pipeline is the alternative parallel shape: one reader goroutine fans
// batches out to parser workers over a channel. More flexible than Chunked
// (works on any io.Reader, not just seekable files) — but the benchmark
// shows what channel traffic costs; run both and compare.
func Pipeline(r io.Reader, workers int, parseLine ParseFunc) (*Stats, error) {
	batches := make(chan *batch, workers*2)
	results := make(chan *Stats, workers)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := NewStats()
			for b := range batches {
				for _, line := range b.lines {
					l, err := parseLine(line)
					if err != nil {
						local.Malformed++
						continue
					}
					local.Record(l)
				}
				b.lines = b.lines[:0] // keep capacity, drop contents
				batchPool.Put(b)
			}
			results <- local
		}()
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	b := batchPool.Get().(*batch)
	for sc.Scan() {
		b.lines = append(b.lines, sc.Text())
		if len(b.lines) == cap(b.lines) {
			batches <- b
			b = batchPool.Get().(*batch)
		}
	}
	if len(b.lines) > 0 {
		batches <- b
	}
	close(batches)

	wg.Wait()
	close(results)

	total := NewStats()
	for s := range results {
		total.Merge(s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return total, nil
}

func mustSize(f *os.File) int64 {
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	return info.Size()
}
