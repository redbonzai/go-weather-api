package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/redbonzai/weather-api/internal/obs"
	"github.com/redbonzai/weather-api/internal/upstream"
)

// Endpoint path templates for the Weather Company Developer Package (v1 API).
// Reference: https://developer.weather.com/docs/api-developer-package
const (
	pathCurrentConditions = "/v1/geocode/%s/%s/observations.json"
	pathHourlyForecast    = "/v1/geocode/%s/%s/forecast/hourly/24hour.json"
	pathDailyForecast     = "/v1/geocode/%s/%s/forecast/daily/7day.json"
	pathAlertHeadlines    = "/v1/geocode/%s/%s/alerts/headlines.json"
	pathAlertDetails      = "/v1/alerts/%s/details.json"
)

// client is the production implementation of Service.
// It is unexported; callers receive a Service interface from New().
type Client struct {
	cfg     Config
	logger  *log.Logger
	metrics *obs.Metrics
	http    *http.Client
	cb      *upstream.CircuitBreaker
	pol     upstream.RetryPolicy
}

// New constructs a weather Service backed by the live weather.com API.
// The returned value satisfies the Service interface.
func New(cfg Config, logger *log.Logger, metrics *obs.Metrics) Service {
	cfg.defaults()
	return &Client{
		cfg:     cfg,
		logger:  logger,
		metrics: metrics,
		http: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
		// Trip after 5 consecutive failures; stay open for 10 s.
		cb:  upstream.NewCircuitBreaker(5, 10*time.Second),
		pol: upstream.RetryPolicy{MaxRetries: 2, BaseDelay: 100 * time.Millisecond, MaxDelay: 500 * time.Millisecond},
	}
}

// CurrentConditions implements Service.
func (client *Client) CurrentConditions(ctx context.Context, lat, lon float64) (*CurrentConditionsResponse, error) {
	path := fmt.Sprintf(pathCurrentConditions, fmtCoord(lat), fmtCoord(lon))
	queryParams := client.baseParams()
	var out CurrentConditionsResponse
	if err := client.get(ctx, "current_conditions", path, queryParams, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HourlyForecast implements Service.
func (client *Client) HourlyForecast(ctx context.Context, lat, lon float64) (*HourlyForecastResponse, error) {
	path := fmt.Sprintf(pathHourlyForecast, fmtCoord(lat), fmtCoord(lon))
	queryParams := client.baseParams()
	var out HourlyForecastResponse
	if err := client.get(ctx, "hourly_forecast", path, queryParams, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DailyForecast implements Service.
func (client *Client) DailyForecast(ctx context.Context, lat, lon float64) (*DailyForecastResponse, error) {
	path := fmt.Sprintf(pathDailyForecast, fmtCoord(lat), fmtCoord(lon))
	queryParams := client.baseParams()
	var out DailyForecastResponse
	if err := client.get(ctx, "daily_forecast", path, queryParams, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AlertHeadlines implements Service.
func (client *Client) AlertHeadlines(ctx context.Context, lat, lon float64) (*AlertHeadlinesResponse, error) {
	path := fmt.Sprintf(pathAlertHeadlines, fmtCoord(lat), fmtCoord(lon))
	// Alert endpoints do not accept a units parameter.
	queryParams := url.Values{}
	queryParams.Set("apiKey", client.cfg.APIKey)
	queryParams.Set("language", client.cfg.Language)
	queryParams.Set("format", "json")
	var out AlertHeadlinesResponse
	if err := client.get(ctx, "alert_headlines", path, queryParams, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AlertDetails implements Service.
func (client *Client) AlertDetails(ctx context.Context, detailKey string) (*AlertDetailsResponse, error) {
	path := fmt.Sprintf(pathAlertDetails, url.PathEscape(detailKey))
	queryParams := url.Values{}
	queryParams.Set("apiKey", client.cfg.APIKey)
	queryParams.Set("language", client.cfg.Language)
	queryParams.Set("format", "json")
	var out AlertDetailsResponse
	if err := client.get(ctx, "alert_details", path, queryParams, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// get executes a GET request with retry + circuit-breaker and decodes the
// JSON response body into dst. dep is the label used for Prometheus metrics.
func (client *Client) get(ctx context.Context, dep, path string, queryParams url.Values, dst any) error {
	if strings.TrimSpace(client.cfg.APIKey) == "" {
		return ErrMissingAPIKey
	}
	now := time.Now()
	if !client.cb.Allow(now) {
		client.metrics.CountUpstreamError(dep, "circuit_open")
		return upstream.ErrCircuitOpen
	}

	rawURL := client.cfg.BaseURL + path + "?" + queryParams.Encode()

	start := time.Now()
	err := upstream.DoWithRetry(ctx, client.pol, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.http.Do(req)
		if err != nil {
			return fmt.Errorf("http do: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			// Authentication errors are permanent; do not retry.
			return fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("%w: %s", ErrRateLimited, resp.Status)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("upstream status %d", resp.StatusCode)
		}

		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	})

	client.metrics.ObserveUpstream(dep, err == nil, time.Since(start))

	if err != nil {
		client.cb.OnFailure(time.Now())

		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			client.metrics.CountUpstreamError(dep, "timeout_or_cancel")
		case errors.Is(err, ErrUnauthorized):
			client.metrics.CountUpstreamError(dep, "auth")
		case errors.Is(err, ErrRateLimited):
			client.metrics.CountUpstreamError(dep, "rate_limited")
		default:
			client.metrics.CountUpstreamError(dep, "other")
		}
		return err
	}

	client.cb.OnSuccess()
	return nil
}

// baseParams returns the query parameters common to all forecast/conditions endpoints.
func (client *Client) baseParams() url.Values {
	queryParams := url.Values{}
	queryParams.Set("apiKey", client.cfg.APIKey)
	queryParams.Set("units", string(client.cfg.Units))
	queryParams.Set("language", client.cfg.Language)
	queryParams.Set("format", "json")
	return queryParams
}

// fmtCoord formats a coordinate to 4 decimal places for use in URL path segments.
func fmtCoord(coord float64) string {
	return fmt.Sprintf("%.4f", coord)
}

// Sentinel errors returned by the weather client.
var (
	// ErrMissingAPIKey is returned when WEATHER_API_KEY was not set (before any HTTP call).
	ErrMissingAPIKey = errors.New("weather api: missing api key")

	// ErrUnauthorized is returned when the upstream rejects the API key.
	ErrUnauthorized = errors.New("weather api: unauthorized")

	// ErrRateLimited is returned when the account has exceeded its request quota.
	ErrRateLimited = errors.New("weather api: rate limited")
)
