package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("MAX_CONCURRENT_REQUESTS", "20")
	t.Setenv("RESPONSE_DELAY_MS", "150")
	t.Setenv("FAILURE_RATE", "0.1")
	t.Setenv("QUEUE_SIZE", "100")

	cfg := loadConfig()

	if cfg.MaxConcurrentRequests != 20 {
		t.Errorf("MaxConcurrentRequests = %d, want 20", cfg.MaxConcurrentRequests)
	}
	if cfg.ResponseDelayMs != 150 {
		t.Errorf("ResponseDelayMs = %d, want 150", cfg.ResponseDelayMs)
	}
	if cfg.FailureRate != 0.1 {
		t.Errorf("FailureRate = %f, want 0.1", cfg.FailureRate)
	}
	if cfg.QueueSize != 100 {
		t.Errorf("QueueSize = %d, want 100", cfg.QueueSize)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg := loadConfig()

	if cfg.MaxConcurrentRequests <= 0 {
		t.Error("MaxConcurrentRequests should have a positive default")
	}
	if cfg.ResponseDelayMs < 0 {
		t.Error("ResponseDelayMs should be non-negative")
	}
	if cfg.FailureRate < 0 || cfg.FailureRate > 1 {
		t.Error("FailureRate should be between 0 and 1")
	}
	if cfg.QueueSize <= 0 {
		t.Error("QueueSize should have a positive default")
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		defaultVal int
		envVal     string
		want       int
	}{
		{"with valid env", "TEST_INT", 10, "25", 25},
		{"without env", "NONEXISTENT", 10, "", 10},
		{"with invalid env", "TEST_INVALID", 10, "invalid", 10},
		{"with zero", "TEST_ZERO", 10, "0", 0},
		{"negative value", "TEST_NEG", 10, "-5", -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv(tt.key, tt.envVal)
			}

			got := getEnvInt(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetEnvFloat(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		defaultVal float64
		envVal     string
		want       float64
	}{
		{"with valid env", "TEST_FLOAT", 0.5, "0.75", 0.75},
		{"without env", "NONEXISTENT", 0.5, "", 0.5},
		{"with invalid env", "TEST_INVALID", 0.5, "invalid", 0.5},
		{"with zero", "TEST_ZERO", 0.5, "0", 0.0},
		{"negative value", "TEST_NEG", 0.5, "-0.25", -0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv(tt.key, tt.envVal)
			}

			got := getEnvFloat(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvFloat() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestConfigurationUpdate(t *testing.T) {
	cfg := &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       100,
		FailureRate:           0.0,
		QueueSize:             50,
	}

	newCfg := &Configuration{
		MaxConcurrentRequests: 20,
		ResponseDelayMs:       200,
		FailureRate:           0.1,
		QueueSize:             100,
	}

	cfg.Update(newCfg)

	if cfg.MaxConcurrentRequests != 20 {
		t.Errorf("MaxConcurrentRequests = %d, want 20", cfg.MaxConcurrentRequests)
	}
	if cfg.ResponseDelayMs != 200 {
		t.Errorf("ResponseDelayMs = %d, want 200", cfg.ResponseDelayMs)
	}
	if cfg.FailureRate != 0.1 {
		t.Errorf("FailureRate = %f, want 0.1", cfg.FailureRate)
	}
	if cfg.QueueSize != 100 {
		t.Errorf("QueueSize = %d, want 100", cfg.QueueSize)
	}
}

func TestConfigurationUpdateInvalidValues(t *testing.T) {
	cfg := &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       100,
		FailureRate:           0.0,
		QueueSize:             50,
	}

	newCfg := &Configuration{
		MaxConcurrentRequests: -1,  // Invalid
		ResponseDelayMs:       -50, // Should be rejected
		FailureRate:           1.5, // Invalid (> 1)
		QueueSize:             0,   // Invalid
	}

	cfg.Update(newCfg)

	// Original values should be preserved for invalid inputs
	if cfg.MaxConcurrentRequests != 10 {
		t.Errorf("MaxConcurrentRequests should remain 10, got %d", cfg.MaxConcurrentRequests)
	}
	if cfg.QueueSize != 50 {
		t.Errorf("QueueSize should remain 50, got %d", cfg.QueueSize)
	}
}

func TestConfigurationGet(t *testing.T) {
	cfg := &Configuration{
		MaxConcurrentRequests: 15,
		ResponseDelayMs:       120,
		FailureRate:           0.05,
		QueueSize:             60,
	}

	got := cfg.Get()

	if got.MaxConcurrentRequests != 15 {
		t.Errorf("MaxConcurrentRequests = %d, want 15", got.MaxConcurrentRequests)
	}
	if got.ResponseDelayMs != 120 {
		t.Errorf("ResponseDelayMs = %d, want 120", got.ResponseDelayMs)
	}
	if got.FailureRate != 0.05 {
		t.Errorf("FailureRate = %f, want 0.05", got.FailureRate)
	}
	if got.QueueSize != 60 {
		t.Errorf("QueueSize = %d, want 60", got.QueueSize)
	}
}

func setupTestEnvironment() {
	config = loadConfig()
	workerName = "test-worker"
	workerColor = "#FF0000"
	requestQueue = make(chan struct{}, config.QueueSize)
	atomic.StoreInt32(&activeRequests, 0)
}

func TestHandleHealthGet(t *testing.T) {
	setupTestEnvironment()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Status == "" {
		t.Error("status should not be empty")
	}
}

func TestHandleHealthMethodNotAllowed(t *testing.T) {
	setupTestEnvironment()

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	handleHealth(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleHealthStatus(t *testing.T) {
	setupTestEnvironment()
	config.MaxConcurrentRequests = 10
	config.QueueSize = 50

	tests := []struct {
		name           string
		currentLoad    int32
		queueDepth     int
		expectedStatus string
	}{
		{"healthy", 2, 5, "healthy"},
		{"degraded load", 7, 5, "degraded"},
		{"degraded queue", 2, 36, "degraded"},
		{"unhealthy load", 9, 5, "unhealthy"},
		{"unhealthy queue", 2, 46, "unhealthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			atomic.StoreInt32(&activeRequests, tt.currentLoad)

			// Clear queue and add items
			for len(requestQueue) > 0 {
				<-requestQueue
			}
			for i := 0; i < tt.queueDepth; i++ {
				requestQueue <- struct{}{}
			}

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()

			handleHealth(w, req)

			var response HealthResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if response.Status != tt.expectedStatus {
				t.Errorf("status = %s, want %s", response.Status, tt.expectedStatus)
			}
			if response.CurrentLoad != tt.currentLoad {
				t.Errorf("currentLoad = %d, want %d", response.CurrentLoad, tt.currentLoad)
			}

			// Clean up queue
			for len(requestQueue) > 0 {
				<-requestQueue
			}
		})
	}
}

func TestHandleTaskPost(t *testing.T) {
	setupTestEnvironment()
	config.MaxConcurrentRequests = 10
	config.ResponseDelayMs = 10
	config.FailureRate = 0.0

	taskReq := TaskRequest{
		ID:     "test-task-1",
		Weight: 1.0,
	}

	body, _ := json.Marshal(taskReq)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleTask(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.ID != "test-task-1" {
		t.Errorf("task ID = %s, want test-task-1", response.ID)
	}
	if response.Worker != workerName {
		t.Errorf("worker = %s, want %s", response.Worker, workerName)
	}
	if response.ProcessingTimeMs <= 0 {
		t.Error("processing time should be positive")
	}
}

func TestHandleTaskMethodNotAllowed(t *testing.T) {
	setupTestEnvironment()

	req := httptest.NewRequest(http.MethodGet, "/task", nil)
	w := httptest.NewRecorder()

	handleTask(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleTaskInvalidJSON(t *testing.T) {
	setupTestEnvironment()

	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var response ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if response.Error == "" {
		t.Error("error message should not be empty")
	}
}

func TestHandleTaskQueueFull(t *testing.T) {
	setupTestEnvironment()
	config.QueueSize = 2

	// Fill the queue
	requestQueue = make(chan struct{}, 2)
	requestQueue <- struct{}{}
	requestQueue <- struct{}{}

	taskReq := TaskRequest{ID: "test-task", Weight: 1.0}
	body, _ := json.Marshal(taskReq)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleTask(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var response ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if !bytes.Contains([]byte(response.Error), []byte("Queue full")) {
		t.Errorf("error should mention queue full, got: %s", response.Error)
	}
}

func TestHandleTaskMaxConcurrentExceeded(t *testing.T) {
	setupTestEnvironment()
	config.MaxConcurrentRequests = 2
	config.ResponseDelayMs = 100
	config.QueueSize = 10

	var wg sync.WaitGroup

	// Start 2 concurrent requests
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			taskReq := TaskRequest{ID: "bg-task", Weight: 1.0}
			body, _ := json.Marshal(taskReq)
			req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handleTask(w, req)
		}()
	}

	// Wait a bit for them to start processing
	time.Sleep(20 * time.Millisecond)

	// Try to send one more (should be rejected)
	taskReq := TaskRequest{ID: "test-task", Weight: 1.0}
	body, _ := json.Marshal(taskReq)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleTask(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	wg.Wait()
}

func TestHandleTaskWithWeight(t *testing.T) {
	setupTestEnvironment()
	config.MaxConcurrentRequests = 10
	config.ResponseDelayMs = 10
	config.FailureRate = 0.0

	tests := []struct {
		name   string
		weight float64
	}{
		{"weight 1.0", 1.0},
		{"weight 2.0", 2.0},
		{"weight 0.5", 0.5},
		{"weight 0", 0.0}, // Should default to 1
		{"weight negative", -1.0}, // Should default to 1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskReq := TaskRequest{
				ID:     "test-task",
				Weight: tt.weight,
			}

			body, _ := json.Marshal(taskReq)
			req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			start := time.Now()
			handleTask(w, req)
			duration := time.Since(start)

			if w.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
			}

			// Check that delay is applied (rough check)
			expectedWeight := tt.weight
			if expectedWeight <= 0 {
				expectedWeight = 1
			}
			expectedDelay := time.Duration(float64(config.ResponseDelayMs)*expectedWeight) * time.Millisecond

			if duration < expectedDelay/2 {
				t.Errorf("duration %v too short, expected around %v", duration, expectedDelay)
			}
		})
	}
}

func TestHandleTaskSimulatedFailure(t *testing.T) {
	setupTestEnvironment()
	config.MaxConcurrentRequests = 10
	config.ResponseDelayMs = 10
	config.FailureRate = 1.0 // Always fail

	taskReq := TaskRequest{
		ID:     "test-task",
		Weight: 1.0,
	}

	body, _ := json.Marshal(taskReq)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleTask(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	var response ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if !bytes.Contains([]byte(response.Error), []byte("Simulated failure")) {
		t.Errorf("error should mention simulated failure, got: %s", response.Error)
	}
}

func TestHandleConfigGet(t *testing.T) {
	setupTestEnvironment()

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	w := httptest.NewRecorder()

	handleConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response Configuration
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify config values
	if response.MaxConcurrentRequests != config.MaxConcurrentRequests {
		t.Errorf("MaxConcurrentRequests mismatch")
	}
}

func TestHandleConfigPut(t *testing.T) {
	setupTestEnvironment()

	newCfg := Configuration{
		MaxConcurrentRequests: 20,
		ResponseDelayMs:       200,
		FailureRate:           0.2,
		QueueSize:             100,
	}

	body, _ := json.Marshal(newCfg)
	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	cfg := config.Get()
	if cfg.MaxConcurrentRequests != 20 {
		t.Errorf("MaxConcurrentRequests = %d, want 20", cfg.MaxConcurrentRequests)
	}
	if cfg.ResponseDelayMs != 200 {
		t.Errorf("ResponseDelayMs = %d, want 200", cfg.ResponseDelayMs)
	}
}

func TestHandleConfigPost(t *testing.T) {
	setupTestEnvironment()

	newCfg := Configuration{
		MaxConcurrentRequests: 15,
		ResponseDelayMs:       150,
		FailureRate:           0.15,
		QueueSize:             75,
	}

	body, _ := json.Marshal(newCfg)
	req := httptest.NewRequest(http.MethodPost, "/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleConfigInvalidJSON(t *testing.T) {
	setupTestEnvironment()

	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleConfigMethodNotAllowed(t *testing.T) {
	setupTestEnvironment()

	req := httptest.NewRequest(http.MethodDelete, "/config", nil)
	w := httptest.NewRecorder()

	handleConfig(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsHandler := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	corsHandler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS origin header not set correctly")
	}
	if w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, PUT, OPTIONS" {
		t.Error("CORS methods header not set correctly")
	}
	if w.Header().Get("Access-Control-Allow-Headers") != "Content-Type" {
		t.Error("CORS headers header not set correctly")
	}
}

func TestCORSPreflightRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsHandler := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	w := httptest.NewRecorder()

	corsHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestConcurrentTaskHandling(t *testing.T) {
	setupTestEnvironment()
	config.MaxConcurrentRequests = 50
	config.ResponseDelayMs = 10
	config.FailureRate = 0.0
	config.QueueSize = 100

	var wg sync.WaitGroup
	successCount := int32(0)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			taskReq := TaskRequest{
				ID:     "concurrent-task",
				Weight: 1.0,
			}

			body, _ := json.Marshal(taskReq)
			req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handleTask(w, req)

			if w.Code == http.StatusOK {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if successCount != 20 {
		t.Errorf("successCount = %d, want 20", successCount)
	}

	// Verify final state
	finalLoad := atomic.LoadInt32(&activeRequests)
	if finalLoad != 0 {
		t.Errorf("activeRequests should be 0 after all tasks complete, got %d", finalLoad)
	}
}

func TestTaskResponseStructure(t *testing.T) {
	resp := TaskResponse{
		ID:               "task-123",
		Worker:           "test-worker",
		Color:            "#FF0000",
		ProcessingTimeMs: 150,
		Timestamp:        "2024-01-01T00:00:00Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var decoded TaskResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if decoded.ID != "task-123" {
		t.Errorf("ID = %s, want task-123", decoded.ID)
	}
	if decoded.Worker != "test-worker" {
		t.Errorf("Worker = %s, want test-worker", decoded.Worker)
	}
	if decoded.ProcessingTimeMs != 150 {
		t.Errorf("ProcessingTimeMs = %d, want 150", decoded.ProcessingTimeMs)
	}
}

func TestErrorResponseStructure(t *testing.T) {
	resp := ErrorResponse{
		Error:  "Test error",
		Worker: "test-worker",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var decoded ErrorResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if decoded.Error != "Test error" {
		t.Errorf("Error = %s, want Test error", decoded.Error)
	}
	if decoded.Worker != "test-worker" {
		t.Errorf("Worker = %s, want test-worker", decoded.Worker)
	}
}

func TestHealthResponseStructure(t *testing.T) {
	resp := HealthResponse{
		Status:      "healthy",
		CurrentLoad: 5,
		QueueDepth:  10,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var decoded HealthResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if decoded.Status != "healthy" {
		t.Errorf("Status = %s, want healthy", decoded.Status)
	}
	if decoded.CurrentLoad != 5 {
		t.Errorf("CurrentLoad = %d, want 5", decoded.CurrentLoad)
	}
	if decoded.QueueDepth != 10 {
		t.Errorf("QueueDepth = %d, want 10", decoded.QueueDepth)
	}
}

func TestActiveRequestsTracking(t *testing.T) {
	setupTestEnvironment()

	atomic.StoreInt32(&activeRequests, 0)

	// Simulate incrementing
	atomic.AddInt32(&activeRequests, 1)
	if atomic.LoadInt32(&activeRequests) != 1 {
		t.Error("activeRequests should be 1")
	}

	atomic.AddInt32(&activeRequests, 3)
	if atomic.LoadInt32(&activeRequests) != 4 {
		t.Error("activeRequests should be 4")
	}

	// Simulate decrementing
	atomic.AddInt32(&activeRequests, -2)
	if atomic.LoadInt32(&activeRequests) != 2 {
		t.Error("activeRequests should be 2")
	}

	atomic.AddInt32(&activeRequests, -2)
	if atomic.LoadInt32(&activeRequests) != 0 {
		t.Error("activeRequests should be 0")
	}
}

func TestConfigurationConcurrentAccess(t *testing.T) {
	cfg := &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       100,
		FailureRate:           0.0,
		QueueSize:             50,
	}

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cfg.Get()
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			newCfg := &Configuration{
				MaxConcurrentRequests: val,
				ResponseDelayMs:       val * 10,
				FailureRate:           0.0,
				QueueSize:             val * 5,
			}
			cfg.Update(newCfg)
		}(i + 1)
	}

	wg.Wait()

	// Should complete without data races
	final := cfg.Get()
	if final.MaxConcurrentRequests <= 0 {
		t.Error("final config should have valid MaxConcurrentRequests")
	}
}

func TestZeroWeightHandling(t *testing.T) {
	setupTestEnvironment()
	config.ResponseDelayMs = 10
	config.FailureRate = 0.0

	taskReq := TaskRequest{
		ID:     "test-task",
		Weight: 0,
	}

	body, _ := json.Marshal(taskReq)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleTask(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	// Should succeed with weight treated as 1
	var response TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestPrometheusMetricsRegistration(t *testing.T) {
	// This test verifies that metrics are properly initialized
	// The init() function should register metrics without panic

	// If we reach here, metrics were registered successfully
	if requestsTotal == nil {
		t.Error("requestsTotal metric not initialized")
	}
	if requestDuration == nil {
		t.Error("requestDuration metric not initialized")
	}
	if currentLoad == nil {
		t.Error("currentLoad metric not initialized")
	}
}