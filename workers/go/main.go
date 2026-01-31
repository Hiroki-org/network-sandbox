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

// init はパッケージで使用する Prometheus メトリクス（requestsTotal、requestDuration、currentLoad）を登録します。
func init() {
	prometheus.MustRegister(requestsTotal)
	prometheus.MustRegister(requestDuration)
	prometheus.MustRegister(currentLoad)
}

// getEnvInt は環境変数 key を整数として読み取り、値が設定されていないか変換に失敗した場合は defaultVal を返します。
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// getEnvFloatは指定した環境変数を読み取り、浮動小数点値に変換して返します。
// 環境変数が設定されていないか有効な浮動小数点に変換できない場合はdefaultValを返します。
func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

// loadConfig は環境変数から初期 Configuration を構築して返します。
// 使用する環境変数とデフォルト値: MAX_CONCURRENT_REQUESTS=10, RESPONSE_DELAY_MS=100, FAILURE_RATE=0.0, QUEUE_SIZE=50。
// 環境変数が未設定または無効な場合は対応するデフォルト値が使われます。
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

// handleTask は POST /task リクエストを処理し、エントリーポイントのキュー受け入れと同時実行制御を行った上で疑似的な処理遅延と故障をシミュレートして JSON レスポンスを返します。
// キューが満杯または同時実行上限超過時は 503 を、リクエストボディが不正な場合は 400 を、シミュレート故障時は 500 を返し、成功時は処理情報を含む TaskResponse を返します。
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
		// Note: defer will handle decrement, no need for explicit decrement here
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

// handleHealth は現在の同時処理数とキュー深度を評価してサービスのヘルス状態を判定し、JSON で結果を返します。
// 
// 判定は現在の負荷比率（現在の同時処理数 / MaxConcurrentRequests）とキュー比率（キュー深度 / QueueSize）に基づき、
// いずれかの比率が 0.9 以上で "unhealthy"、いずれかが 0.7 以上で "degraded"、それ以外は "healthy" を返します。
// レスポンスは Content-Type: application/json を設定し、HealthResponse（Status, CurrentLoad, QueueDepth）をエンコードして返します.
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

// handleConfig はランタイム設定の取得と更新を行う HTTP ハンドラです。
// GET リクエストでは現在の設定を JSON で返します。
// PUT または POST リクエストではリクエストボディの JSON を Configuration としてデコードし、妥当であれば設定を反映して更新後の設定を JSON で返し、更新内容をログに記録します。
// ボディのデコードに失敗した場合は 400 Bad Request を返します。
// その他の HTTP メソッドに対しては 405 Method Not Allowed を返します。
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

// 設定されるヘッダー: Access-Control-Allow-Origin="*", Access-Control-Allow-Methods="GET, POST, PUT, OPTIONS", Access-Control-Allow-Headers="Content-Type".
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

// main はワーカー用の HTTP サーバーを初期化して起動します。
// 環境変数から構成とワーカー情報を読み込み、要求キューとメトリクスを初期化し、/task、/health、/config、/metrics のハンドラを登録して CORS を適用します。
// 指定したポート（PORT 環境変数、未指定時は 8080）でリクエストを受け付け、SIGINT/SIGTERM 受信時にグレースフルシャットダウンを行います。
func main() {
	// Note: As of Go 1.20+, the global random is automatically seeded
	// No need for explicit rand.Seed call

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