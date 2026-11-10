// Command raftkv runs one node of a replicated key-value store.
//
//	raftkv -id 1 -peers 1=127.0.0.1:8201,2=127.0.0.1:8202,3=127.0.0.1:8203 -data ./data/1
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"raftkv/internal/clock"
	"raftkv/internal/kv"
	"raftkv/internal/raft"
	"raftkv/internal/server"
	"raftkv/internal/transport"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "raftkv:", err)
		os.Exit(1)
	}
}

func run() error {
	idFlag := flag.Uint64("id", 0, "this node's id (must appear in -peers)")
	peerList := flag.String("peers", "", "comma-separated id=host:port for every node, this one included")
	data := flag.String("data", "", "directory for the node's write-ahead log")
	flag.Parse()

	addrs, err := parsePeers(*peerList)
	if err != nil {
		return err
	}
	self := raft.NodeID(*idFlag)
	addr, ok := addrs[self]
	if !ok || *data == "" {
		return errors.New("need -id (listed in -peers) and -data")
	}
	if err := os.MkdirAll(*data, 0o755); err != nil {
		return err
	}
	storage, err := raft.OpenFileStorage(filepath.Join(*data, "raft.wal"))
	if err != nil {
		return err
	}
	defer storage.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("node", self)
	node, err := raft.New(raft.Config{
		ID: self, Peers: slices.Collect(maps.Keys(addrs)),
		Storage:   storage,
		Machine:   kv.NewStore(),
		Transport: transport.NewHTTP(ctx, self, addrs),
		Clock:     clock.Real{},
		Seed:      uint64(time.Now().UnixNano()),
		OnRole:    func(term uint64, r raft.Role) { log.Info("role change", "term", term, "role", r) },
	})
	if err != nil {
		return err
	}
	nodeDone := make(chan struct{})
	go func() {
		defer close(nodeDone)
		node.Run(ctx)
	}()
	defer func() { // the node must stop writing before the deferred storage.Close
		stop()
		<-nodeDone
	}()

	mux := http.NewServeMux()
	mux.Handle("POST /raft", transport.Handler(node))
	mux.Handle("/", server.New(node, addrs).Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Info("listening", "addr", addr, "data", *data)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func parsePeers(s string) (map[raft.NodeID]string, error) {
	addrs := make(map[raft.NodeID]string)
	for _, part := range strings.Split(s, ",") {
		idStr, addr, ok := strings.Cut(part, "=")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if !ok || err != nil || id == 0 || addr == "" {
			return nil, fmt.Errorf("bad -peers entry %q, want id=host:port", part)
		}
		addrs[raft.NodeID(id)] = addr
	}
	return addrs, nil
}
