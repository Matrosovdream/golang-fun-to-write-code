package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing.txt":
			http.NotFound(w, r)
		default:
			fmt.Fprintf(w, "content of %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	urls := []string{srv.URL + "/a.txt", srv.URL + "/b.txt", srv.URL + "/missing.txt"}

	results, err := downloadAll(urls, dir, 2)
	if err == nil {
		t.Fatal("expected a joined error for the 404")
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	ok := 0
	for _, r := range results {
		if r.Err == nil {
			ok++
		}
	}
	if ok != 2 {
		t.Errorf("ok = %d, want 2", ok)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(raw) != "content of /a.txt" {
		t.Errorf("a.txt = %q, %v", raw, err)
	}
}

// The semaphore test: a slow server counts how many requests are in flight
// at once; the max must never exceed the limit.
func TestConcurrencyIsBounded(t *testing.T) {
	var inFlight, maxSeen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := maxSeen.Load()
			if cur <= old || maxSeen.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		fmt.Fprint(w, "x")
	}))
	defer srv.Close()

	urls := make([]string, 8)
	for i := range urls {
		urls[i] = fmt.Sprintf("%s/f%d.txt", srv.URL, i)
	}

	const limit = 3
	if _, err := downloadAll(urls, t.TempDir(), limit); err != nil {
		t.Fatal(err)
	}
	if got := maxSeen.Load(); got > limit {
		t.Errorf("max in-flight = %d, limit was %d — semaphore leaked", got, limit)
	}
}

func TestFilenameFor(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://x.com/docs/report.pdf", "report.pdf"},
		{"https://x.com/", "x.com-index.html"},
		{"https://x.com", "x.com-index.html"},
		{"https://y.org/", "y.org-index.html"}, // two bare hosts must not collide
	}
	for _, tc := range tests {
		if got := filenameFor(tc.url); got != tc.want {
			t.Errorf("filenameFor(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}
