package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestUnboundedGoroutines(t *testing.T) {
	// Create a slow mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Simulate slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	lb := NewLoadBalancer()
	// Add many workers pointing to the slow server
	numWorkers := 50
	for i := 0; i < numWorkers; i++ {
		lb.AddWorker(fmt.Sprintf("worker-%d", i), ts.URL, "red", 1, 10)
	}

	// Initial goroutines
	initialGoroutines := runtime.NumGoroutine()
	t.Logf("Initial goroutines: %d", initialGoroutines)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HealthCheck with very short interval (shorter than response time)
	// Response is 200ms. Interval 10ms.
	// In 1 second, we have 100 checks triggered per worker.
	// Without fix: we expect potentially 50 * 5 = 250 concurrent checks growing to 50 * 100?
	// Actually max concurrent checks depends on how fast they pile up vs finish.
	// If check takes 200ms, and we trigger every 10ms.
	// We will have roughly 200/10 = 20 overlapping checks per worker.
	// 50 workers * 20 = 1000 goroutines.
	go lb.HealthCheck(ctx, 10*time.Millisecond)

	// Monitor for 1 second
	time.Sleep(1 * time.Second)

	finalGoroutines := runtime.NumGoroutine()
	t.Logf("Final goroutines: %d", finalGoroutines)

	// With 50 workers, we might expect ~50-60 goroutines max if strictly limited (one per worker).
	// Without limit, we expect ~1000.
	// We set a threshold of 300 to be safe.
	if finalGoroutines > initialGoroutines+300 {
		t.Errorf("FAIL: Goroutine count grew significantly: %d -> %d", initialGoroutines, finalGoroutines)
	} else {
		t.Logf("PASS: Goroutine count within limits: %d -> %d", initialGoroutines, finalGoroutines)
	}
}
