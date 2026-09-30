package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// OpenMeteo talks to the free open-meteo.com API (no key required).
// Two calls per lookup: geocode the city name, then fetch current weather.
type OpenMeteo struct {
	client      *http.Client
	geoBase     string
	forecastBase string
}

type OpenMeteoOption func(*OpenMeteo)

// WithHTTPClient injects the client — this is where the retry transport
// comes in, and how tests point the provider at an httptest server.
func WithHTTPClient(c *http.Client) OpenMeteoOption {
	return func(o *OpenMeteo) { o.client = c }
}

func WithBaseURLs(geo, forecast string) OpenMeteoOption {
	return func(o *OpenMeteo) { o.geoBase, o.forecastBase = geo, forecast }
}

func NewOpenMeteo(opts ...OpenMeteoOption) *OpenMeteo {
	o := &OpenMeteo{
		client:       &http.Client{Timeout: 10 * time.Second},
		geoBase:      "https://geocoding-api.open-meteo.com/v1/search",
		forecastBase: "https://api.open-meteo.com/v1/forecast",
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *OpenMeteo) Current(ctx context.Context, city string) (Report, error) {
	lat, lon, name, country, err := o.geocode(ctx, city)
	if err != nil {
		return Report{}, err
	}

	q := url.Values{
		"latitude":  {strconv.FormatFloat(lat, 'f', 4, 64)},
		"longitude": {strconv.FormatFloat(lon, 'f', 4, 64)},
		"current":   {"temperature_2m,wind_speed_10m,weather_code"},
	}
	var payload struct {
		Current struct {
			Temp float64 `json:"temperature_2m"`
			Wind float64 `json:"wind_speed_10m"`
			Code int     `json:"weather_code"`
		} `json:"current"`
	}
	if err := o.getJSON(ctx, o.forecastBase+"?"+q.Encode(), &payload); err != nil {
		return Report{}, fmt.Errorf("forecast: %w", err)
	}

	return Report{
		City:        name,
		Country:     country,
		Lat:         lat,
		Lon:         lon,
		TempC:       payload.Current.Temp,
		WindKmh:     payload.Current.Wind,
		Description: describe(payload.Current.Code),
		FetchedAt:   time.Now(),
	}, nil
}

func (o *OpenMeteo) geocode(ctx context.Context, city string) (lat, lon float64, name, country string, err error) {
	q := url.Values{"name": {city}, "count": {"1"}}
	var payload struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if err = o.getJSON(ctx, o.geoBase+"?"+q.Encode(), &payload); err != nil {
		return 0, 0, "", "", fmt.Errorf("geocode: %w", err)
	}
	if len(payload.Results) == 0 {
		return 0, 0, "", "", fmt.Errorf("%q: %w", city, ErrCityNotFound)
	}
	r := payload.Results[0]
	return r.Latitude, r.Longitude, r.Name, r.Country, nil
}

func (o *OpenMeteo) getJSON(ctx context.Context, rawURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// describe maps WMO weather codes to text (abridged).
func describe(code int) string {
	switch {
	case code == 0:
		return "clear sky"
	case code <= 3:
		return "partly cloudy"
	case code <= 48:
		return "fog"
	case code <= 57:
		return "drizzle"
	case code <= 67:
		return "rain"
	case code <= 77:
		return "snow"
	case code <= 82:
		return "rain showers"
	case code <= 86:
		return "snow showers"
	default:
		return "thunderstorm"
	}
}
