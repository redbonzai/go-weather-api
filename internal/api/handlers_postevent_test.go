package api

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redbonzai/weather-api/internal/obs"
	"github.com/redbonzai/weather-api/internal/queue"
	"github.com/redbonzai/weather-api/internal/upstream"
)

func TestPostEvent_EmptyBodyAccepted(t *testing.T) {
	jobQ := queue.New(queue.Config{MaxQueue: 100, NumWorker: 2})
	jobQ.Start()
	t.Cleanup(jobQ.Stop)

	logger := log.New(io.Discard, "", 0)
	metrics := obs.NewMetrics()
	h := NewHandlers(Deps{
		Logger:  logger,
		Metrics: metrics,
		Up:      upstream.NewClient(logger, metrics),
		Queue:   jobQ,
	})

	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.PostEvent(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d body=%q", http.StatusAccepted, rec.Code, rec.Body.String())
	}
}
