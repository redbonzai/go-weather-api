package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redbonzai/weather-api/internal/api"
	"github.com/redbonzai/weather-api/internal/cache"
	"github.com/redbonzai/weather-api/internal/httpserver"
	"github.com/redbonzai/weather-api/internal/nws"
	"github.com/redbonzai/weather-api/internal/obs"
	"github.com/redbonzai/weather-api/internal/queue"
	"github.com/redbonzai/weather-api/internal/ratelimit"
	"github.com/redbonzai/weather-api/internal/upstream"
	"github.com/redbonzai/weather-api/internal/weather"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	metrics := obs.NewMetrics()

	// Upstream client with retry + circuit breaker (Problem 11)
	up := upstream.NewClient(logger, metrics)

	// Weather Company API service — reads WEATHER_API_KEY from the environment.
	// Set BASE_URL to override the default (useful in staging / tests).
	weatherSvc := weather.New(weather.Config{
		APIKey:      envOrDefault("WEATHER_API_KEY", ""),
		BaseURL:     envOrDefault("WEATHER_BASE_URL", ""),
		Units:       weather.UnitsMetric,
		Language:    "en-US",
		HTTPTimeout: 2 * time.Second,
	}, logger, metrics)

	nwsClient := nws.New(envOrDefault("NWS_USER_AGENT", ""))

	// Cache with TTL + singleflight + stale-while-revalidate (Problem 3)
	forecastCache := cache.New[string, api.ForecastResponse](cache.Config{
		TTL:            30 * time.Second,
		StaleExtension: 10 * time.Second,
		MaxItems:       5000,
	})

	// Rate limiter (Problem 4)
	limiter := ratelimit.NewIPLimiter(10, 20, 5*time.Minute) // 10 rps, burst 20, evict idle after 5m

	// Queue + workers (Problem 5)
	jobQ := queue.New(queue.Config{
		MaxQueue:  1000,
		NumWorker: 50,
	})
	jobQ.Start()

	handler := api.NewHandlers(api.Deps{
		Logger:  logger,
		Metrics: metrics,
		Up:      up,
		Cache:   forecastCache,
		Queue:   jobQ,
		Weather: weatherSvc,
		NWS:     nwsClient,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.Health)
	mux.HandleFunc("/forecast", handler.Forecast)     // Problem 1 + 3 + 7 + 8 + 11
	mux.HandleFunc("/conditions", handler.Conditions) // Problem 2
	mux.HandleFunc("/events", handler.PostEvent)      // Problem 5
	mux.Handle("/metrics", metrics.Handler())         // Problem 8

	// Takehome: National Weather Service summary (register before /weather/* if using a catch-all; here paths are distinct)
	mux.HandleFunc("/weather", handler.TakehomeNWS)

	// Weather Company Developer Package — live read endpoints (all need ?lat=&lon= except alerts/detail ?key=)
	mux.HandleFunc("/weather/current", handler.WeatherCurrentConditions)
	mux.HandleFunc("/weather/forecast/hourly", handler.WeatherHourlyForecast)
	mux.HandleFunc("/weather/forecast/daily", handler.WeatherDailyForecast)
	mux.HandleFunc("/weather/alerts", handler.WeatherAlertHeadlines)
	mux.HandleFunc("/weather/alerts/detail", handler.WeatherAlertDetails)

	// Common path mistakes → canonical routes (query preserved)
	registerAliasRedirect(mux, "/weather/current/hourly", "/weather/forecast/hourly")
	registerAliasRedirect(mux, "/weather/current/daily", "/weather/forecast/daily")

	// Trailing slash → canonical path (Go’s mux does not treat /path and /path/ as the same).
	registerTrailingSlashRedirect(mux, "/weather/current/hourly")
	registerTrailingSlashRedirect(mux, "/weather/current/daily")
	registerTrailingSlashRedirect(mux, "/weather/current")
	registerTrailingSlashRedirect(mux, "/weather/forecast/hourly")
	registerTrailingSlashRedirect(mux, "/weather/forecast/daily")
	registerTrailingSlashRedirect(mux, "/weather/alerts")
	registerTrailingSlashRedirect(mux, "/weather/alerts/detail")
	registerTrailingSlashRedirect(mux, "/health")
	registerTrailingSlashRedirect(mux, "/forecast")
	registerTrailingSlashRedirect(mux, "/conditions")
	registerTrailingSlashRedirect(mux, "/events")

	// Middleware: request id + logging + rate limit + metrics (Problems 4,7,8)
	middleware := httpserver.Chain(
		mux,
		httpserver.RequestID(),
		httpserver.Logging(logger),
		httpserver.Metrics(metrics),
		httpserver.RateLimit(limiter),
	)

	listenAddr := envOrDefault("LISTEN_ADDR", ":8080")
	srv := httpserver.New(httpserver.Config{
		Addr:              listenAddr,
		Handler:           middleware,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	})

	// Graceful shutdown (Problem 6)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Printf("listening on http://localhost%s\n", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Println("shutdown start")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Stop accepting HTTP first so SIGTERM (Kubernetes) does not race with new /events enqueueing work.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Printf("shutdown: http server: %v", err)
	}
	jobQ.Stop()

	logger.Println("shutdown complete")
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// registerAliasRedirect serves only exact "from" and 307-redirects to "to" (query preserved).
func registerAliasRedirect(mux *http.ServeMux, from, to string) {
	mux.HandleFunc(from, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != from {
			http.NotFound(writer, request)
			return
		}
		dest := to
		if request.URL.RawQuery != "" {
			dest += "?" + request.URL.RawQuery
		}
		http.Redirect(writer, request, dest, http.StatusTemporaryRedirect)
	})
}

// registerTrailingSlashRedirect serves only exact "{base}/" and 307-redirects to base (query preserved).
// Do not use for paths that share a prefix with deeper routes (e.g. /weather/ would capture /weather/current).
func registerTrailingSlashRedirect(mux *http.ServeMux, base string) {
	if strings.HasSuffix(base, "/") {
		panic("registerTrailingSlashRedirect: base must have no trailing slash: " + base)
	}
	withSlash := base + "/"
	mux.HandleFunc(withSlash, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != withSlash {
			http.NotFound(writer, request)
			return
		}
		dest := base
		if request.URL.RawQuery != "" {
			dest += "?" + request.URL.RawQuery
		}
		http.Redirect(writer, request, dest, http.StatusTemporaryRedirect)
	})
}
