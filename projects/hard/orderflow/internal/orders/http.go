package orders

import (
	"encoding/json"
	"errors"
	"net/http"
)

func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /orders", s.post)
	mux.HandleFunc("GET /orders", s.list)
	mux.HandleFunc("GET /orders/{id}", s.get)
}

// post answers 202: the order exists but the saga has only just started.
func (s *Service) post(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	o, err := s.Create(r.Context(), req, r.Header.Get("X-Correlation-ID"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Location", "/orders/"+o.ID)
	writeJSON(w, http.StatusAccepted, o)
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	o, err := s.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	all, err := s.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, all)
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
