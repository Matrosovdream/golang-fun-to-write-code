package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchStreamsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		fmt.Fprint(w, "hello body")
	}))
	defer srv.Close()

	var body, errw bytes.Buffer
	n, err := fetch(&body, &errw, srv.Client(), srv.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	if body.String() != "hello body" || n != 10 {
		t.Errorf("body = %q (%d bytes)", body.String(), n)
	}
	if !strings.Contains(errw.String(), "200 OK") || !strings.Contains(errw.String(), "X-Test: yes") {
		t.Errorf("verbose output missing pieces:\n%s", errw.String())
	}
}

func TestFetchFollowsRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		fmt.Fprint(w, "landed")
	}))
	defer srv.Close()

	var body, errw bytes.Buffer
	_, err := fetch(&body, &errw, srv.Client(), srv.URL+"/old", true)
	if err != nil {
		t.Fatal(err)
	}
	if body.String() != "landed" {
		t.Errorf("body = %q", body.String())
	}
	// The verbose line must show the FINAL url, proving the redirect happened.
	if !strings.Contains(errw.String(), "/new") {
		t.Errorf("final URL not shown:\n%s", errw.String())
	}
}

func TestFetchReports4xx(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	var body bytes.Buffer
	_, err := fetch(&body, io.Discard, srv.Client(), srv.URL, false)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want 404 mention", err)
	}
	if body.Len() != 0 {
		t.Errorf("error body must not reach dst, got %q", body.String())
	}
}

func TestFetchTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 50 * time.Millisecond}
	var body bytes.Buffer
	start := time.Now()
	_, err := fetch(&body, &body, client, srv.URL, false)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %v, timeout did not bound the wait", time.Since(start))
	}
}
