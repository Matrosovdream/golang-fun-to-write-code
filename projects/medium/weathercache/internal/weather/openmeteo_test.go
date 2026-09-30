package weather_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"weathercache/internal/weather"
)

// The other way to test HTTP clients (vs. interface mocks): a fake server.
// This exercises real URL building, JSON decoding and status handling.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/geo", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") == "Atlantis" {
			fmt.Fprint(w, `{"results": []}`)
			return
		}
		fmt.Fprint(w, `{"results":[{"name":"Berlin","country":"Germany","latitude":52.52,"longitude":13.41}]}`)
	})
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "52.5200", r.URL.Query().Get("latitude"))
		fmt.Fprint(w, `{"current":{"temperature_2m":21.5,"wind_speed_10m":11.2,"weather_code":2}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenMeteoCurrent(t *testing.T) {
	srv := fakeAPI(t)
	om := weather.NewOpenMeteo(
		weather.WithBaseURLs(srv.URL+"/geo", srv.URL+"/forecast"),
		weather.WithHTTPClient(srv.Client()),
	)

	r, err := om.Current(context.Background(), "Berlin")
	require.NoError(t, err)
	require.Equal(t, "Berlin", r.City)
	require.Equal(t, "Germany", r.Country)
	require.InDelta(t, 21.5, r.TempC, 0.01)
	require.Equal(t, "partly cloudy", r.Description)
}

func TestOpenMeteoUnknownCity(t *testing.T) {
	srv := fakeAPI(t)
	om := weather.NewOpenMeteo(
		weather.WithBaseURLs(srv.URL+"/geo", srv.URL+"/forecast"),
		weather.WithHTTPClient(srv.Client()),
	)

	_, err := om.Current(context.Background(), "Atlantis")
	require.ErrorIs(t, err, weather.ErrCityNotFound)
}
