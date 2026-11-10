// searchlite indexes text and markdown files and answers ranked queries:
//
//	searchlite index [-o snapshot] [dir]
//	searchlite query [-i snapshot] [-k n] <query>
//	searchlite serve [-i snapshot] [-addr host:port]
//
// Without a dir or a snapshot, the embedded demo corpus is used.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"searchlite/internal/corpus"
	"searchlite/internal/index"
	"searchlite/internal/query"
	"searchlite/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "searchlite:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: searchlite index | query | serve  (-h after a command lists its flags)")
	}
	switch args[0] {
	case "index":
		return runIndex(args[1:])
	case "query":
		return runQuery(args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// open loads the snapshot if there is one, else indexes the embedded corpus.
func open(snapshot string) (*index.Index, error) {
	if snapshot != "" {
		ix, err := index.Load(snapshot)
		if !errors.Is(err, fs.ErrNotExist) {
			return ix, err
		}
	}
	ix := index.New()
	return ix, ix.AddFS(corpus.FS())
}

func runIndex(args []string) error {
	fl := flag.NewFlagSet("index", flag.ExitOnError)
	out := fl.String("o", "searchlite.idx", "snapshot file to write")
	fl.Parse(args)
	src := corpus.FS()
	if fl.NArg() > 0 {
		src = os.DirFS(fl.Arg(0))
	}
	ix := index.New()
	if err := ix.AddFS(src); err != nil {
		return err
	}
	if err := ix.Save(*out); err != nil {
		return err
	}
	st := ix.Stats()
	fmt.Printf("indexed %d docs (%d terms, %d tokens) into %s\n", st.Docs, st.Terms, st.Tokens, *out)
	return nil
}

func runQuery(args []string) error {
	fl := flag.NewFlagSet("query", flag.ExitOnError)
	snapshot := fl.String("i", "", "snapshot file (default: the embedded demo corpus)")
	k := fl.Int("k", 5, "number of results")
	fl.Parse(args)
	if fl.NArg() != 1 {
		return errors.New("usage: searchlite query [-i snapshot] [-k n] <query>")
	}
	node, err := query.Parse(fl.Arg(0))
	if err != nil {
		return err
	}
	ix, err := open(*snapshot)
	if err != nil {
		return err
	}
	hits, total := ix.Search(node, *k)
	fmt.Printf("%d matches\n", total)
	for i, h := range hits {
		fmt.Printf("%2d. %6.3f  %s (%s)\n    %s\n", i+1, h.Score, h.Title, h.Name, h.Snippet)
	}
	return nil
}

func runServe(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("serve", flag.ExitOnError)
	snapshot := fl.String("i", "", "snapshot file to load (default: the embedded demo corpus)")
	addr := fl.String("addr", "127.0.0.1:8101", "listen address")
	fl.Parse(args)
	ix, err := open(*snapshot)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *addr, Handler: server.Handler(ix), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("serving %d docs on http://%s", ix.Stats().Docs, *addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
