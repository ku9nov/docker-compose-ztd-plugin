package metricsgate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnalyzeFailsOnHigh5xxRatio(t *testing.T) {
	t.Parallel()

	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call++
		if call == 1 {
			fmt.Fprintln(w, `traefik_service_requests_total{code="200",service="api_new@file"} 100`)
			fmt.Fprintln(w, `traefik_service_requests_total{code="500",service="api_new@file"} 1`)
			fmt.Fprintln(w, `traefik_service_request_duration_seconds_sum{service="api_new@file"} 2`)
			fmt.Fprintln(w, `traefik_service_request_duration_seconds_count{service="api_new@file"} 101`)
			return
		}
		fmt.Fprintln(w, `traefik_service_requests_total{code="200",service="api_new@file"} 150`)
		fmt.Fprintln(w, `traefik_service_requests_total{code="500",service="api_new@file"} 20`)
		fmt.Fprintln(w, `traefik_service_request_duration_seconds_sum{service="api_new@file"} 4`)
		fmt.Fprintln(w, `traefik_service_request_duration_seconds_count{service="api_new@file"} 170`)
	}))
	defer server.Close()

	cfg := Config{
		URL:              server.URL,
		Window:           30 * time.Millisecond,
		Interval:         10 * time.Millisecond,
		MinRequests:      5,
		Max5xxRatio:      0.05,
		Max4xxRatio:      -1,
		MaxMeanLatencyMS: -1,
	}

	result, err := Analyze(context.Background(), nil, cfg, []string{"api_new"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Verdict != VerdictFail {
		t.Fatalf("expected fail verdict, got %s", result.Verdict)
	}
	if result.TotalRequests <= 0 {
		t.Fatalf("expected positive requests delta, got %.0f", result.TotalRequests)
	}
}

func TestAnalyzeReturnsInsufficientData(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `traefik_service_requests_total{code="200",service="api-green@file"} 10`)
		fmt.Fprintln(w, `traefik_service_request_duration_seconds_sum{service="api-green@file"} 1`)
		fmt.Fprintln(w, `traefik_service_request_duration_seconds_count{service="api-green@file"} 10`)
	}))
	defer server.Close()

	cfg := Config{
		URL:              server.URL,
		Window:           25 * time.Millisecond,
		Interval:         10 * time.Millisecond,
		MinRequests:      50,
		Max5xxRatio:      0.05,
		Max4xxRatio:      -1,
		MaxMeanLatencyMS: -1,
	}

	result, err := Analyze(context.Background(), nil, cfg, []string{"api-green"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Verdict != VerdictInsufficientData {
		t.Fatalf("expected insufficient_data verdict, got %s", result.Verdict)
	}
}
