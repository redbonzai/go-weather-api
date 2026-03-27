package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/redbonzai/weather-api/internal/cache"
	"github.com/redbonzai/weather-api/internal/nws"
	"github.com/redbonzai/weather-api/internal/obs"
	"github.com/redbonzai/weather-api/internal/queue"
	"github.com/redbonzai/weather-api/internal/upstream"
	"github.com/redbonzai/weather-api/internal/weather"
)

type Deps struct {
	Logger  *log.Logger
	Metrics *obs.Metrics
	Up      *upstream.Client
	Cache   *cache.Cache[string, ForecastResponse]
	Queue   *queue.Queue
	Weather weather.Service
	NWS     *nws.Client
}

type Handlers struct {
	deps Deps
}

func NewHandlers(deps Deps) *Handlers { return &Handlers{deps: deps} }

func (handlers *Handlers) Health(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ok"})
}

// TakehomeNWS serves GET /weather?lat=&lon= using api.weather.gov (short forecast for today + hot/cold/moderate).
func (handlers *Handlers) TakehomeNWS(writer http.ResponseWriter, request *http.Request) {
	if handlers.deps.NWS == nil {
		http.Error(writer, "nws client not configured", http.StatusInternalServerError)
		return
	}
	lat, lon, ok := parseLatLon(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()

	shortText, feel, err := handlers.deps.NWS.TodaySummary(ctx, lat, lon)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(writer, http.StatusOK, TakehomeWeatherResponse{
		ShortForecast:   shortText,
		TemperatureFeel: feel,
		Lat:             lat,
		Lon:             lon,
	})
}

// Problem 1 + 3 + 11: timeouts, cancellation, cache, retry/breaker in upstream client
func (handlers *Handlers) Forecast(writer http.ResponseWriter, request *http.Request) {
	lat, lon, ok := parseLatLon(writer, request)
	if !ok {
		return
	}

	// request budget
	reqCtx, cancel := context.WithTimeout(request.Context(), 250*time.Millisecond)
	defer cancel()

	cacheKey := "lat=" + formatFloat(lat) + "&lon=" + formatFloat(lon)
	now := time.Now()

	resp, cached, err := handlers.deps.Cache.GetOrLoad(cacheKey, now, func() (ForecastResponse, error) {
		// upstream has its own shorter budget
		upCtx, upCancel := context.WithTimeout(reqCtx, 150*time.Millisecond)
		defer upCancel()

		data, err := handlers.deps.Up.FetchForecast(upCtx, lat, lon)
		if err != nil {
			return ForecastResponse{}, err
		}
		return ForecastResponse{Lat: lat, Lon: lon, Data: data}, nil
	})

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(writer, "timeout", http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, context.Canceled) {
			// No official 499 in net/http; still useful to show intention in interviews
			writer.WriteHeader(499)
			_, _ = writer.Write([]byte("client closed request"))
			return
		}
		if errors.Is(err, upstream.ErrCircuitOpen) {
			http.Error(writer, "dependency unavailable (circuit open)", http.StatusServiceUnavailable)
			return
		}
		http.Error(writer, "internal error", http.StatusInternalServerError)
		return
	}

	resp.Cached = cached
	writeJSON(writer, http.StatusOK, resp)
}

// Problem 2: bounded fan-out aggregation (errgroup + semaphore)
func (handlers *Handlers) Conditions(writer http.ResponseWriter, request *http.Request) {
	zip := request.URL.Query().Get("zip")
	if zip == "" {
		http.Error(writer, "missing zip", http.StatusBadRequest)
		return
	}
	partial := request.URL.Query().Get("partial") == "true"

	reqCtx, cancel := context.WithTimeout(request.Context(), 300*time.Millisecond)
	defer cancel()

	sem := semaphore.NewWeighted(2) // bounded fanout
	group, groupCtx := errgroup.WithContext(reqCtx)

	data := make(map[string]any)
	var mu = make(chan struct{}, 1) // simple lock w/out sync import; in real code use sync.Mutex

	run := func(name string, fn func(context.Context) (map[string]any, error)) {
		group.Go(func() error {
			if err := sem.Acquire(groupCtx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			conditionData, err := fn(groupCtx)
			if err != nil {
				return err
			}
			mu <- struct{}{}
			data[name] = conditionData
			<-mu
			return nil
		})
	}

	run("alerts", func(ctx context.Context) (map[string]any, error) { return handlers.deps.Up.FetchAlerts(ctx, zip) })
	run("hourly", func(ctx context.Context) (map[string]any, error) { return handlers.deps.Up.FetchHourly(ctx, zip) })
	run("air_quality", func(ctx context.Context) (map[string]any, error) { return handlers.deps.Up.FetchAirQuality(ctx, zip) })

	err := group.Wait()
	if err != nil {
		if partial {
			writeJSON(writer, http.StatusOK, ConditionsResponse{Zip: zip, Partial: true, Data: data})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(writer, "timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(writer, "dependency failure", http.StatusBadGateway)
		return
	}

	writeJSON(writer, http.StatusOK, ConditionsResponse{Zip: zip, Partial: false, Data: data})
}

// Problem 5: enqueue event with backpressure
func (handlers *Handlers) PostEvent(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "read body", http.StatusBadRequest)
		return
	}
	var payload map[string]any
	if len(bytes.TrimSpace(body)) == 0 {
		payload = map[string]any{}
	} else if err = json.Unmarshal(body, &payload); err != nil {
		http.Error(writer, "bad json", http.StatusBadRequest)
		return
	}
	_ = payload // body validated; hook for future persistence

	err = handlers.deps.Queue.Enqueue(func(ctx context.Context) error {
		// simulate "write to Mongo"
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
			return nil
		}
	})
	if err != nil {
		if errors.Is(err, queue.ErrQueueFull) {
			http.Error(writer, "queue full", http.StatusServiceUnavailable)
			return
		}
		http.Error(writer, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(writer, http.StatusAccepted, map[string]any{
		"queued": true,
		"depth":  handlers.deps.Queue.Depth(),
	})
}

// ---------------------------------------------------------------------------
// Weather Company API handlers
// ---------------------------------------------------------------------------

// GET /weather/current?lat=&lon=
func (handlers *Handlers) WeatherCurrentConditions(writer http.ResponseWriter, request *http.Request) {
	lat, lon, ok := parseLatLon(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()

	result, err := handlers.deps.Weather.CurrentConditions(ctx, lat, lon)
	if err := handleWeatherError(writer, err); err != nil {
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

// GET /weather/forecast/hourly?lat=&lon=
func (handlers *Handlers) WeatherHourlyForecast(writer http.ResponseWriter, request *http.Request) {
	lat, lon, ok := parseLatLon(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()

	result, err := handlers.deps.Weather.HourlyForecast(ctx, lat, lon)
	if err := handleWeatherError(writer, err); err != nil {
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

// GET /weather/forecast/daily?lat=&lon=
func (handlers *Handlers) WeatherDailyForecast(writer http.ResponseWriter, request *http.Request) {
	lat, lon, ok := parseLatLon(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()

	result, err := handlers.deps.Weather.DailyForecast(ctx, lat, lon)
	if err := handleWeatherError(writer, err); err != nil {
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

// GET /weather/alerts?lat=&lon=
func (handlers *Handlers) WeatherAlertHeadlines(writer http.ResponseWriter, request *http.Request) {
	lat, lon, ok := parseLatLon(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()

	result, err := handlers.deps.Weather.AlertHeadlines(ctx, lat, lon)
	if err := handleWeatherError(writer, err); err != nil {
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

// GET /weather/alerts/detail?key=<detailKey>
func (handlers *Handlers) WeatherAlertDetails(writer http.ResponseWriter, request *http.Request) {
	alertKey := request.URL.Query().Get("key")
	if alertKey == "" {
		http.Error(writer, "missing key", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()

	result, err := handlers.deps.Weather.AlertDetails(ctx, alertKey)
	if err := handleWeatherError(writer, err); err != nil {
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

// handleWeatherError maps sentinel errors from the weather service to HTTP
// status codes and writes the response. Returns a non-nil error when a response
// was written (so the caller can return early), nil otherwise.
func handleWeatherError(writer http.ResponseWriter, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		http.Error(writer, "upstream timeout", http.StatusGatewayTimeout)
	case errors.Is(err, context.Canceled):
		writer.WriteHeader(499)
		_, _ = writer.Write([]byte("client closed request"))
	case errors.Is(err, upstream.ErrCircuitOpen):
		http.Error(writer, "dependency unavailable (circuit open)", http.StatusServiceUnavailable)
	case errors.Is(err, weather.ErrMissingAPIKey):
		http.Error(writer, "weather company key not configured: set WEATHER_API_KEY, or use GET /weather?lat=&lon= for National Weather Service (no API key)", http.StatusServiceUnavailable)
	case errors.Is(err, weather.ErrUnauthorized):
		http.Error(writer, "weather api key invalid", http.StatusBadGateway)
	case errors.Is(err, weather.ErrRateLimited):
		http.Error(writer, "weather api rate limited", http.StatusTooManyRequests)
	default:
		http.Error(writer, "internal error", http.StatusInternalServerError)
	}
	return err
}

func parseLatLon(writer http.ResponseWriter, request *http.Request) (float64, float64, bool) {
	latStr := request.URL.Query().Get("lat")
	lonStr := request.URL.Query().Get("lon")
	if latStr == "" || lonStr == "" {
		http.Error(writer, "missing lat/lon", http.StatusBadRequest)
		return 0, 0, false
	}
	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		http.Error(writer, "invalid lat", http.StatusBadRequest)
		return 0, 0, false
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		http.Error(writer, "invalid lon", http.StatusBadRequest)
		return 0, 0, false
	}
	return lat, lon, true
}

func writeJSON(writer http.ResponseWriter, code int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(code)
	_ = json.NewEncoder(writer).Encode(payload)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 4, 64)
}
