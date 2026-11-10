// Package server exposes an index over HTTP: GET /search and POST /docs.
// Posted documents live in memory only; the index's own locking is what
// makes them searchable while other searches are running.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"searchlite/internal/index"
	"searchlite/internal/query"
)

const (
	defaultK    = 10
	maxK        = 100
	maxDocBytes = 1 << 20
)

func Handler(ix *index.Index) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search", search(ix))
	mux.HandleFunc("POST /docs", addDoc(ix))
	return mux
}

func search(ix *index.Index) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		k, err := strconv.Atoi(params.Get("k"))
		if err != nil || k < 1 || k > maxK {
			k = defaultK
		}
		node, err := query.Parse(params.Get("q"))
		if err != nil {
			body := map[string]any{"error": err.Error()}
			var pe *query.ParseError
			if errors.As(err, &pe) {
				body["offset"] = pe.Off
			}
			writeJSON(w, http.StatusBadRequest, body)
			return
		}
		hits, total := ix.Search(node, k)
		writeJSON(w, http.StatusOK, map[string]any{"query": params.Get("q"), "total": total, "hits": hits})
	}
}

func addDoc(ix *index.Index) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var doc struct{ Name, Text string } // matches "name" and "text" case-insensitively
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDocBytes)).Decode(&doc); err != nil ||
			doc.Name == "" || strings.TrimSpace(doc.Text) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": `body must be {"name": "...", "text": "..."}`})
			return
		}
		id, err := ix.Add(doc.Name, doc.Text)
		if err != nil { // the only way Add fails is a duplicate name
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": doc.Name})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
