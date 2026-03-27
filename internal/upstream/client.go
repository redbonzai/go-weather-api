package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/redbonzai/weather-api/internal/obs"
)

type Client struct {
	logger  *log.Logger
	metrics *obs.Metrics

	http *http.Client
	cb   *CircuitBreaker
	pol  RetryPolicy
}

func NewClient(logger *log.Logger, metrics *obs.Metrics) *Client {
	return &Client{
		logger:  logger,
		metrics: metrics,
		http: &http.Client{
			Timeout: 2 * time.Second, // global safety net; still use per-call ctx timeouts
		},
		cb:  NewCircuitBreaker(5, 5*time.Second),
		pol: RetryPolicy{MaxRetries: 2, BaseDelay: 50 * time.Millisecond, MaxDelay: 200 * time.Millisecond},
	}
}

// Fake upstream calls for interview practice.
// In real life these would hit other services.
func (client *Client) FetchForecast(ctx context.Context, lat, lon float64) (map[string]any, error) {
	return client.call(ctx, "forecast", func(ctx context.Context) error {
		// simulate remote latency + occasional failure
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
		return nil
	})
}

func (client *Client) FetchAlerts(ctx context.Context, zip string) (map[string]any, error) {
	return client.call(ctx, "alerts", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(60 * time.Millisecond):
		}
		return nil
	})
}

func (client *Client) FetchHourly(ctx context.Context, zip string) (map[string]any, error) {
	return client.call(ctx, "hourly", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(70 * time.Millisecond):
		}
		return nil
	})
}

func (client *Client) FetchAirQuality(ctx context.Context, zip string) (map[string]any, error) {
	return client.call(ctx, "air_quality", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(55 * time.Millisecond):
		}
		return nil
	})
}

func (client *Client) call(ctx context.Context, dep string, fn func(context.Context) error) (map[string]any, error) {
	now := time.Now()
	if !client.cb.Allow(now) {
		client.metrics.CountUpstreamError(dep, "circuit_open")
		return nil, ErrCircuitOpen
	}

	start := time.Now()
	err := DoWithRetry(ctx, client.pol, fn)
	ok := err == nil
	client.metrics.ObserveUpstream(dep, ok, time.Since(start))

	if err != nil {
		client.cb.OnFailure(time.Now())
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			client.metrics.CountUpstreamError(dep, "timeout_or_cancel")
		} else {
			client.metrics.CountUpstreamError(dep, "other")
		}
		return nil, err
	}
	client.cb.OnSuccess()

	// return dummy json-ish data
	raw := []byte(`{"dep":"` + dep + `","ok":true}`)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m, nil
}

// Example of how you would do real HTTP calls (kept here for realism; not used above)
func (client *Client) DoHTTP(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)
	return client.http.Do(req)
}