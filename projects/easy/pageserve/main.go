// pageserve serves a directory as a small website:
//
//	/          HTML listing of the directory (html/template)
//	/files/…   the files themselves (http.FileServer)
//	/stats     how often every path was requested
//
//	pageserve -dir . -addr :8088
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	var (
		dir  = flag.String("dir", ".", "directory to serve")
		addr = flag.String("addr", ":8088", "listen address")
	)
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           newSite(os.DirFS(*dir)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("serving %s on http://localhost%s\n", *dir, *addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "pageserve:", err)
		os.Exit(1)
	}
}
