package guardrail

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPrometheusClientQueriesVectorResult(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("query")
		if r.URL.Query().Get("time") == "" {
			t.Fatal("expected time query parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{"metric": {"job": "payment-api"}, "value": [1787130000.000, "0.015"]}
				]
			}
		}`))
	}))
	defer server.Close()

	client, err := NewPrometheusClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	got, err := client.Query(t.Context(), `sum(rate(errors_total[1m]))`, time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != 0.015 {
		t.Fatalf("expected value 0.015, got %f", got)
	}
	if gotQuery != `sum(rate(errors_total[1m]))` {
		t.Fatalf("unexpected query %q", gotQuery)
	}
}

func TestPrometheusClientQueriesScalarResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "scalar",
				"result": [1787130000.000, "42"]
			}
		}`))
	}))
	defer server.Close()

	client, err := NewPrometheusClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	got, err := client.Query(t.Context(), "up", time.Time{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != 42 {
		t.Fatalf("expected value 42, got %f", got)
	}
}

func TestPrometheusClientReturnsNoDataForEmptyVector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {"resultType": "vector", "result": []}
		}`))
	}))
	defer server.Close()

	client, err := NewPrometheusClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Query(t.Context(), "up", time.Time{})
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("expected no data, got %v", err)
	}
}

func TestPrometheusClientReturnsMultipleSeriesForMultiVector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{"metric": {"pod": "a"}, "value": [1787130000.000, "1"]},
					{"metric": {"pod": "b"}, "value": [1787130000.000, "2"]}
				]
			}
		}`))
	}))
	defer server.Close()

	client, err := NewPrometheusClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Query(t.Context(), "up", time.Time{})
	if !errors.Is(err, ErrMultipleSeries) {
		t.Fatalf("expected multiple series, got %v", err)
	}
}

func TestPrometheusClientReturnsErrorForPrometheusFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "error",
			"error": "query timeout"
		}`))
	}))
	defer server.Close()

	client, err := NewPrometheusClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Query(t.Context(), "up", time.Time{})
	if err == nil {
		t.Fatal("expected query error")
	}
}
