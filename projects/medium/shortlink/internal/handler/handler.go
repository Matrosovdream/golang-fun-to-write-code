package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"

	"shortlink/internal/repo"
	"shortlink/internal/service"
)

type Handler struct {
	svc      *service.Service
	baseURL  string
	log      *slog.Logger
	validate *validator.Validate
}

// New wires the whole HTTP surface: routes + middleware chain.
func New(svc *service.Service, baseURL string, log *slog.Logger) http.Handler {
	h := &Handler{
		svc:      svc,
		baseURL:  baseURL,
		log:      log,
		validate: validator.New(),
	}

	r := chi.NewRouter()
	// Order matters: RequestID first so everything downstream can log it;
	// Recoverer innermost-but-one so panics are still logged with the ID.
	r.Use(middleware.RequestID)
	r.Use(accessLog(log))
	r.Use(middleware.Recoverer)

	r.Post("/api/links", h.shorten)
	r.Get("/api/links/{code}", h.stats)
	r.Get("/{code}", h.redirect)
	return r
}

type shortenRequest struct {
	URL string `json:"url" validate:"required,http_url"`
}

type shortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

func (h *Handler) shorten(w http.ResponseWriter, r *http.Request) {
	var req shortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		h.respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	l, err := h.svc.Shorten(r.Context(), req.URL)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	h.respondJSON(w, http.StatusCreated, shortenResponse{
		Code:     l.Code,
		ShortURL: h.baseURL + "/" + l.Code,
	})
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request) {
	target, err := h.svc.Resolve(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	l, err := h.svc.Stats(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	h.respondJSON(w, http.StatusOK, l)
}

// mapError translates domain errors to HTTP codes in one place, so handlers
// never sprinkle status codes around.
func (h *Handler) mapError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		h.respondError(w, r, http.StatusNotFound, "no such link")
	case errors.Is(err, service.ErrInvalidURL):
		h.respondError(w, r, http.StatusBadRequest, err.Error())
	default:
		// Internal details stay in the log; the client gets a generic message.
		h.log.Error("internal error", "err", err, "request_id", middleware.GetReqID(r.Context()))
		h.respondError(w, r, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error("encode response", "err", err)
	}
}

func (h *Handler) respondError(w http.ResponseWriter, _ *http.Request, status int, msg string) {
	h.respondJSON(w, status, map[string]string{"error": msg})
}
