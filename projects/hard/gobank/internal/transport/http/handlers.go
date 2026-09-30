package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"go.uber.org/zap"

	"gobank/internal/domain"
	"gobank/internal/service"
)

type credentialsRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=72"` // bcrypt caps input at 72 bytes
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !a.decode(w, r, &req) {
		return
	}
	token, err := a.auth.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		a.mapError(w, r, err)
		return
	}
	a.respondJSON(w, http.StatusCreated, map[string]string{"token": token})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !a.decode(w, r, &req) {
		return
	}
	token, err := a.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		a.mapError(w, r, err)
		return
	}
	a.respondJSON(w, http.StatusOK, map[string]string{"token": token})
}

type createAccountRequest struct {
	Currency string `json:"currency" validate:"required,iso4217"`
}

func (a *API) createAccount(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if !a.decode(w, r, &req) {
		return
	}
	acc, err := a.bank.CreateAccount(r.Context(), userID(r.Context()), req.Currency)
	if err != nil {
		a.mapError(w, r, err)
		return
	}
	a.respondJSON(w, http.StatusCreated, acc)
}

func (a *API) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := a.bank.MyAccounts(r.Context(), userID(r.Context()))
	if err != nil {
		a.mapError(w, r, err)
		return
	}
	a.respondJSON(w, http.StatusOK, accounts)
}

type amountRequest struct {
	Amount int64 `json:"amount" validate:"required,gt=0"`
}

func (a *API) deposit(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathID(r)
	if err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	var req amountRequest
	if !a.decode(w, r, &req) {
		return
	}
	tr, err := a.bank.Deposit(r.Context(), userID(r.Context()), accountID, req.Amount)
	if err != nil {
		a.mapError(w, r, err)
		return
	}
	a.respondJSON(w, http.StatusCreated, tr)
}

type transferRequest struct {
	FromAccountID int64 `json:"from_account_id" validate:"required"`
	ToAccountID   int64 `json:"to_account_id" validate:"required"`
	Amount        int64 `json:"amount" validate:"required,gt=0"`
}

func (a *API) transfer(w http.ResponseWriter, r *http.Request) {
	var req transferRequest
	if !a.decode(w, r, &req) {
		return
	}
	tr, err := a.bank.Transfer(r.Context(), userID(r.Context()),
		req.FromAccountID, req.ToAccountID, req.Amount)
	if err != nil {
		a.mapError(w, r, err)
		return
	}
	a.respondJSON(w, http.StatusCreated, tr)
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathID(r)
	if err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	transfers, err := a.bank.History(r.Context(), userID(r.Context()), accountID)
	if err != nil {
		a.mapError(w, r, err)
		return
	}
	a.respondJSON(w, http.StatusOK, transfers)
}

// --- helpers ---

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// decode reads + validates the body; on failure it responds itself and
// returns false, keeping handlers to a single early-return line.
func (a *API) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if err := a.validate.Struct(dst); err != nil {
		a.respondError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func (a *API) mapError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials), errors.Is(err, service.ErrInvalidToken):
		a.respondError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrEmailTaken):
		a.respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrAccountNotFound):
		a.respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrNotYourAccount):
		a.respondError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds),
		errors.Is(err, domain.ErrCurrencyMismatch),
		errors.Is(err, domain.ErrSameAccount),
		errors.Is(err, domain.ErrInvalidAmount):
		a.respondError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		a.log.Error("internal error", zap.Error(err))
		a.respondError(w, http.StatusInternalServerError, "internal error")
	}
}

func (a *API) respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) respondError(w http.ResponseWriter, status int, msg string) {
	a.respondJSON(w, status, map[string]string{"error": msg})
}
