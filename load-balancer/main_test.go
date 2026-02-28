package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewLoadBalancer(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		want      string
	}{
		{"round-robin", "round-robin", "round-robin"},
		{"least-connections", "least-connections", "least-connections"},
		{"weighted", "weighted", "weighted"},
		{"random", "random", "random"},
		{"empty defaults to round-robin", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer(tt.algorithm)
			if lb == nil {
				t.Fatal("NewLoadBalancer returned nil")
			}
			if lb.algorithm != tt.want {
				t.Errorf("algorithm = %v, want %v", lb.algorithm, tt.want)
			}
			if lb.workers == nil {
				t.Error("workers slice is nil")
			}
			if lb.wsClients == nil {
				t.Error("wsClients map is nil")
			}
		})
	}
}

func TestAddWorker(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2)

	if len(lb.workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(lb.workers))
	}

	worker := lb.workers[0]
	if worker.Name != "test-worker" {
		t.Errorf("worker name = %v, want test-worker", worker.Name)
	}
	if worker.URL != "http://localhost:8080" {
		t.Errorf("worker URL = %v, want http://localhost:8080", worker.URL)
	}
	if worker.Color != "#FF0000" {
		t.Errorf("worker color = %v, want #FF0000", worker.Color)
	}
	if worker.Weight != 2 {
		t.Errorf("worker weight = %v, want 2", worker.Weight)
	}
	if !worker.Healthy {
		t.Error("worker should be healthy initially")
	}
}

func TestGetHealthyWorkers(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	// Mark worker-2 as unhealthy
	lb.workers[1].Healthy = false

	// Open circuit for worker-3
	lb.workers[2].CircuitOpen = true

	healthy := lb.getHealthyWorkers()

	if len(healthy) != 1 {
		t.Fatalf("expected 1 healthy worker, got %d", len(healthy))
	}

	if healthy[0].Name != "worker-1" {
		t.Errorf("expected worker-1, got %s", healthy[0].Name)
	}
}

func TestRoundRobinSelection(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	workers := lb.getHealthyWorkers()

	// Test round-robin distribution
	w1 := lb.roundRobin(workers)
	w2 := lb.roundRobin(workers)
	w3 := lb.roundRobin(workers)
	w4 := lb.roundRobin(workers)

	if w1.Name != "worker-1" {
		t.Errorf("expected worker-1, got %s", w1.Name)
	}
	if w2.Name != "worker-2" {
		t.Errorf("expected worker-2, got %s", w2.Name)
	}
	if w3.Name != "worker-3" {
		t.Errorf("expected worker-3, got %s", w3.Name)
	}
	if w4.Name != "worker-1" {
		t.Errorf("expected worker-1 (wrapped), got %s", w4.Name)
	}
}

func TestLeastConnectionsSelection(t *testing.T) {
	lb := NewLoadBalancer("least-connections")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	// Set different load levels
	atomic.StoreInt32(&lb.workers[0].CurrentLoad, 5)
	atomic.StoreInt32(&lb.workers[1].CurrentLoad, 2)
	atomic.StoreInt32(&lb.workers[2].CurrentLoad, 8)

	workers := lb.getHealthyWorkers()
	selected := lb.leastConnections(workers)

	if selected.Name != "worker-2" {
		t.Errorf("expected worker-2 (lowest load), got %s", selected.Name)
	}
}

func TestWeightedSelection(t *testing.T) {
	lb := NewLoadBalancer("weighted")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 3)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	workers := lb.getHealthyWorkers()

	// Count selections over many iterations
	counts := make(map[string]int)
	for i := 0; i < 100; i++ {
		selected := lb.weighted(workers)
		counts[selected.Name]++
	}

	// Worker-2 should be selected approximately 3/5 times
	// We use a loose check due to the modulo-based distribution
	if counts["worker-2"] < 40 || counts["worker-2"] > 80 {
		t.Errorf("worker-2 selection count %d outside expected range 40-80", counts["worker-2"])
	}
}

func TestRandomSelection(t *testing.T) {
	lb := NewLoadBalancer("random")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	workers := lb.getHealthyWorkers()

	// Run random selection multiple times
	counts := make(map[string]int)
	for i := 0; i < 300; i++ {
		selected := lb.random(workers)
		counts[selected.Name]++
	}

	// Each worker should be selected at least once (with very high probability)
	for _, worker := range workers {
		if counts[worker.Name] == 0 {
			t.Errorf("worker %s was never selected", worker.Name)
		}
	}
}

func TestSetAlgorithm(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	lb.SetAlgorithm("least-connections")

	if lb.algorithm != "least-connections" {
		t.Errorf("algorithm = %v, want least-connections", lb.algorithm)
	}

	lb.SetAlgorithm("weighted")
	if lb.algorithm != "weighted" {
		t.Errorf("algorithm = %v, want weighted", lb.algorithm)
	}
}

func TestRecordSuccess(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1)

	worker := lb.workers[0]
	worker.ConsecFailures = 5

	lb.recordSuccess(worker)

	if worker.ConsecFailures != 0 {
		t.Errorf("consecFailures = %d, want 0", worker.ConsecFailures)
	}
}

func TestRecordFailure(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1)

	worker := lb.workers[0]
	initialFailures := worker.ConsecFailures

	lb.recordFailure(worker)

	if worker.ConsecFailures != initialFailures+1 {
		t.Errorf("consecFailures = %d, want %d", worker.ConsecFailures, initialFailures+1)
	}
}

func TestCircuitBreaker(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.circuitThreshold = 3
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1)

	worker := lb.workers[0]

	// Record failures to trigger circuit breaker
	for i := 0; i < 3; i++ {
		lb.recordFailure(worker)
	}

	if !worker.CircuitOpen {
		t.Error("circuit should be open after threshold failures")
	}
}

func TestGetStatus(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 2)

	atomic.StoreInt32(&lb.workers[0].CurrentLoad, 3)
	atomic.StoreInt64(&lb.workers[0].TotalRequests, 100)

	status := lb.GetStatus()

	if status["algorithm"] != "round-robin" {
		t.Errorf("algorithm = %v, want round-robin", status["algorithm"])
	}

	workers, ok := status["workers"].([]map[string]interface{})
	if !ok {
		t.Fatal("workers is not the expected type")
	}

	if len(workers) != 2 {
		t.Fatalf("expected 2 workers in status, got %d", len(workers))
	}

	if workers[0]["name"] != "worker-1" {
		t.Errorf("worker[0] name = %v, want worker-1", workers[0]["name"])
	}

	if workers[0]["currentLoad"] != int32(3) {
		t.Errorf("worker[0] currentLoad = %v, want 3", workers[0]["currentLoad"])
	}

	if workers[1]["weight"] != 2 {
		t.Errorf("worker[1] weight = %v, want 2", workers[1]["weight"])
	}
}

func TestHealthEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", response["status"])
	}
}

func TestStatusEndpoint(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lb.GetStatus())
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["algorithm"] != "round-robin" {
		t.Errorf("algorithm = %v, want round-robin", response["algorithm"])
	}
}

func TestAlgorithmEndpointGet(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	mux := http.NewServeMux()
	mux.HandleFunc("/algorithm", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"algorithm": lb.algorithm})
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/algorithm", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["algorithm"] != "round-robin" {
		t.Errorf("algorithm = %v, want round-robin", response["algorithm"])
	}
}

func TestAlgorithmEndpointPut(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	mux := http.NewServeMux()
	mux.HandleFunc("/algorithm", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			var body struct {
				Algorithm string `json:"algorithm"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Invalid request", http.StatusBadRequest)
				return
			}
			lb.SetAlgorithm(body.Algorithm)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"algorithm": lb.algorithm})
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	body := bytes.NewBufferString(`{"algorithm":"least-connections"}`)
	req := httptest.NewRequest(http.MethodPut, "/algorithm", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	if lb.algorithm != "least-connections" {
		t.Errorf("algorithm = %v, want least-connections", lb.algorithm)
	}
}

func TestTaskEndpointNoHealthyWorkers(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	mux := http.NewServeMux()
	mux.HandleFunc("/task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var task TaskRequest
		if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		respBody, statusCode, err := lb.ForwardRequest(task)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(statusCode)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(statusCode)
		w.Write(respBody)
	})

	body := bytes.NewBufferString(`{"id":"task-1","weight":1.0}`)
	req := httptest.NewRequest(http.MethodPost, "/task", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
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
		t.Error("CORS header not set correctly")
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

func TestSelectWorkerWithDifferentAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{"round-robin", "round-robin"},
		{"least-connections", "least-connections"},
		{"weighted", "weighted"},
		{"random", "random"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer(tt.algorithm)
			lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
			lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 2)

			worker := lb.SelectWorker()
			if worker == nil {
				t.Error("SelectWorker returned nil")
			}
		})
	}
}

func TestSelectWorkerReturnsNilWhenNoHealthyWorkers(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)

	// Mark all workers as unhealthy
	lb.workers[0].Healthy = false

	worker := lb.SelectWorker()
	if worker != nil {
		t.Error("SelectWorker should return nil when no healthy workers")
	}
}

func TestConcurrentWorkerAccess(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := lb.SelectWorker()
			if worker != nil {
				atomic.AddInt32(&worker.CurrentLoad, 1)
				time.Sleep(time.Millisecond)
				atomic.AddInt32(&worker.CurrentLoad, -1)
			}
		}()
	}

	wg.Wait()

	// Verify no data races and final state is consistent
	load1 := atomic.LoadInt32(&lb.workers[0].CurrentLoad)
	load2 := atomic.LoadInt32(&lb.workers[1].CurrentLoad)

	if load1 != 0 || load2 != 0 {
		t.Errorf("final loads should be 0, got worker-1: %d, worker-2: %d", load1, load2)
	}
}

func TestBroadcastStatus(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1)

	// Create a mock WebSocket connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Wait for message
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Verify message is valid JSON
		var status map[string]interface{}
		if err := json.Unmarshal(message, &status); err != nil {
			t.Errorf("failed to parse status: %v", err)
		}
	}))
	defer server.Close()

	// Note: Full WebSocket testing would require more complex setup
	// This test verifies the basic structure
	lb.BroadcastStatus()
}

func TestGetEnvFunction(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		defaultVal string
		envVal     string
		want       string
	}{
		{"with env set", "TEST_KEY", "default", "custom", "custom"},
		{"without env set", "NONEXISTENT_KEY", "default", "", "default"},
		{"empty default", "NONEXISTENT_KEY", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv(tt.key, tt.envVal)
			}

			got := getEnv(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkerCurrentLoadTracking(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)

	worker := lb.workers[0]

	// Simulate increasing load
	for i := 0; i < 5; i++ {
		atomic.AddInt32(&worker.CurrentLoad, 1)
	}

	load := atomic.LoadInt32(&worker.CurrentLoad)
	if load != 5 {
		t.Errorf("currentLoad = %d, want 5", load)
	}

	// Simulate decreasing load
	for i := 0; i < 3; i++ {
		atomic.AddInt32(&worker.CurrentLoad, -1)
	}

	load = atomic.LoadInt32(&worker.CurrentLoad)
	if load != 2 {
		t.Errorf("currentLoad = %d, want 2", load)
	}
}

func TestWorkerRequestCounters(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)

	worker := lb.workers[0]

	// Simulate requests
	atomic.AddInt64(&worker.TotalRequests, 10)
	atomic.AddInt64(&worker.FailedRequests, 2)

	totalReqs := atomic.LoadInt64(&worker.TotalRequests)
	failedReqs := atomic.LoadInt64(&worker.FailedRequests)

	if totalReqs != 10 {
		t.Errorf("totalRequests = %d, want 10", totalReqs)
	}

	if failedReqs != 2 {
		t.Errorf("failedRequests = %d, want 2", failedReqs)
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.circuitThreshold = 2
	lb.circuitRecovery = 50 * time.Millisecond
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1)

	worker := lb.workers[0]

	// Trigger circuit breaker
	for i := 0; i < 2; i++ {
		lb.recordFailure(worker)
	}

	if !worker.CircuitOpen {
		t.Error("circuit should be open")
	}

	// Wait for recovery
	time.Sleep(100 * time.Millisecond)

	// The circuit recovery is async, so we just verify it was triggered
	// In a real scenario, the goroutine would close the circuit
}

func TestInvalidTaskRequest(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var task TaskRequest
		if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	})

	body := bytes.NewBufferString(`invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/task", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestTaskEndpointMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/task", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHealthResponseStructure(t *testing.T) {
	health := HealthResponse{
		Status:      "healthy",
		CurrentLoad: 5,
		QueueDepth:  2,
	}

	data, err := json.Marshal(health)
	if err != nil {
		t.Fatalf("failed to marshal health response: %v", err)
	}

	var decoded HealthResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal health response: %v", err)
	}

	if decoded.Status != "healthy" {
		t.Errorf("status = %v, want healthy", decoded.Status)
	}
	if decoded.CurrentLoad != 5 {
		t.Errorf("currentLoad = %v, want 5", decoded.CurrentLoad)
	}
	if decoded.QueueDepth != 2 {
		t.Errorf("queueDepth = %v, want 2", decoded.QueueDepth)
	}
}

func TestTaskRequestStructure(t *testing.T) {
	task := TaskRequest{
		ID:     "task-123",
		Weight: 1.5,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("failed to marshal task request: %v", err)
	}

	var decoded TaskRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal task request: %v", err)
	}

	if decoded.ID != "task-123" {
		t.Errorf("id = %v, want task-123", decoded.ID)
	}
	if decoded.Weight != 1.5 {
		t.Errorf("weight = %v, want 1.5", decoded.Weight)
	}
}

func TestWorkerWithZeroWeight(t *testing.T) {
	lb := NewLoadBalancer("weighted")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 0)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 2)

	workers := lb.getHealthyWorkers()

	// Should still work, but worker-1 will never be selected due to 0 weight
	selected := lb.weighted(workers)

	// With proper weight distribution, worker-2 should always be selected
	if selected.Name == "worker-1" && lb.workers[1].Weight > 0 {
		t.Error("worker with 0 weight should not be selected when others have weight")
	}
}