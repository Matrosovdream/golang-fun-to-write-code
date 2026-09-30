package service_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gobank/internal/service"
)

func TestTokenRoundTrip(t *testing.T) {
	tm := service.NewTokenManager("test-secret", time.Hour)

	token, err := tm.Issue(42)
	require.NoError(t, err)

	userID, err := tm.Parse(token)
	require.NoError(t, err)
	require.EqualValues(t, 42, userID)
}

func TestExpiredTokenRejected(t *testing.T) {
	tm := service.NewTokenManager("test-secret", -time.Minute) // born expired

	token, err := tm.Issue(42)
	require.NoError(t, err)

	_, err = tm.Parse(token)
	require.ErrorIs(t, err, service.ErrInvalidToken)
}

func TestForeignSignatureRejected(t *testing.T) {
	token, err := service.NewTokenManager("secret-a", time.Hour).Issue(42)
	require.NoError(t, err)

	_, err = service.NewTokenManager("secret-b", time.Hour).Parse(token)
	require.ErrorIs(t, err, service.ErrInvalidToken)
}
