package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// makeTree builds a known directory layout in a temp dir:
//
//	root/a.txt (100B)  root/sub/b.txt (2500B)  root/sub/deep/c.txt (7B)
func makeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, size int) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", 100)
	write("sub/b.txt", 2500)
	write("sub/deep/c.txt", 7)
	return root
}

func TestScan(t *testing.T) {
	root := makeTree(t)

	sum, err := scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sum.total != 2607 {
		t.Errorf("total = %d, want 2607", sum.total)
	}
	if sum.files != 3 {
		t.Errorf("files = %d, want 3", sum.files)
	}
	if sum.dirs != 3 { // root, sub, sub/deep
		t.Errorf("dirs = %d, want 3", sum.dirs)
	}

	biggest := sum.biggest(2)
	if len(biggest) != 2 || biggest[0].size != 2500 || biggest[1].size != 100 {
		t.Errorf("biggest(2) = %v, want b.txt then a.txt", biggest)
	}
}

func TestScanMissingRoot(t *testing.T) {
	sum, err := scan(filepath.Join(t.TempDir(), "nope"))
	// Policy: even a missing root is "skip and report", not a fatal error.
	if err != nil {
		t.Fatalf("scan returned error %v, want skip-and-count", err)
	}
	if sum.skipped != 1 {
		t.Errorf("skipped = %d, want 1", sum.skipped)
	}
}

func TestHuman(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{4404019, "4.2MB"},
		{5 << 30, "5.0GB"},
	}
	for _, tc := range tests {
		if got := human(tc.n); got != tc.want {
			t.Errorf("human(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
