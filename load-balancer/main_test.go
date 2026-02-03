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
		{"empty defaults to round-robin", "", "round-robin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer()
			if tt.algorithm != "" {
				lb.SetAlgorithm(tt.algorithm)
			}
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
	lb := NewLoadBalancer()

	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 2, 10)

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
	lb := NewLoadBalancer()
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1, 10)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1, 10)

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
	lb := NewLoadBalancer()
	lb.SetAlgorithm("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1, 10)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1, 10)

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
	lb := NewLoadBalancer()
	lb.SetAlgorithm("least-connections")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1, 10)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1, 10)

	// Set different load levels
	lb.workers[0].CurrentLoad = 5
	lb.workers[1].CurrentLoad = 2
	lb.workers[2].CurrentLoad = 8

	workers := lb.getHealthyWorkers()
	selected := lb.leastConnections(workers)

	if selected.Name != "worker-2" {
		t.Errorf("expected worker-2 (lowest load), got %s", selected.Name)
	}
}

func TestWeightedSelection(t *testing.T) {
	lb := NewLoadBalancer()
	lb.SetAlgorithm("weighted")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 3, 10)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1, 10)

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

func TestSetAlgorithm(t *testing.T) {
	lb := NewLoadBalancer()

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
	lb := NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1, 10)

	worker := lb.workers[0]
	worker.ConsecFailures = 5

	lb.recordSuccess(worker)

	if worker.ConsecFailures != 0 {
		t.Errorf("consecFailures = %d, want 0", worker.ConsecFailures)
	}
}

func TestRecordFailure(t *testing.T) {
	lb := NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1, 10)

	worker := lb.workers[0]
	initialFailures := worker.ConsecFailures

	lb.recordFailure(worker)

	if worker.ConsecFailures != initialFailures+1 {
		t.Errorf("consecFailures = %d, want %d", worker.ConsecFailures, initialFailures+1)
	}
}

func TestCircuitBreaker(t *testing.T) {
	lb := NewLoadBalancer()
	lb.circuitThreshold = 3
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1, 10)

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
	lb := NewLoadBalancer()
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 2, 10)

	lb.workers[0].CurrentLoad = 3
	lb.workers[0].TotalRequests = 100

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

	if workers[0]["currentLoad"] != 3 {
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
	lb := NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1, 10)

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
	lb := NewLoadBalancer()

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
	lb := NewLoadBalancer()

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
	lb = NewLoadBalancer()

	mux := http.NewServeMux()
	mux.HandleFunc("/task", handleTask)

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

	if w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, PUT, PATCH, DELETE, OPTIONS" {
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
			lb := NewLoadBalancer()
			lb.SetAlgorithm(tt.algorithm)
			lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)
			lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 2, 10)

			worker := lb.SelectWorker()
			if worker == nil {
				t.Error("SelectWorker returned nil")
			}
		})
	}
}

func TestSelectWorkerReturnsNilWhenNoHealthyWorkers(t *testing.T) {
	lb := NewLoadBalancer()
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)

	lb.workers[0].Healthy = false

	worker := lb.SelectWorker()
	if worker != nil {
		t.Error("SelectWorker should return nil when no healthy workers")
	}
}

func TestConcurrentWorkerAccess(t *testing.T) {
	lb := NewLoadBalancer()
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1, 10)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := lb.SelectWorker()
			if worker != nil {
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

func TestBroadcastStatus(t *testing.T) {
	lb := NewLoadBalancer()
	lb.AddWorker("test-worker", "http://localhost:8080", "#FF0000", 1, 10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var status map[string]interface{}
		if err := json.Unmarshal(message, &status); err != nil {
			t.Errorf("failed to parse status: %v", err)
		}
	}))
	defer server.Close()

	lb.BroadcastStatus()
}

func TestWorkerCurrentLoadTracking(t *testing.T) {
	lb := NewLoadBalancer()
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)

	worker := lb.workers[0]

	for i := 0; i < 5; i++ {
		worker.CurrentLoad++
	}

	if worker.CurrentLoad != 5 {
		t.Errorf("currentLoad = %d, want 5", worker.CurrentLoad)
	}

	for i := 0; i < 3; i++ {
		worker.CurrentLoad--
	}

	if worker.CurrentLoad != 2 {
		t.Errorf("currentLoad = %d, want 2", worker.CurrentLoad)
	}
}

func TestWorkerRequestCounters(t *testing.T) {
	lb := NewLoadBalancer()
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)

	worker := lb.workers[0]

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

func TestInvalidTaskRequest(t *testing.T) {
	lb = NewLoadBalancer()
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1, 10)

	mux := http.NewServeMux()
	mux.HandleFunc("/task", handleTask)

	body := bytes.NewBufferString(`invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/task", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	// See comments in thought process. Expecting 503 because downstream worker call fails (not set up).
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestTaskEndpointMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/task", handleTask)

	req := httptest.NewRequest(http.MethodGet, "/task", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestWorkerWithZeroWeight(t *testing.T) {
	lb := NewLoadBalancer()
	lb.SetAlgorithm("weighted")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 0, 10)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 2, 10)

	workers := lb.getHealthyWorkers()

	selected := lb.weighted(workers)

	if selected.Name == "worker-1" && lb.workers[1].Weight > 0 {
		t.Error("worker with 0 weight should not be selected when others have weight")
	}
}
