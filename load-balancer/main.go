package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Worker represents a backend worker
type Worker struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Color          string `json:"color"`
	Weight         int    `json:"weight"`
	MaxLoad        int    `json:"maxLoad"`
	Healthy        bool   `json:"healthy"`
	CurrentLoad    int64  `json:"currentLoad"`
	Enabled        bool   `json:"enabled"`
	TotalRequests  int64  `json:"totalRequests"`
	FailedRequests int64  `json:"failedRequests"`
	CircuitOpen    bool   `json:"circuitOpen"`
	ConsecFailures int64  `json:"consecFailures"`
}

// LoadBalancer manages workers and distribution
type LoadBalancer struct {
	mu            sync.RWMutex
	workers       []*Worker
	algorithm     string
	roundRobinIdx uint64
	wsClients     map[*websocket.Conn]bool
	wsClientsMu   sync.Mutex
	// Circuit breaker configuration
	circuitThreshold int
}

// Prometheus metrics
var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lb_requests_total",
			Help: "Total requests processed by worker",
		},
		[]string{"worker", "status"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "lb_request_duration_ms",
			Help:    "Request duration in milliseconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		},
		[]string{"worker"},
	)
	workerHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lb_worker_health",
			Help: "Worker health status (1=healthy, 0=unhealthy)",
		},
		[]string{"worker"},
	)
	workerActiveConnections = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lb_worker_active_connections",
			Help: "Active connections per worker",
		},
		[]string{"worker"},
	)
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
		if allowedOrigins == "" {
			// Development mode: allow all origins
			return true
		}
		origin := r.Header.Get("Origin")
		for _, allowed := range strings.Split(allowedOrigins, ",") {
			if strings.TrimSpace(allowed) == origin {
				return true
			}
		}
		log.Printf("WebSocket connection rejected from origin: %s", origin)
		return false
	},
}

func init() {
	prometheus.MustRegister(requestsTotal, requestDuration, workerHealth, workerActiveConnections)
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer() *LoadBalancer {
	return &LoadBalancer{
		workers:          make([]*Worker, 0),
		algorithm:        "round-robin",
		wsClients:        make(map[*websocket.Conn]bool),
		circuitThreshold: 3,
	}
}

// AddWorker adds a worker to the pool
func (lb *LoadBalancer) AddWorker(name, url, color string, weight, maxLoad int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.workers = append(lb.workers, &Worker{
		Name:    name,
		URL:     url,
		Color:   color,
		Weight:  weight,
		MaxLoad: maxLoad,
		Healthy: true,
		Enabled: true,
	})
}

// SelectWorker selects a worker based on the current algorithm
func (lb *LoadBalancer) SelectWorker() *Worker {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if len(lb.workers) == 0 {
		return nil
	}

	switch lb.algorithm {
	case "least-connections":
		return lb.leastConnections()
	case "weighted":
		return lb.weighted()
	case "random":
		return lb.random()
	default:
		return lb.roundRobin()
	}
}

func (lb *LoadBalancer) roundRobin() *Worker {
	startIdx := atomic.AddUint64(&lb.roundRobinIdx, 1)
	n := uint64(len(lb.workers))

	for i := uint64(0); i < n; i++ {
		idx := (startIdx + i) % n
		w := lb.workers[idx]
		if w.Healthy && w.Enabled && !w.CircuitOpen {
			return w
		}
	}
	return nil
}

func (lb *LoadBalancer) leastConnections() *Worker {
	var selected *Worker
	minLoad := int64(1<<63 - 1) // MaxInt64

	for _, w := range lb.workers {
		if !w.Healthy || !w.Enabled || w.CircuitOpen {
			continue
		}
		load := atomic.LoadInt64(&w.CurrentLoad)
		if load < minLoad {
			minLoad = load
			selected = w
		}
	}
	return selected
}

func (lb *LoadBalancer) weighted() *Worker {
	totalWeight := 0

	// First pass: calculate total weight
	for _, w := range lb.workers {
		if w.Healthy && w.Enabled && !w.CircuitOpen {
			totalWeight += w.Weight
		}
	}

	if totalWeight == 0 {
		// Fallback to first healthy one
		for _, w := range lb.workers {
			if w.Healthy && w.Enabled && !w.CircuitOpen {
				return w
			}
		}
		return nil
	}

	r := rand.Intn(totalWeight)
	for _, w := range lb.workers {
		if w.Healthy && w.Enabled && !w.CircuitOpen {
			r -= w.Weight
			if r < 0 {
				return w
			}
		}
	}

	return nil
}

func (lb *LoadBalancer) random() *Worker {
	count := 0
	for _, w := range lb.workers {
		if w.Healthy && w.Enabled && !w.CircuitOpen {
			count++
		}
	}

	if count == 0 {
		return nil
	}

	r := rand.Intn(count)
	current := 0
	for _, w := range lb.workers {
		if w.Healthy && w.Enabled && !w.CircuitOpen {
			if current == r {
				return w
			}
			current++
		}
	}
	return nil
}

// SetAlgorithm changes the load balancing algorithm
func (lb *LoadBalancer) SetAlgorithm(algo string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.algorithm = algo
}

// GetStatus returns the current status
func (lb *LoadBalancer) GetStatus() map[string]interface{} {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	workers := make([]map[string]interface{}, len(lb.workers))
	for i, w := range lb.workers {
		workers[i] = map[string]interface{}{
			"name":           w.Name,
			"url":            w.URL,
			"color":          w.Color,
			"weight":         w.Weight,
			"maxLoad":        w.MaxLoad,
			"healthy":        w.Healthy,
			"currentLoad":    atomic.LoadInt64(&w.CurrentLoad),
			"enabled":        w.Enabled,
			"totalRequests":  atomic.LoadInt64(&w.TotalRequests),
			"failedRequests": atomic.LoadInt64(&w.FailedRequests),
			"circuitOpen":    w.CircuitOpen,
		}
	}
	return map[string]interface{}{
		"algorithm": lb.algorithm,
		"workers":   workers,
	}
}

// HealthCheck runs periodic health checks on workers
func (lb *LoadBalancer) HealthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lb.checkAllWorkers()
		}
	}
}

func (lb *LoadBalancer) checkAllWorkers() {
	lb.mu.RLock()
	workers := make([]*Worker, len(lb.workers))
	copy(workers, lb.workers)
	lb.mu.RUnlock()

	for _, w := range workers {
		go lb.checkWorker(w)
	}
}

func (lb *LoadBalancer) checkWorker(w *Worker) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(w.URL + "/health")

	isHealthy := err == nil && resp != nil && resp.StatusCode == http.StatusOK
	if resp != nil {
		resp.Body.Close()
	}

	if !isHealthy {
		failures := atomic.AddInt64(&w.ConsecFailures, 1)
		if failures >= int64(lb.circuitThreshold) {
			lb.mu.Lock()
			w.CircuitOpen = true
			w.Healthy = false
			lb.mu.Unlock()
		}
	} else {
		atomic.StoreInt64(&w.ConsecFailures, 0)

		lb.mu.RLock()
		needsUpdate := !w.Healthy || w.CircuitOpen
		lb.mu.RUnlock()

		if needsUpdate {
			lb.mu.Lock()
			w.Healthy = true
			w.CircuitOpen = false
			lb.mu.Unlock()
		}
	}

	healthVal := 0.0
	if isHealthy {
		healthVal = 1.0
	}
	workerHealth.WithLabelValues(w.Name).Set(healthVal)
	workerActiveConnections.WithLabelValues(w.Name).Set(float64(atomic.LoadInt64(&w.CurrentLoad)))
}

// UpdateWorker updates worker settings
func (lb *LoadBalancer) UpdateWorker(name string, enabled *bool, weight *int) bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for _, w := range lb.workers {
		if w.Name == name {
			if enabled != nil {
				w.Enabled = *enabled
			}
			if weight != nil && *weight > 0 {
				w.Weight = *weight
			}
			return true
		}
	}
	return false
}

// BroadcastStatus sends status to all WebSocket clients
func (lb *LoadBalancer) BroadcastStatus() {
	lb.wsClientsMu.Lock()
	defer lb.wsClientsMu.Unlock()

	status := lb.GetStatus()

	data, err := json.Marshal(status)
	if err != nil {
		log.Printf("Failed to marshal status for broadcast: %v", err)
		return
	}
	for client := range lb.wsClients {
		if err := client.WriteMessage(websocket.TextMessage, data); err != nil {
			client.Close()
			delete(lb.wsClients, client)
		}
	}
}

// StartBroadcast starts periodic status broadcasts
func (lb *LoadBalancer) StartBroadcast(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lb.BroadcastStatus()
		}
	}
}

var lb *LoadBalancer

func handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	worker := lb.SelectWorker()
	if worker == nil {
		requestsTotal.WithLabelValues("none", "error").Inc()
		http.Error(w, `{"error": "No healthy workers available"}`, http.StatusServiceUnavailable)
		return
	}

	var taskReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&taskReq); err != nil {
		taskReq = map[string]interface{}{"weight": 1.0}
	}

	atomic.AddInt64(&worker.CurrentLoad, 1)
	atomic.AddInt64(&worker.TotalRequests, 1)

	start := time.Now()

	client := &http.Client{Timeout: 30 * time.Second}
	body, _ := json.Marshal(taskReq)
	resp, err := client.Post(worker.URL+"/task", "application/json", strings.NewReader(string(body)))

	duration := float64(time.Since(start).Milliseconds())
	requestDuration.WithLabelValues(worker.Name).Observe(duration)

	atomic.AddInt64(&worker.CurrentLoad, -1)

	if err != nil || resp.StatusCode >= 500 {
		atomic.AddInt64(&worker.FailedRequests, 1)
		failures := atomic.AddInt64(&worker.ConsecFailures, 1)

		if failures >= int64(lb.circuitThreshold) {
			lb.mu.Lock()
			worker.CircuitOpen = true
			lb.mu.Unlock()
		}

		requestsTotal.WithLabelValues(worker.Name, "error").Inc()
		http.Error(w, `{"error": "Worker failed"}`, http.StatusServiceUnavailable)
		return
	}

	atomic.StoreInt64(&worker.ConsecFailures, 0)

	requestsTotal.WithLabelValues(worker.Name, "success").Inc()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		result = map[string]interface{}{}
	}
	resp.Body.Close()

	result["worker"] = worker.Name
	result["workerColor"] = worker.Color
	result["processingTimeMs"] = int(duration)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)

	lb.BroadcastStatus()
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lb.GetStatus())
}

var availableAlgorithms = []string{"round-robin", "least-connections", "weighted", "random"}

var validAlgorithms = func() map[string]struct{} {
	m := make(map[string]struct{}, len(availableAlgorithms))
	for _, a := range availableAlgorithms {
		m[a] = struct{}{}
	}
	return m
}()

func handleAlgorithm(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		lb.mu.RLock()
		algo := lb.algorithm
		lb.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"algorithm": algo,
			"available": availableAlgorithms,
		})

	case http.MethodPut, http.MethodPost:
		var req struct {
			Algorithm string `json:"algorithm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if _, ok := validAlgorithms[req.Algorithm]; !ok {
			http.Error(w, "Invalid algorithm", http.StatusBadRequest)
			return
		}
		lb.SetAlgorithm(req.Algorithm)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"algorithm": req.Algorithm,
			"available": availableAlgorithms,
		})
		lb.BroadcastStatus()

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/workers/")
	name := strings.TrimSuffix(path, "/")
	if name == "" {
		http.Error(w, "Worker name required", http.StatusBadRequest)
		return
	}

	var req struct {
		Enabled *bool `json:"enabled,omitempty"`
		Weight  *int  `json:"weight,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if !lb.UpdateWorker(name, req.Enabled, req.Weight) {
		http.Error(w, "Worker not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	lb.BroadcastStatus()
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func handleWorkerConfig(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/workers/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "config" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	workerName := parts[0]

	lb.mu.RLock()
	var workerURL string
	for _, worker := range lb.workers {
		if worker.Name == workerName {
			workerURL = worker.URL
			break
		}
	}
	lb.mu.RUnlock()

	if workerURL == "" {
		http.Error(w, "Worker not found", http.StatusNotFound)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	var proxyReq *http.Request
	var err error

	switch r.Method {
	case http.MethodGet:
		proxyReq, err = http.NewRequestWithContext(r.Context(), http.MethodGet, workerURL+"/config", nil)
	case http.MethodPut, http.MethodPost:
		proxyReq, err = http.NewRequestWithContext(r.Context(), r.Method, workerURL+"/config", r.Body)
		if proxyReq != nil {
			proxyReq.Header.Set("Content-Type", "application/json")
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Failed to reach worker", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read worker response", http.StatusBadGateway)
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err == nil {
		result["worker"] = workerName
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		json.NewEncoder(w).Encode(result)
	} else {
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	lb.wsClientsMu.Lock()
	lb.wsClients[conn] = true
	lb.wsClientsMu.Unlock()

	status := lb.GetStatus()
	data, _ := json.Marshal(status)
	conn.WriteMessage(websocket.TextMessage, data)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			lb.wsClientsMu.Lock()
			delete(lb.wsClients, conn)
			lb.wsClientsMu.Unlock()
			conn.Close()
			break
		}
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	lb = NewLoadBalancer()

	if algo := os.Getenv("LB_ALGORITHM"); algo != "" {
		lb.SetAlgorithm(algo)
	}

	workerConfigs := []struct {
		envVar  string
		name    string
		color   string
		weight  int
		maxLoad int
	}{
		{"WORKER_GO_1_URL", "go-worker-1", "#3B82F6", 5, 3},
		{"WORKER_GO_2_URL", "go-worker-2", "#6366F1", 2, 3},
		{"WORKER_RUST_1_URL", "rust-worker-1", "#F97316", 6, 3},
		{"WORKER_RUST_2_URL", "rust-worker-2", "#EAB308", 1, 3},
		{"WORKER_PYTHON_1_URL", "python-worker-1", "#10B981", 1, 3},
		{"WORKER_PYTHON_2_URL", "python-worker-2", "#14B8A6", 3, 3},
	}

	for _, cfg := range workerConfigs {
		if url := os.Getenv(cfg.envVar); url != "" {
			weightEnvKey := strings.ToUpper(strings.ReplaceAll(cfg.name, "-", "_")) + "_WEIGHT"
			weight := cfg.weight
			if wStr := os.Getenv(weightEnvKey); wStr != "" {
				if w, err := strconv.Atoi(wStr); err == nil && w > 0 {
					weight = w
				}
			}
			lb.AddWorker(cfg.name, url, cfg.color, weight, cfg.maxLoad)
			log.Printf("Added worker: %s -> %s (weight=%d, maxLoad=%d)", cfg.name, url, weight, cfg.maxLoad)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go lb.HealthCheck(ctx, 5*time.Second)
	go lb.StartBroadcast(ctx, 1*time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/task", handleTask)
	mux.HandleFunc("/status", handleStatus)
	mux.HandleFunc("/algorithm", handleAlgorithm)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ws", handleWebSocket)
	mux.HandleFunc("/workers/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/workers/")
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		if len(parts) == 2 && parts[1] == "config" {
			handleWorkerConfig(w, r)
		} else {
			handleWorker(w, r)
		}
	})
	mux.Handle("/metrics", promhttp.Handler())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	handler := corsMiddleware(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: handler,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Received shutdown signal, stopping...")
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("Load balancer starting on port %s with algorithm %s", port, lb.algorithm)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Println("Load balancer stopped")
}
