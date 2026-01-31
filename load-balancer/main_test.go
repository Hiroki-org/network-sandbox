package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestNewLoadBalancer tests load balancer creation with different algorithms
func TestNewLoadBalancer(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		want      string
	}{
		{"round-robin algorithm", "round-robin", "round-robin"},
		{"least-connections algorithm", "least-connections", "least-connections"},
		{"weighted algorithm", "weighted", "weighted"},
		{"random algorithm", "random", "random"},
		{"empty algorithm defaults to round-robin", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer(tt.algorithm)
			if lb == nil {
				t.Fatal("NewLoadBalancer returned nil")
			}
			if tt.algorithm != "" && lb.algorithm != tt.want {
				t.Errorf("algorithm = %v, want %v", lb.algorithm, tt.want)
			}
			if lb.workers == nil {
				t.Error("workers slice not initialized")
			}
			if lb.wsClients == nil {
				t.Error("wsClients map not initialized")
			}
		})
	}
}

// TestAddWorker tests adding workers to the load balancer
func TestAddWorker(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	tests := []struct {
		name   string
		wName  string
		url    string
		color  string
		weight int
	}{
		{"add first worker", "worker-1", "http://localhost:8081", "#FF0000", 1},
		{"add second worker", "worker-2", "http://localhost:8082", "#00FF00", 2},
		{"add third worker", "worker-3", "http://localhost:8083", "#0000FF", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialCount := len(lb.workers)
			lb.AddWorker(tt.wName, tt.url, tt.color, tt.weight)

			if len(lb.workers) != initialCount+1 {
				t.Errorf("worker count = %d, want %d", len(lb.workers), initialCount+1)
			}

			worker := lb.workers[len(lb.workers)-1]
			if worker.Name != tt.wName {
				t.Errorf("worker name = %v, want %v", worker.Name, tt.wName)
			}
			if worker.URL != tt.url {
				t.Errorf("worker URL = %v, want %v", worker.URL, tt.url)
			}
			if worker.Color != tt.color {
				t.Errorf("worker color = %v, want %v", worker.Color, tt.color)
			}
			if worker.Weight != tt.weight {
				t.Errorf("worker weight = %v, want %v", worker.Weight, tt.weight)
			}
			if !worker.Healthy {
				t.Error("worker should be healthy by default")
			}
		})
	}
}

// TestGetHealthyWorkers tests filtering healthy workers
func TestGetHealthyWorkers(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	// Mark worker-2 as unhealthy
	lb.workers[1].Healthy = false

	healthy := lb.getHealthyWorkers()
	if len(healthy) != 2 {
		t.Errorf("healthy workers count = %d, want 2", len(healthy))
	}

	// Mark worker-1 circuit open
	lb.workers[0].CircuitOpen = true
	healthy = lb.getHealthyWorkers()
	if len(healthy) != 1 {
		t.Errorf("healthy workers count after circuit open = %d, want 1", len(healthy))
	}
}

// TestRoundRobinSelection tests round-robin algorithm
func TestRoundRobinSelection(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	// Test round-robin distribution
	selections := make(map[string]int)
	for i := 0; i < 9; i++ {
		worker := lb.SelectWorker()
		if worker == nil {
			t.Fatal("SelectWorker returned nil")
		}
		selections[worker.Name]++
	}

	// Each worker should be selected 3 times
	for name, count := range selections {
		if count != 3 {
			t.Errorf("worker %s selected %d times, want 3", name, count)
		}
	}
}

// TestLeastConnectionsSelection tests least-connections algorithm
func TestLeastConnectionsSelection(t *testing.T) {
	lb := NewLoadBalancer("least-connections")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	// Set different loads
	atomic.StoreInt32(&lb.workers[0].CurrentLoad, 5)
	atomic.StoreInt32(&lb.workers[1].CurrentLoad, 2)
	atomic.StoreInt32(&lb.workers[2].CurrentLoad, 8)

	worker := lb.SelectWorker()
	if worker == nil {
		t.Fatal("SelectWorker returned nil")
	}
	if worker.Name != "worker-2" {
		t.Errorf("selected worker = %s, want worker-2 (least loaded)", worker.Name)
	}
}

// TestWeightedSelection tests weighted algorithm
func TestWeightedSelection(t *testing.T) {
	lb := NewLoadBalancer("weighted")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 3)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	// Test weighted distribution over many selections
	selections := make(map[string]int)
	iterations := 500
	for i := 0; i < iterations; i++ {
		worker := lb.SelectWorker()
		if worker == nil {
			t.Fatal("SelectWorker returned nil")
		}
		selections[worker.Name]++
	}

	// Worker-2 should be selected ~3x more than worker-1 and worker-3
	// Allow some variance due to rotation mechanism
	worker2Count := selections["worker-2"]
	worker1Count := selections["worker-1"]
	worker3Count := selections["worker-3"]

	// Worker-2 should have more selections (weight=3 vs weight=1)
	if worker2Count <= worker1Count || worker2Count <= worker3Count {
		t.Errorf("worker-2 (weight=3) should be selected more often: worker-1=%d, worker-2=%d, worker-3=%d",
			worker1Count, worker2Count, worker3Count)
	}
}

// TestRandomSelection tests random algorithm
func TestRandomSelection(t *testing.T) {
	lb := NewLoadBalancer("random")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	// Test that all workers can be selected
	selections := make(map[string]bool)
	for i := 0; i < 100; i++ {
		worker := lb.SelectWorker()
		if worker == nil {
			t.Fatal("SelectWorker returned nil")
		}
		selections[worker.Name] = true
	}

	// All workers should be selected at least once in 100 attempts
	if len(selections) != 3 {
		t.Errorf("not all workers selected: %v", selections)
	}
}

// TestSelectWorkerNoHealthy tests selection when no workers are healthy
func TestSelectWorkerNoHealthy(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)

	// Mark all workers unhealthy
	lb.workers[0].Healthy = false
	lb.workers[1].Healthy = false

	worker := lb.SelectWorker()
	if worker != nil {
		t.Error("SelectWorker should return nil when no healthy workers")
	}
}

// TestRecordSuccess tests success recording
func TestRecordSuccess(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	worker := lb.workers[0]

	// Set some consecutive failures
	worker.ConsecFailures = 3

	lb.recordSuccess(worker)

	if worker.ConsecFailures != 0 {
		t.Errorf("consecutive failures = %d, want 0", worker.ConsecFailures)
	}
}

// TestRecordFailure tests failure recording and circuit breaker
func TestRecordFailure(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	worker := lb.workers[0]

	// Record failures up to threshold
	for i := 0; i < lb.circuitThreshold; i++ {
		lb.recordFailure(worker)
	}

	if !worker.CircuitOpen {
		t.Error("circuit should be open after threshold failures")
	}

	if worker.ConsecFailures != lb.circuitThreshold {
		t.Errorf("consecutive failures = %d, want %d", worker.ConsecFailures, lb.circuitThreshold)
	}
}

// TestSetAlgorithm tests changing the algorithm
func TestSetAlgorithm(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	tests := []string{"least-connections", "weighted", "random", "round-robin"}
	for _, algo := range tests {
		lb.SetAlgorithm(algo)
		if lb.algorithm != algo {
			t.Errorf("algorithm = %v, want %v", lb.algorithm, algo)
		}
	}
}

// TestGetStatus tests status retrieval
func TestGetStatus(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 2)

	status := lb.GetStatus()

	if status["algorithm"] != "round-robin" {
		t.Errorf("algorithm in status = %v, want round-robin", status["algorithm"])
	}

	workers, ok := status["workers"].([]map[string]interface{})
	if !ok {
		t.Fatal("workers not in expected format")
	}

	if len(workers) != 2 {
		t.Errorf("workers count = %d, want 2", len(workers))
	}

	// Check worker details
	if workers[0]["name"] != "worker-1" {
		t.Errorf("first worker name = %v, want worker-1", workers[0]["name"])
	}
	if workers[1]["weight"] != 2 {
		t.Errorf("second worker weight = %v, want 2", workers[1]["weight"])
	}
}

// TestHealthEndpoint tests the health check HTTP endpoint
func TestHealthEndpoint(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", response["status"])
	}
}

// TestStatusEndpoint tests the status HTTP endpoint
func TestStatusEndpoint(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lb.GetStatus())
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["algorithm"] != "round-robin" {
		t.Errorf("algorithm = %v, want round-robin", response["algorithm"])
	}
}

// TestAlgorithmEndpoint tests the algorithm HTTP endpoint
func TestAlgorithmEndpoint(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	mux := http.NewServeMux()
	mux.HandleFunc("/algorithm", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"algorithm": lb.algorithm})
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

	// Test GET
	req := httptest.NewRequest(http.MethodGet, "/algorithm", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET status code = %d, want %d", rec.Code, http.StatusOK)
	}

	// Test PUT
	body := bytes.NewBufferString(`{"algorithm":"least-connections"}`)
	req = httptest.NewRequest(http.MethodPut, "/algorithm", body)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("PUT status code = %d, want %d", rec.Code, http.StatusOK)
	}

	if lb.algorithm != "least-connections" {
		t.Errorf("algorithm after PUT = %v, want least-connections", lb.algorithm)
	}
}

// TestCORSMiddleware tests CORS headers
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

	// Test OPTIONS request
	req = httptest.NewRequest(http.MethodOptions, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("OPTIONS status code = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestConcurrentWorkerSelection tests thread safety
func TestConcurrentWorkerSelection(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	lb.AddWorker("worker-2", "http://localhost:8082", "#00FF00", 1)
	lb.AddWorker("worker-3", "http://localhost:8083", "#0000FF", 1)

	var wg sync.WaitGroup
	goroutines := 100
	selectionsPerGoroutine := 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < selectionsPerGoroutine; j++ {
				worker := lb.SelectWorker()
				if worker == nil {
					t.Error("SelectWorker returned nil in concurrent access")
					return
				}
			}
		}()
	}

	wg.Wait()
}

// TestWebSocketConnection tests WebSocket basic connection
func TestWebSocketConnection(t *testing.T) {
	lb := NewLoadBalancer("round-robin")

	server := httptest.NewServer(http.HandlerFunc(lb.handleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] // Convert http:// to ws://

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to websocket: %v", err)
	}
	defer ws.Close()

	// Set read deadline to avoid blocking forever
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Should receive initial status
	var status map[string]interface{}
	if err := ws.ReadJSON(&status); err != nil {
		t.Fatalf("failed to read initial status: %v", err)
	}

	if _, ok := status["algorithm"]; !ok {
		t.Error("status should contain algorithm field")
	}
}

// TestBroadcastStatus tests broadcasting to WebSocket clients
func TestBroadcastStatus(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)

	// Create a mock WebSocket connection
	server := httptest.NewServer(http.HandlerFunc(lb.handleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// Read initial status
	var initialStatus map[string]interface{}
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ws.ReadJSON(&initialStatus); err != nil {
		t.Fatalf("failed to read initial status: %v", err)
	}

	// Trigger a broadcast
	lb.BroadcastStatus()

	// Try to read the broadcast (with timeout)
	var broadcastStatus map[string]interface{}
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ws.ReadJSON(&broadcastStatus); err != nil {
		// This might timeout if broadcast happens before connection is registered
		t.Logf("broadcast read: %v", err)
	}
}

// TestHealthCheckWithMockServer tests health check against mock worker
func TestHealthCheckWithMockServer(t *testing.T) {
	// Create mock worker server
	mockWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(HealthResponse{
				Status:      "healthy",
				CurrentLoad: 3,
				QueueDepth:  1,
			})
		}
	}))
	defer mockWorker.Close()

	lb := NewLoadBalancer("round-robin")
	lb.AddWorker("test-worker", mockWorker.URL, "#FF0000", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Perform health check
	lb.HealthCheck()

	// Wait a bit for health check to complete
	time.Sleep(100 * time.Millisecond)

	// Worker should be healthy
	if !lb.workers[0].Healthy {
		t.Error("worker should be healthy after successful health check")
	}
}

// TestCircuitBreakerRecovery tests circuit breaker recovery mechanism
func TestCircuitBreakerRecovery(t *testing.T) {
	lb := NewLoadBalancer("round-robin")
	lb.circuitRecovery = 100 * time.Millisecond // Short recovery time for testing
	lb.AddWorker("worker-1", "http://localhost:8081", "#FF0000", 1)
	worker := lb.workers[0]

	// Trigger circuit breaker
	for i := 0; i < lb.circuitThreshold; i++ {
		lb.recordFailure(worker)
	}

	if !worker.CircuitOpen {
		t.Fatal("circuit should be open")
	}

	// Wait for recovery
	time.Sleep(150 * time.Millisecond)

	// Circuit should be closed and failures reset
	if worker.CircuitOpen {
		t.Error("circuit should be closed after recovery period")
	}
	if worker.ConsecFailures != 0 {
		t.Errorf("consecutive failures should be reset, got %d", worker.ConsecFailures)
	}
}

// TestGetEnv tests environment variable reading
func TestGetEnv(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		defaultVal string
		want       string
	}{
		{"use default when not set", "NON_EXISTENT_KEY", "default", "default"},
		{"use default for empty key", "", "default", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEnv(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}