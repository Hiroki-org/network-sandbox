package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLoadConfig tests configuration loading from environment
func TestLoadConfig(t *testing.T) {
	// Save original env vars
	origMax := os.Getenv("MAX_CONCURRENT_REQUESTS")
	origDelay := os.Getenv("RESPONSE_DELAY_MS")
	origFailure := os.Getenv("FAILURE_RATE")
	origQueue := os.Getenv("QUEUE_SIZE")

	// Restore env vars after test
	defer func() {
		os.Setenv("MAX_CONCURRENT_REQUESTS", origMax)
		os.Setenv("RESPONSE_DELAY_MS", origDelay)
		os.Setenv("FAILURE_RATE", origFailure)
		os.Setenv("QUEUE_SIZE", origQueue)
	}()

	tests := []struct {
		name     string
		envVars  map[string]string
		expected Configuration
	}{
		{
			name:    "default configuration",
			envVars: map[string]string{},
			expected: Configuration{
				MaxConcurrentRequests: 10,
				ResponseDelayMs:       100,
				FailureRate:           0.0,
				QueueSize:             50,
			},
		},
		{
			name: "custom configuration",
			envVars: map[string]string{
				"MAX_CONCURRENT_REQUESTS": "20",
				"RESPONSE_DELAY_MS":       "200",
				"FAILURE_RATE":            "0.1",
				"QUEUE_SIZE":              "100",
			},
			expected: Configuration{
				MaxConcurrentRequests: 20,
				ResponseDelayMs:       200,
				FailureRate:           0.1,
				QueueSize:             100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars
			os.Unsetenv("MAX_CONCURRENT_REQUESTS")
			os.Unsetenv("RESPONSE_DELAY_MS")
			os.Unsetenv("FAILURE_RATE")
			os.Unsetenv("QUEUE_SIZE")

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := loadConfig()

			if cfg.MaxConcurrentRequests != tt.expected.MaxConcurrentRequests {
				t.Errorf("MaxConcurrentRequests = %d, want %d", cfg.MaxConcurrentRequests, tt.expected.MaxConcurrentRequests)
			}
			if cfg.ResponseDelayMs != tt.expected.ResponseDelayMs {
				t.Errorf("ResponseDelayMs = %d, want %d", cfg.ResponseDelayMs, tt.expected.ResponseDelayMs)
			}
			if cfg.FailureRate != tt.expected.FailureRate {
				t.Errorf("FailureRate = %f, want %f", cfg.FailureRate, tt.expected.FailureRate)
			}
			if cfg.QueueSize != tt.expected.QueueSize {
				t.Errorf("QueueSize = %d, want %d", cfg.QueueSize, tt.expected.QueueSize)
			}
		})
	}
}

// TestConfigurationUpdate tests configuration update functionality
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

// TestConfigurationUpdateInvalidValues tests that invalid values are rejected
func TestConfigurationUpdateInvalidValues(t *testing.T) {
	cfg := &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       100,
		FailureRate:           0.5,
		QueueSize:             50,
	}

	// Test invalid values (should not update)
	invalidCfg := &Configuration{
		MaxConcurrentRequests: -1,  // Invalid
		ResponseDelayMs:       -10, // Invalid
		FailureRate:           1.5, // Invalid
		QueueSize:             -5,  // Invalid
	}

	cfg.Update(invalidCfg)

	// Original values should be preserved
	if cfg.MaxConcurrentRequests != 10 {
		t.Errorf("MaxConcurrentRequests should remain 10, got %d", cfg.MaxConcurrentRequests)
	}
	if cfg.QueueSize != 50 {
		t.Errorf("QueueSize should remain 50, got %d", cfg.QueueSize)
	}
	if cfg.FailureRate != 0.5 {
		t.Errorf("FailureRate should remain 0.5, got %f", cfg.FailureRate)
	}
}

// TestConfigurationGet tests thread-safe configuration retrieval
func TestConfigurationGet(t *testing.T) {
	cfg := &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       100,
		FailureRate:           0.0,
		QueueSize:             50,
	}

	retrieved := cfg.Get()

	if retrieved.MaxConcurrentRequests != cfg.MaxConcurrentRequests {
		t.Errorf("retrieved config mismatch")
	}
}

// TestHandleTaskSuccess tests successful task handling
func TestHandleTaskSuccess(t *testing.T) {
	// Initialize global config and worker info
	config = &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       10, // Short delay for testing
		FailureRate:           0.0,
		QueueSize:             50,
	}
	workerName = "test-worker"
	workerColor = "#FF0000"
	requestQueue = make(chan struct{}, 50)
	atomic.StoreInt32(&activeRequests, 0)

	task := TaskRequest{
		ID:     "test-task-1",
		Weight: 1.0,
	}

	body, _ := json.Marshal(task)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleTask(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response TaskResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.ID != task.ID {
		t.Errorf("response ID = %s, want %s", response.ID, task.ID)
	}
	if response.Worker != workerName {
		t.Errorf("response Worker = %s, want %s", response.Worker, workerName)
	}
	if response.Color != workerColor {
		t.Errorf("response Color = %s, want %s", response.Color, workerColor)
	}
	if response.ProcessingTimeMs <= 0 {
		t.Error("processing time should be positive")
	}
}

// TestHandleTaskInvalidMethod tests invalid HTTP method rejection
func TestHandleTaskInvalidMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/task", nil)
	rec := httptest.NewRecorder()

	handleTask(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleTaskInvalidJSON tests invalid JSON body handling
func TestHandleTaskInvalidJSON(t *testing.T) {
	config = loadConfig()
	workerName = "test-worker"
	requestQueue = make(chan struct{}, 50)
	atomic.StoreInt32(&activeRequests, 0)

	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader([]byte("invalid json")))
	rec := httptest.NewRecorder()

	handleTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestHandleTaskQueueFull tests queue full rejection
func TestHandleTaskQueueFull(t *testing.T) {
	config = loadConfig()
	workerName = "test-worker"
	requestQueue = make(chan struct{}, 0) // No capacity
	atomic.StoreInt32(&activeRequests, 0)

	task := TaskRequest{ID: "test-task", Weight: 1.0}
	body, _ := json.Marshal(task)

	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleTask(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if response.Worker != workerName {
		t.Errorf("error response worker = %s, want %s", response.Worker, workerName)
	}
}

// TestHandleTaskMaxConcurrency tests max concurrent requests limit
func TestHandleTaskMaxConcurrency(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 1,
		ResponseDelayMs:       10,
		FailureRate:           0.0,
		QueueSize:             50,
	}
	workerName = "test-worker"
	requestQueue = make(chan struct{}, 50)
	atomic.StoreInt32(&activeRequests, 2) // Already at max

	task := TaskRequest{ID: "test-task", Weight: 1.0}
	body, _ := json.Marshal(task)

	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleTask(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// TestHandleTaskWithWeight tests task processing with different weights
func TestHandleTaskWithWeight(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       10,
		FailureRate:           0.0,
		QueueSize:             50,
	}
	workerName = "test-worker"
	workerColor = "#FF0000"
	requestQueue = make(chan struct{}, 50)
	atomic.StoreInt32(&activeRequests, 0)

	tests := []struct {
		name   string
		weight float64
	}{
		{"weight 0.5", 0.5},
		{"weight 1.0", 1.0},
		{"weight 2.0", 2.0},
		{"weight 0 (should default to 1)", 0.0},
		{"weight negative (should default to 1)", -1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := TaskRequest{ID: "test-task", Weight: tt.weight}
			body, _ := json.Marshal(task)

			req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
			rec := httptest.NewRecorder()

			handleTask(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

// TestHandleHealth tests health check endpoint
func TestHandleHealth(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       100,
		FailureRate:           0.0,
		QueueSize:             50,
	}
	requestQueue = make(chan struct{}, 50)
	atomic.StoreInt32(&activeRequests, 3)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.CurrentLoad != 3 {
		t.Errorf("CurrentLoad = %d, want 3", response.CurrentLoad)
	}
	if response.Status != "healthy" {
		t.Errorf("Status = %s, want healthy", response.Status)
	}
}

// TestHandleHealthDifferentStatuses tests health status calculation
func TestHandleHealthDifferentStatuses(t *testing.T) {
	tests := []struct {
		name         string
		load         int32
		queueDepth   int
		maxConcur    int
		queueSize    int
		wantStatus   string
	}{
		{"healthy low load", 2, 5, 10, 50, "healthy"},
		{"degraded high load", 8, 10, 10, 50, "degraded"},
		{"unhealthy critical load", 10, 48, 10, 50, "unhealthy"},
		{"degraded high queue", 3, 40, 10, 50, "degraded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config = &Configuration{
				MaxConcurrentRequests: tt.maxConcur,
				ResponseDelayMs:       100,
				FailureRate:           0.0,
				QueueSize:             tt.queueSize,
			}
			requestQueue = make(chan struct{}, tt.queueSize)

			// Fill queue to desired depth
			for i := 0; i < tt.queueDepth; i++ {
				requestQueue <- struct{}{}
			}
			defer func() {
				// Drain queue
				for len(requestQueue) > 0 {
					<-requestQueue
				}
			}()

			atomic.StoreInt32(&activeRequests, tt.load)

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()

			handleHealth(rec, req)

			var response HealthResponse
			json.NewDecoder(rec.Body).Decode(&response)

			if response.Status != tt.wantStatus {
				t.Errorf("Status = %s, want %s", response.Status, tt.wantStatus)
			}
		})
	}
}

// TestHandleHealthInvalidMethod tests health endpoint with invalid method
func TestHandleHealthInvalidMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	handleHealth(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleConfigGet tests getting current configuration
func TestHandleConfigGet(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 15,
		ResponseDelayMs:       150,
		FailureRate:           0.05,
		QueueSize:             75,
	}

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rec := httptest.NewRecorder()

	handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response Configuration
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.MaxConcurrentRequests != 15 {
		t.Errorf("MaxConcurrentRequests = %d, want 15", response.MaxConcurrentRequests)
	}
}

// TestHandleConfigUpdate tests updating configuration
func TestHandleConfigUpdate(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       100,
		FailureRate:           0.0,
		QueueSize:             50,
	}

	newCfg := Configuration{
		MaxConcurrentRequests: 20,
		ResponseDelayMs:       200,
		FailureRate:           0.1,
		QueueSize:             100,
	}

	body, _ := json.Marshal(newCfg)
	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	if config.MaxConcurrentRequests != 20 {
		t.Errorf("config not updated: MaxConcurrentRequests = %d, want 20", config.MaxConcurrentRequests)
	}
}

// TestHandleConfigInvalidJSON tests config update with invalid JSON
func TestHandleConfigInvalidJSON(t *testing.T) {
	config = loadConfig()

	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader([]byte("invalid")))
	rec := httptest.NewRecorder()

	handleConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestCORSMiddleware tests CORS middleware functionality
func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS Allow-Origin header not set")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("CORS Allow-Methods header not set")
	}

	// Test OPTIONS request
	req = httptest.NewRequest(http.MethodOptions, "/", nil)
	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("OPTIONS status code = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestConcurrentTaskHandling tests handling multiple concurrent tasks
func TestConcurrentTaskHandling(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 100,
		ResponseDelayMs:       10,
		FailureRate:           0.0,
		QueueSize:             200,
	}
	workerName = "test-worker"
	workerColor = "#FF0000"
	requestQueue = make(chan struct{}, 200)
	atomic.StoreInt32(&activeRequests, 0)

	var wg sync.WaitGroup
	concurrentRequests := 50

	successCount := int32(0)

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(taskID int) {
			defer wg.Done()

			task := TaskRequest{
				ID:     "task-" + string(rune(taskID)),
				Weight: 1.0,
			}
			body, _ := json.Marshal(task)

			req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
			rec := httptest.NewRecorder()

			handleTask(rec, req)

			if rec.Code == http.StatusOK {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if successCount != int32(concurrentRequests) {
		t.Errorf("successful requests = %d, want %d", successCount, concurrentRequests)
	}
}

// TestGetEnvInt tests integer environment variable parsing
func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      string
		defaultVal int
		want       int
	}{
		{"valid integer", "TEST_INT", "42", 10, 42},
		{"invalid integer uses default", "TEST_INT", "invalid", 10, 10},
		{"empty value uses default", "TEST_INT", "", 10, 10},
		{"unset key uses default", "UNSET_KEY", "", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				os.Setenv(tt.key, tt.value)
				defer os.Unsetenv(tt.key)
			}

			got := getEnvInt(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetEnvFloat tests float environment variable parsing
func TestGetEnvFloat(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      string
		defaultVal float64
		want       float64
	}{
		{"valid float", "TEST_FLOAT", "3.14", 1.0, 3.14},
		{"invalid float uses default", "TEST_FLOAT", "invalid", 1.0, 1.0},
		{"empty value uses default", "TEST_FLOAT", "", 1.0, 1.0},
		{"unset key uses default", "UNSET_KEY", "", 1.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				os.Setenv(tt.key, tt.value)
				defer os.Unsetenv(tt.key)
			}

			got := getEnvFloat(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSimulatedFailure tests task failure simulation
func TestSimulatedFailure(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       10,
		FailureRate:           1.0, // Always fail
		QueueSize:             50,
	}
	workerName = "test-worker"
	requestQueue = make(chan struct{}, 50)
	atomic.StoreInt32(&activeRequests, 0)

	task := TaskRequest{ID: "test-task", Weight: 1.0}
	body, _ := json.Marshal(task)

	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleTask(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if response.Worker != workerName {
		t.Errorf("error response worker = %s, want %s", response.Worker, workerName)
	}
}

// TestActiveRequestsTracking tests that active requests are tracked correctly
func TestActiveRequestsTracking(t *testing.T) {
	config = &Configuration{
		MaxConcurrentRequests: 10,
		ResponseDelayMs:       50, // Longer delay to allow concurrent execution
		FailureRate:           0.0,
		QueueSize:             50,
	}
	workerName = "test-worker"
	workerColor = "#FF0000"
	requestQueue = make(chan struct{}, 50)
	atomic.StoreInt32(&activeRequests, 0)

	var wg sync.WaitGroup
	concurrentTasks := 5

	// Start concurrent tasks
	for i := 0; i < concurrentTasks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task := TaskRequest{ID: "test-task", Weight: 1.0}
			body, _ := json.Marshal(task)
			req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			handleTask(rec, req)
		}()
	}

	// Wait a bit for tasks to start
	time.Sleep(10 * time.Millisecond)

	// Check that active requests were incremented
	active := atomic.LoadInt32(&activeRequests)
	if active <= 0 {
		t.Logf("active requests during execution: %d (may vary due to timing)", active)
	}

	wg.Wait()

	// After all complete, should be back to 0
	final := atomic.LoadInt32(&activeRequests)
	if final != 0 {
		t.Errorf("final active requests = %d, want 0", final)
	}
}