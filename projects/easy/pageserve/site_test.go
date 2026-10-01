package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// fstest.MapFS: an in-memory fs.FS — no temp dirs needed at all.
func testSite() http.Handler {
	return newSite(fstest.MapFS{
		"hello.txt":            {Data: []byte("hi there")},
		"<script>x</script>":   {Data: []byte("evil name")},
	})
}

func get(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	res, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func TestListingShowsFiles(t *testing.T) {
	srv := httptest.NewServer(testSite())
	defer srv.Close()

	status, body := get(t, srv, "/")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !strings.Contains(body, "hello.txt") || !strings.Contains(body, "8 bytes") {
		t.Errorf("listing incomplete:\n%s", body)
	}
}

// html/template escapes by context — the hostile filename must arrive as
// text, never as a live <script> tag.
func TestListingEscapesHostileNames(t *testing.T) {
	srv := httptest.NewServer(testSite())
	defer srv.Close()

	_, body := get(t, srv, "/")
	if strings.Contains(body, "<script>x</script>") {
		t.Errorf("XSS: raw script tag in output:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("escaped name missing:\n%s", body)
	}
}

func TestFileServing(t *testing.T) {
	srv := httptest.NewServer(testSite())
	defer srv.Close()

	status, body := get(t, srv, "/files/hello.txt")
	if status != http.StatusOK || body != "hi there" {
		t.Errorf("got %d %q", status, body)
	}

	status, _ = get(t, srv, "/files/missing.txt")
	if status != http.StatusNotFound {
		t.Errorf("missing file: status = %d, want 404", status)
	}
}

func TestStatsCounts(t *testing.T) {
	srv := httptest.NewServer(testSite())
	defer srv.Close()

	get(t, srv, "/")
	get(t, srv, "/")
	get(t, srv, "/files/hello.txt")

	_, body := get(t, srv, "/stats")
	for _, want := range []string{"<td>/</td><td>2</td>", "<td>/files/hello.txt</td><td>1</td>"} {
		if !strings.Contains(body, want) {
			t.Errorf("stats missing %q:\n%s", want, body)
		}
	}
}
