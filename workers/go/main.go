package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Configuration holds simulation parameters
type Configuration struct {
	MaxConcurrentRequests int     `json:"max_concurrent_requests"`
	ResponseDelayMs       int     `json:"response_delay_ms"`
	FailureRate           float64 `json:"failure_rate"`
	QueueSize             int     `json:"queue_size"`
	mu                    sync.RWMutex
}

// TaskRequest represents incoming task
type TaskRequest struct {
	ID     string  `json:"id"`
	Weight float64 `json:"weight"`
}

// TaskResponse represents successful response
type TaskResponse struct {
	ID               string `json:"id"`
	Worker           string `json:"worker"`
	Color            string `json:"color"`
	ProcessingTimeMs int64  `json:"processingTimeMs"`
	Timestamp        string `json:"timestamp"`
}

// ErrorResponse represents error response
type ErrorResponse struct {
	Error  string `json:"error"`
	Worker string `json:"worker"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status      string `json:"status"`
	CurrentLoad int32  `json:"currentLoad"`
	QueueDepth  int    `json:"queueDepth"`
}

var (
	config      *Configuration
	workerName  string
	workerColor string

	// Metrics
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "worker_requests_total",
			Help: "Total number of requests processed",
		},
		[]string{"worker", "status"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "worker_request_duration_ms",
			Help:    "Request duration in milliseconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		},
		[]string{"worker"},
	)
	currentLoad = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "worker_current_load",
			Help: "Current number of concurrent requests",
		},
		[]string{"worker"},
	)

	// Concurrency control
	activeRequests int32
	requestQueue   chan struct{}
)

func init() {
	prometheus.MustRegister(requestsTotal)
	prometheus.MustRegister(requestDuration)
	prometheus.MustRegister(currentLoad)
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

func loadConfig() *Configuration {
	return &Configuration{
		MaxConcurrentRequests: getEnvInt("MAX_CONCURRENT_REQUESTS", 10),
		ResponseDelayMs:       getEnvInt("RESPONSE_DELAY_MS", 100),
		FailureRate:           getEnvFloat("FAILURE_RATE", 0.0),
		QueueSize:             getEnvInt("QUEUE_SIZE", 50),
	}
}

func (c *Configuration) Update(newConfig *Configuration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if newConfig.MaxConcurrentRequests > 0 {
		c.MaxConcurrentRequests = newConfig.MaxConcurrentRequests
	}
	if newConfig.ResponseDelayMs >= 0 {
		c.ResponseDelayMs = newConfig.ResponseDelayMs
	}
	if newConfig.FailureRate >= 0 && newConfig.FailureRate <= 1 {
		c.FailureRate = newConfig.FailureRate
	}
	if newConfig.QueueSize > 0 {
		c.QueueSize = newConfig.QueueSize
	}
}

func (c *Configuration) Get() Configuration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Configuration{
		MaxConcurrentRequests: c.MaxConcurrentRequests,
		ResponseDelayMs:       c.ResponseDelayMs,
		FailureRate:           c.FailureRate,
		QueueSize:             c.QueueSize,
	}
}

func handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.Get()

	// Check queue capacity
	select {
	case requestQueue <- struct{}{}:
		defer func() { <-requestQueue }()
	default:
		requestsTotal.WithLabelValues(workerName, "rejected").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "Queue full - service overloaded",
			Worker: workerName,
		})
		return
	}

	// Check concurrent request limit
	current := atomic.AddInt32(&activeRequests, 1)
	defer func() {
		atomic.AddInt32(&activeRequests, -1)
		currentLoad.WithLabelValues(workerName).Set(float64(atomic.LoadInt32(&activeRequests)))
	}()
	currentLoad.WithLabelValues(workerName).Set(float64(current))

	if int(current) > cfg.MaxConcurrentRequests {
		atomic.AddInt32(&activeRequests, -1)
		requestsTotal.WithLabelValues(workerName, "overloaded").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  fmt.Sprintf("Max concurrent requests exceeded (%d/%d)", current, cfg.MaxConcurrentRequests),
			Worker: workerName,
		})
		return
	}

	// Parse request
	var task TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		requestsTotal.WithLabelValues(workerName, "error").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "Invalid request body",
			Worker: workerName,
		})
		return
	}

	startTime := time.Now()

	// Simulate processing with delay
	weight := task.Weight
	if weight <= 0 {
		weight = 1
	}
	delay := time.Duration(float64(cfg.ResponseDelayMs)*weight) * time.Millisecond
	time.Sleep(delay)

	processingTime := time.Since(startTime).Milliseconds()
	requestDuration.WithLabelValues(workerName).Observe(float64(processingTime))

	// Simulate failure based on failure rate
	if rand.Float64() < cfg.FailureRate {
		requestsTotal.WithLabelValues(workerName, "failed").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "Simulated failure",
			Worker: workerName,
		})
		return
	}

	// Success response
	requestsTotal.WithLabelValues(workerName, "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TaskResponse{
		ID:               task.ID,
		Worker:           workerName,
		Color:            workerColor,
		ProcessingTimeMs: processingTime,
		Timestamp:        time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.Get()
	load := atomic.LoadInt32(&activeRequests)
	queueDepth := len(requestQueue)

	var status string
	loadRatio := float64(load) / float64(cfg.MaxConcurrentRequests)
	queueRatio := float64(queueDepth) / float64(cfg.QueueSize)

	switch {
	case loadRatio >= 0.9 || queueRatio >= 0.9:
		status = "unhealthy"
	case loadRatio >= 0.7 || queueRatio >= 0.7:
		status = "degraded"
	default:
		status = "healthy"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{
		Status:      status,
		CurrentLoad: load,
		QueueDepth:  queueDepth,
	})
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config.Get())
	case http.MethodPut, http.MethodPost:
		var newConfig Configuration
		if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
			http.Error(w, "Invalid config body", http.StatusBadRequest)
			return
		}
		config.Update(&newConfig)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config.Get())
		log.Printf("Config updated: %+v\n", config.Get())
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Load configuration
	config = loadConfig()
	workerName = os.Getenv("WORKER_NAME")
	if workerName == "" {
		workerName = "go-worker-1"
	}
	workerColor = os.Getenv("WORKER_COLOR")
	if workerColor == "" {
		workerColor = "#3B82F6" // Blue
	}

	// Initialize request queue
	requestQueue = make(chan struct{}, config.QueueSize)

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/task", handleTask)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/config", handleConfig)
	mux.Handle("/metrics", promhttp.Handler())

	handler := corsMiddleware(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	log.Printf("Starting %s on port %s (color: %s)\n", workerName, port, workerColor)
	log.Printf("Config: max_concurrent=%d, delay=%dms, failure_rate=%.2f, queue_size=%d\n",
		config.MaxConcurrentRequests, config.ResponseDelayMs, config.FailureRate, config.QueueSize)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
