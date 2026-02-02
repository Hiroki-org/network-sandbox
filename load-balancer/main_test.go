package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleWorkerConfigGet(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	// Create a mock worker server that returns config
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"max_concurrent_requests": 5,
			"response_delay_ms":       500,
			"failure_rate":            0.02,
			"queue_size":              10,
		})
	}))
	defer workerServer.Close()

	// Update worker URL to point to mock server
	lb.mu.Lock()
	lb.workers[0].URL = workerServer.URL
	lb.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/workers/test-worker/config", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["worker"] != "test-worker" {
		t.Errorf("worker = %v, want test-worker", response["worker"])
	}

	if response["max_concurrent_requests"] != float64(5) {
		t.Errorf("max_concurrent_requests = %v, want 5", response["max_concurrent_requests"])
	}
}

func TestHandleWorkerConfigPut(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	// Create a mock worker server that accepts config updates
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var config map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
	}))
	defer workerServer.Close()

	// Update worker URL to point to mock server
	lb.mu.Lock()
	lb.workers[0].URL = workerServer.URL
	lb.mu.Unlock()

	body := bytes.NewBufferString(`{"max_concurrent_requests":10}`)
	req := httptest.NewRequest(http.MethodPut, "/workers/test-worker/config", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["worker"] != "test-worker" {
		t.Errorf("worker = %v, want test-worker", response["worker"])
	}

	if response["max_concurrent_requests"] != float64(10) {
		t.Errorf("max_concurrent_requests = %v, want 10", response["max_concurrent_requests"])
	}
}

func TestHandleWorkerConfigPost(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	// Create a mock worker server that accepts config updates via POST
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var config map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
	}))
	defer workerServer.Close()

	// Update worker URL to point to mock server
	lb.mu.Lock()
	lb.workers[0].URL = workerServer.URL
	lb.mu.Unlock()

	body := bytes.NewBufferString(`{"failure_rate":0.5}`)
	req := httptest.NewRequest(http.MethodPost, "/workers/test-worker/config", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["worker"] != "test-worker" {
		t.Errorf("worker = %v, want test-worker", response["worker"])
	}
}

func TestHandleWorkerConfigWorkerNotFound(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	req := httptest.NewRequest(http.MethodGet, "/workers/nonexistent-worker/config", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleWorkerConfigInvalidPath(t *testing.T) {
	lb = NewLoadBalancer()

	req := httptest.NewRequest(http.MethodGet, "/workers/", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleWorkerConfigMethodNotAllowed(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	req := httptest.NewRequest(http.MethodDelete, "/workers/test-worker/config", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleWorkerConfigWorkerTimeout(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	// Create a mock worker server that delays response
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Longer than the 5s timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer workerServer.Close()

	// Update worker URL to point to mock server
	lb.mu.Lock()
	lb.workers[0].URL = workerServer.URL
	lb.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/workers/test-worker/config", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleWorkerConfigWorkerError(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	// Create a mock worker server that returns an error
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
	}))
	defer workerServer.Close()

	// Update worker URL to point to mock server
	lb.mu.Lock()
	lb.workers[0].URL = workerServer.URL
	lb.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/workers/test-worker/config", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	// The handler copies the worker's status code
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestCORSMiddlewareUpdatedMethods(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsHandler := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	corsHandler.ServeHTTP(w, req)

	allowedMethods := w.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(allowedMethods, "PATCH") {
		t.Error("CORS methods should include PATCH")
	}
	if !strings.Contains(allowedMethods, "DELETE") {
		t.Error("CORS methods should include DELETE")
	}
}

func TestUpdateWorkerHandlesWeight(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	newWeight := 10
	success := lb.UpdateWorker("test-worker", nil, &newWeight)

	if !success {
		t.Error("UpdateWorker should return true for existing worker")
	}

	if lb.workers[0].Weight != 10 {
		t.Errorf("worker weight = %d, want 10", lb.workers[0].Weight)
	}
}

func TestUpdateWorkerIgnoresZeroWeight(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	zeroWeight := 0
	lb.UpdateWorker("test-worker", nil, &zeroWeight)

	// Weight should remain unchanged
	if lb.workers[0].Weight != 2 {
		t.Errorf("worker weight = %d, want 2 (unchanged)", lb.workers[0].Weight)
	}
}

func TestUpdateWorkerIgnoresNegativeWeight(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	negWeight := -5
	lb.UpdateWorker("test-worker", nil, &negWeight)

	// Weight should remain unchanged
	if lb.workers[0].Weight != 2 {
		t.Errorf("worker weight = %d, want 2 (unchanged)", lb.workers[0].Weight)
	}
}

func TestWebSocketOriginValidation(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins string
		requestOrigin  string
		shouldAllow    bool
	}{
		{"no restriction in dev mode", "", "http://example.com", true},
		{"allowed origin", "http://localhost:3000", "http://localhost:3000", true},
		{"disallowed origin", "http://localhost:3000", "http://evil.com", false},
		{"multiple allowed origins", "http://localhost:3000,http://localhost:3001", "http://localhost:3001", true},
		{"not in allowed list", "http://localhost:3000,http://localhost:3001", "http://example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.allowedOrigins != "" {
				t.Setenv("ALLOWED_ORIGINS", tt.allowedOrigins)
			}

			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			req.Header.Set("Origin", tt.requestOrigin)

			allowed := upgrader.CheckOrigin(req)

			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin() = %v, want %v", allowed, tt.shouldAllow)
			}
		})
	}
}

func TestGetStatusIncludesAllWorkerFields(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	status := lb.GetStatus()

	workers, ok := status["workers"].([]map[string]interface{})
	if !ok {
		t.Fatal("workers is not the expected type")
	}

	if len(workers) != 1 {
		t.Fatalf("expected 1 worker in status, got %d", len(workers))
	}

	worker := workers[0]

	// Check all expected fields are present
	expectedFields := []string{
		"name", "url", "color", "weight", "maxLoad",
		"healthy", "currentLoad", "enabled",
		"totalRequests", "failedRequests", "circuitOpen",
	}

	for _, field := range expectedFields {
		if _, exists := worker[field]; !exists {
			t.Errorf("worker status missing field: %s", field)
		}
	}
}

func TestWorkerRouting(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	// Create a mock worker server
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"max_concurrent_requests": 5,
		})
	}))
	defer workerServer.Close()

	lb.mu.Lock()
	lb.workers[0].URL = workerServer.URL
	lb.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/workers/", func(w http.ResponseWriter, r *http.Request) {
		// Route to config handler if path contains /config
		if strings.Contains(r.URL.Path, "/config") {
			handleWorkerConfig(w, r)
		} else {
			handleWorker(w, r)
		}
	})

	// Test config route
	req := httptest.NewRequest(http.MethodGet, "/workers/test-worker/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("config route status code = %d, want %d", w.Code, http.StatusOK)
	}

	// Test worker update route
	body := bytes.NewBufferString(`{"enabled":false}`)
	req = httptest.NewRequest(http.MethodPatch, "/workers/test-worker", body)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("worker update route status code = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleWorkerConfigWithMissingWorker(t *testing.T) {
	lb = NewLoadBalancer()

	req := httptest.NewRequest(http.MethodGet, "/workers/missing/config", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleWorkerConfigInvalidJSON(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	// Create a mock worker server
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer workerServer.Close()

	lb.mu.Lock()
	lb.workers[0].URL = workerServer.URL
	lb.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/workers/test-worker/config", nil)
	w := httptest.NewRecorder()

	handleWorkerConfig(w, req)

	// Should still succeed (we just couldn't decode response, but that's ok)
	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleAlgorithmReturnsAvailableAlgorithms(t *testing.T) {
	lb = NewLoadBalancer()

	mux := http.NewServeMux()
	mux.HandleFunc("/algorithm", handleAlgorithm)

	req := httptest.NewRequest(http.MethodGet, "/algorithm", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, exists := response["available"]; !exists {
		t.Error("response should include 'available' field")
	}
}

func TestMaxLoadWorkerTracking(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 5)

	worker := lb.workers[0]
	if worker.MaxLoad != 5 {
		t.Errorf("worker.MaxLoad = %d, want 5", worker.MaxLoad)
	}

	// Verify maxLoad appears in status
	status := lb.GetStatus()
	workers := status["workers"].([]map[string]interface{})
	if workers[0]["maxLoad"] != 5 {
		t.Errorf("status maxLoad = %v, want 5", workers[0]["maxLoad"])
	}
}