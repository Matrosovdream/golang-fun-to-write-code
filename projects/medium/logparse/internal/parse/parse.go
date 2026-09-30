// Package parse turns access-log lines into structured records.
//
// Log format (one line):
//
//	192.168.1.10 [30/Sep/2026:12:00:00] "GET /api/users" 200 1234 12.345
//
// (ip, timestamp, request, status, bytes, latency-ms)
package parse

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var ErrMalformed = errors.New("malformed log line")

type Line struct {
	IP        string
	Method    string
	Path      string
	Status    int
	Bytes     int
	LatencyMs float64
}

var lineRe = regexp.MustCompile(
	`^(\S+) \[([^\]]+)\] "(\S+) (\S+)" (\d{3}) (\d+) ([0-9.]+)$`)

// ParseNaive is the "first thing you'd write" version: one regexp.
// Correct, readable — and, as the benchmark shows, slow and allocation-heavy.
func ParseNaive(line string) (Line, error) {
	m := lineRe.FindStringSubmatch(line)
	if m == nil {
		return Line{}, fmt.Errorf("%w: %q", ErrMalformed, line)
	}
	status, _ := strconv.Atoi(m[5])
	bytes, _ := strconv.Atoi(m[6])
	lat, err := strconv.ParseFloat(m[7], 64)
	if err != nil {
		return Line{}, fmt.Errorf("%w: bad latency in %q", ErrMalformed, line)
	}
	return Line{IP: m[1], Method: m[3], Path: m[4], Status: status, Bytes: bytes, LatencyMs: lat}, nil
}

// ParseFast walks the line once with strings.Cut and IndexByte: no regexp
// engine, no submatch slice, no throwaway substrings beyond the fields we
// actually keep. Same behavior, ~10-20x faster (see parse_bench_test.go).
func ParseFast(line string) (Line, error) {
	ip, rest, ok := strings.Cut(line, " [")
	if !ok || ip == "" {
		return Line{}, badLine(line)
	}
	_, rest, ok = strings.Cut(rest, `] "`) // skip the timestamp
	if !ok {
		return Line{}, badLine(line)
	}
	method, rest, ok := strings.Cut(rest, " ")
	if !ok {
		return Line{}, badLine(line)
	}
	path, rest, ok := strings.Cut(rest, `" `)
	if !ok {
		return Line{}, badLine(line)
	}
	statusStr, rest, ok := strings.Cut(rest, " ")
	if !ok {
		return Line{}, badLine(line)
	}
	bytesStr, latStr, ok := strings.Cut(rest, " ")
	if !ok {
		return Line{}, badLine(line)
	}

	status, err := strconv.Atoi(statusStr)
	if err != nil || status < 100 || status > 599 {
		return Line{}, badLine(line)
	}
	bytes, err := strconv.Atoi(bytesStr)
	if err != nil {
		return Line{}, badLine(line)
	}
	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		return Line{}, badLine(line)
	}
	return Line{IP: ip, Method: method, Path: path, Status: status, Bytes: bytes, LatencyMs: lat}, nil
}

// badLine lives in its own function so the error path (which allocates)
// stays out of ParseFast's hot path — check the inlining with
// `go build -gcflags=-m ./internal/parse`.
func badLine(line string) error {
	return fmt.Errorf("%w: %q", ErrMalformed, line)
}
