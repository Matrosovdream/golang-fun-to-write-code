// Package it holds the integration tests: a real PostgreSQL in a container,
// real migrations, the real router — only the network port is fake.
//
// Run with `make itest` (or `go test ./...`); `go test -short` skips it.
package it

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"gobank/internal/repo/postgres"
	"gobank/internal/service"
	httpapi "gobank/internal/transport/http"
)

func startAPI(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("gobank"),
		tcpostgres.WithUsername("gobank"),
		tcpostgres.WithPassword("gobank"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	require.NoError(t, postgres.Migrate(dsn))
	db, err := postgres.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	tokens := service.NewTokenManager("it-secret", time.Hour)
	auth := service.NewAuth(postgres.NewUserRepo(db), tokens)
	bank := service.NewBank(postgres.NewAccountRepo(db), postgres.NewTransferRepo(db), db)

	srv := httptest.NewServer(httpapi.NewRouter(auth, bank, tokens, zap.NewNop()))
	t.Cleanup(srv.Close)
	return srv
}

// call is a minimal API client for the tests.
func call(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, srv.URL+path, &buf)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	var payload any
	_ = json.NewDecoder(res.Body).Decode(&payload)
	m, _ := payload.(map[string]any)
	return res.StatusCode, m
}

func registerUser(t *testing.T, srv *httptest.Server, email string) (token string) {
	status, body := call(t, srv, "POST", "/api/register", "", map[string]string{
		"email": email, "password": "hunter2hunter2",
	})
	require.Equal(t, http.StatusCreated, status)
	return body["token"].(string)
}

func newAccount(t *testing.T, srv *httptest.Server, token string, deposit int64) int64 {
	status, body := call(t, srv, "POST", "/api/accounts", token, map[string]string{"currency": "USD"})
	require.Equal(t, http.StatusCreated, status)
	id := int64(body["id"].(float64))

	status, _ = call(t, srv, "POST", fmt.Sprintf("/api/accounts/%d/deposit", id), token,
		map[string]int64{"amount": deposit})
	require.Equal(t, http.StatusCreated, status)
	return id
}

func balanceOf(t *testing.T, srv *httptest.Server, token string, accountID int64) int64 {
	req, err := http.NewRequest("GET", srv.URL+"/api/accounts", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	var accounts []map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&accounts))
	for _, a := range accounts {
		if int64(a["id"].(float64)) == accountID {
			return int64(a["balance"].(float64))
		}
	}
	t.Fatalf("account %d not found", accountID)
	return 0
}

func TestBankAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Docker")
	}
	srv := startAPI(t)

	tokenA := registerUser(t, srv, "alice@example.com")
	tokenB := registerUser(t, srv, "bob@example.com")

	accA := newAccount(t, srv, tokenA, 100_000)
	accB := newAccount(t, srv, tokenB, 100_000)

	t.Run("duplicate email is 409", func(t *testing.T) {
		status, _ := call(t, srv, "POST", "/api/register", "", map[string]string{
			"email": "alice@example.com", "password": "hunter2hunter2"})
		require.Equal(t, http.StatusConflict, status)
	})

	t.Run("login works and wrong password is 401", func(t *testing.T) {
		status, _ := call(t, srv, "POST", "/api/login", "", map[string]string{
			"email": "alice@example.com", "password": "hunter2hunter2"})
		require.Equal(t, http.StatusOK, status)

		status, _ = call(t, srv, "POST", "/api/login", "", map[string]string{
			"email": "alice@example.com", "password": "wrong-password"})
		require.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("simple transfer moves money", func(t *testing.T) {
		status, _ := call(t, srv, "POST", "/api/transfers", tokenA, map[string]int64{
			"from_account_id": accA, "to_account_id": accB, "amount": 5_000})
		require.Equal(t, http.StatusCreated, status)
		require.EqualValues(t, 95_000, balanceOf(t, srv, tokenA, accA))
		require.EqualValues(t, 105_000, balanceOf(t, srv, tokenB, accB))
	})

	t.Run("cannot transfer from someone else's account", func(t *testing.T) {
		status, _ := call(t, srv, "POST", "/api/transfers", tokenB, map[string]int64{
			"from_account_id": accA, "to_account_id": accB, "amount": 1})
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("insufficient funds is 422 and changes nothing", func(t *testing.T) {
		before := balanceOf(t, srv, tokenA, accA)
		status, _ := call(t, srv, "POST", "/api/transfers", tokenA, map[string]int64{
			"from_account_id": accA, "to_account_id": accB, "amount": 100_000_000})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Equal(t, before, balanceOf(t, srv, tokenA, accA))
	})

	// The reason ByIDForUpdate + ordered locking exist: opposite transfers
	// hammering the same two rows concurrently. Without ordered locks this
	// deadlocks; without row locks it loses money.
	t.Run("concurrent opposite transfers keep the ledger consistent", func(t *testing.T) {
		startA := balanceOf(t, srv, tokenA, accA)
		startB := balanceOf(t, srv, tokenB, accB)

		const n = 10
		var wg sync.WaitGroup
		for range n {
			wg.Add(2)
			go func() {
				defer wg.Done()
				status, _ := call(t, srv, "POST", "/api/transfers", tokenA, map[string]int64{
					"from_account_id": accA, "to_account_id": accB, "amount": 100})
				require.Equal(t, http.StatusCreated, status)
			}()
			go func() {
				defer wg.Done()
				status, _ := call(t, srv, "POST", "/api/transfers", tokenB, map[string]int64{
					"from_account_id": accB, "to_account_id": accA, "amount": 100})
				require.Equal(t, http.StatusCreated, status)
			}()
		}
		wg.Wait()

		require.Equal(t, startA, balanceOf(t, srv, tokenA, accA), "A's balance must be unchanged")
		require.Equal(t, startB, balanceOf(t, srv, tokenB, accB), "B's balance must be unchanged")
	})

	t.Run("history shows transfers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL+fmt.Sprintf("/api/accounts/%d/history", accA), nil)
		req.Header.Set("Authorization", "Bearer "+tokenA)
		res, err := srv.Client().Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var transfers []map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&transfers))
		require.NotEmpty(t, transfers)
	})
}
