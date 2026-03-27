package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrailingSlashRedirect_preservesQuery(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/conditions", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("zip") == "" {
			http.Error(writer, "missing zip", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	})
	registerTrailingSlashRedirect(mux, "/conditions")

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + "/conditions/?zip=90210")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after redirect, got %d", resp.StatusCode)
	}
}

func TestWeatherRoutes_alertsDetailNotShadowedByAlertsSlash(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/weather/alerts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
	})
	mux.HandleFunc("/weather/alerts/detail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
	})
	registerTrailingSlashRedirect(mux, "/weather/alerts")
	registerTrailingSlashRedirect(mux, "/weather/alerts/detail")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/weather/alerts/detail", nil))
	if rec.Code != 202 {
		t.Fatalf("/weather/alerts/detail: want 202, got %d", rec.Code)
	}
}

func TestWeatherRoutes_aliasCurrentHourlyRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/weather/forecast/hourly", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(203)
	})
	registerAliasRedirect(mux, "/weather/current/hourly", "/weather/forecast/hourly")
	registerTrailingSlashRedirect(mux, "/weather/current")
	registerTrailingSlashRedirect(mux, "/weather/current/hourly")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/weather/current/hourly", nil))
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("alias: want 307, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/weather/forecast/hourly" {
		t.Fatalf("Location = %q", loc)
	}
}
