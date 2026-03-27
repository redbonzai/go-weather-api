package nws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.weather.gov"

// ErrNoTodayPeriod is returned when the forecast payload has no usable period.
var ErrNoTodayPeriod = errors.New("nws: no forecast period for today")

// Client calls api.weather.gov. NWS requires a descriptive User-Agent.
type Client struct {
	baseURL   string
	userAgent string
	http      *http.Client
}

// New builds a client. userAgent must identify your app (NWS policy); if empty, default is used.
func New(userAgent string) *Client {
	if userAgent == "" {
		userAgent = "github.com/redbonzai/weather-api (takehome; no contact configured)"
	}
	return &Client{
		baseURL:   defaultBaseURL,
		userAgent: userAgent,
		http: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
}

// TodaySummary returns the short forecast text for the current daytime period and a hot/cold/moderate label.
func (client *Client) TodaySummary(ctx context.Context, lat, lon float64) (shortForecast string, tempFeel string, err error) {
	pointsURL := fmt.Sprintf("%s/points/%.4f,%.4f", client.baseURL, lat, lon)
	var points pointsDoc
	if err := client.getJSON(ctx, pointsURL, &points); err != nil {
		return "", "", err
	}
	forecastURL := strings.TrimSpace(points.Properties.Forecast)
	if forecastURL == "" {
		return "", "", fmt.Errorf("nws: points response missing forecast URL")
	}
	var fc forecastDoc
	if err := client.getJSON(ctx, forecastURL, &fc); err != nil {
		return "", "", err
	}
	period, ok := pickTodayPeriod(fc.Properties.Periods)
	if !ok {
		return "", "", ErrNoTodayPeriod
	}
	tempFahrenheit := tempFahrenheit(period.Temperature, period.TemperatureUnit)
	return period.ShortForecast, ClassifyTemperatureF(tempFahrenheit), nil
}

func pickTodayPeriod(periods []forecastPeriod) (forecastPeriod, bool) {
	if len(periods) == 0 {
		return forecastPeriod{}, false
	}
	for _, p := range periods {
		if strings.EqualFold(strings.TrimSpace(p.Name), "Today") {
			return p, true
		}
	}
	return periods[0], true
}

// ClassifyTemperatureF maps °F to takehome categories (documented in README).
func ClassifyTemperatureF(temperature float64) string {
	switch {
	case temperature < 50:
		return "cold"
	case temperature > 82:
		return "hot"
	default:
		return "moderate"
	}
}

func tempFahrenheit(temp int, unit string) float64 {
	u := strings.ToUpper(strings.TrimSpace(unit))
	if u == "C" {
		return float64(temp)*9/5 + 32
	}
	return float64(temp)
}

func (client *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/geo+json")
	req.Header.Set("User-Agent", client.userAgent)

	resp, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("nws: %s returned %s: %s", url, resp.Status, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("nws: decode json: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type pointsDoc struct {
	Properties struct {
		Forecast string `json:"forecast"`
	} `json:"properties"`
}

type forecastDoc struct {
	Properties struct {
		Periods []forecastPeriod `json:"periods"`
	} `json:"properties"`
}

type forecastPeriod struct {
	Name            string `json:"name"`
	ShortForecast   string `json:"shortForecast"`
	Temperature     int    `json:"temperature"`
	TemperatureUnit string `json:"temperatureUnit"`
}
