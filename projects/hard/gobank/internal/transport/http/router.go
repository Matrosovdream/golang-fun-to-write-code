// Package httpapi is the HTTP transport: routing, DTOs, auth middleware and
// error mapping. It knows nothing about SQL and the services know nothing
// about HTTP.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"

	"gobank/internal/service"
)

type API struct {
	auth     *service.Auth
	bank     *service.Bank
	tokens   *service.TokenManager
	log      *zap.Logger
	validate *validator.Validate
}

func NewRouter(auth *service.Auth, bank *service.Bank, tokens *service.TokenManager, log *zap.Logger) http.Handler {
	api := &API{
		auth:     auth,
		bank:     bank,
		tokens:   tokens,
		log:      log,
		validate: validator.New(),
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(log))
	r.Use(middleware.Recoverer)

	r.Post("/api/register", api.register)
	r.Post("/api/login", api.login)

	// Everything in this group requires a valid Bearer token.
	r.Group(func(r chi.Router) {
		r.Use(api.requireAuth)
		r.Post("/api/accounts", api.createAccount)
		r.Get("/api/accounts", api.listAccounts)
		r.Post("/api/accounts/{id}/deposit", api.deposit)
		r.Get("/api/accounts/{id}/history", api.history)
		r.Post("/api/transfers", api.transfer)
	})
	return r
}
