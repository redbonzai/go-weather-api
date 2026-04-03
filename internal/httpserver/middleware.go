package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redbonzai/weather-api/internal/obs"
	"github.com/redbonzai/weather-api/internal/ratelimit"
)

type Middleware func(http.Handler) http.Handler

func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
	for idx := len(middleware) - 1; idx >= 0; idx-- {
		handler = middleware[idx](handler)
	}
	return handler
}

type ctxKey string

const requestIDKey ctxKey = "request_id"

func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requestID := request.Header.Get("X-Request-Id")
			if requestID == "" {
				randBytes := make([]byte, 12)
				_, _ = rand.Read(randBytes)
				requestID = hex.EncodeToString(randBytes)
			}
			ctx := context.WithValue(request.Context(), requestIDKey, requestID)
			writer.Header().Set("X-Request-Id", requestID)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func GetRequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(code int) {
	recorder.status = code
	recorder.ResponseWriter.WriteHeader(code)
}

func Logging(logger *log.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			recorder := &statusRecorder{ResponseWriter: writer, status: 200}
			start := time.Now()
			next.ServeHTTP(recorder, request)
			latency := time.Since(start)

			logger.Printf("request method=%s path=%s status=%d latency_ms=%d request_id=%s",
				request.Method, request.URL.Path, recorder.status, latency.Milliseconds(), GetRequestID(request.Context()))
		})
	}
}

func Metrics(metric *obs.Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			recorder := &statusRecorder{ResponseWriter: writer, status: 200}
			start := time.Now()
			next.ServeHTTP(recorder, request)
			metric.ObserveRequest(request.Method, request.URL.Path, recorder.status, time.Since(start))
		})
	}
}

func RateLimit(limiter *ratelimit.IPLimiter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			// Never rate-limit probes or scrapes: kubelet / Prometheus often share one source IP
			// and would get 429, blocking readiness (Kubernetes) or missing metrics.
			switch request.URL.Path {
			case "/health", "/metrics":
				next.ServeHTTP(writer, request)
				return
			}
			ip := clientIP(request)
			if !limiter.Allow(ip) {
				writer.Header().Set("Retry-After", "1")
				http.Error(writer, "rate limited", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func clientIP(request *http.Request) string {
	// If behind proxy, you'd use trusted headers; for interview, keep simple and explicit.
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	// fallback
	return strings.Split(request.RemoteAddr, ":")[0]
}
