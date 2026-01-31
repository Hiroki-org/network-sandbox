use axum::{
    extract::State,
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use metrics::{counter, gauge, histogram};
use metrics_exporter_prometheus::{Matcher, PrometheusBuilder, PrometheusHandle};
use parking_lot::RwLock;
use rand::Rng;
use serde::{Deserialize, Serialize};
use std::{
    env,
    net::SocketAddr,
    sync::{
        atomic::{AtomicI32, AtomicI64, Ordering},
        Arc,
    },
    time::{Duration, Instant},
};
use tokio::{
    signal,
    sync::Semaphore,
    time::sleep,
};
use tower_http::cors::{Any, CorsLayer};

#[derive(Debug, Clone, Serialize, Deserialize)]
struct Configuration {
    max_concurrent_requests: i32,
    response_delay_ms: i32,
    failure_rate: f64,
    queue_size: i32,
}

#[derive(Debug, Deserialize)]
struct TaskRequest {
    id: String,
    weight: Option<f64>,
}

#[derive(Debug, Serialize)]
struct TaskResponse {
    id: String,
    worker: String,
    color: String,
    #[serde(rename = "processingTimeMs")]
    processing_time_ms: i64,
    timestamp: String,
}

#[derive(Debug, Serialize)]
struct ErrorResponse {
    error: String,
    worker: String,
}

#[derive(Debug, Serialize)]
struct HealthResponse {
    status: String,
    #[serde(rename = "currentLoad")]
    current_load: i32,
    #[serde(rename = "queueDepth")]
    queue_depth: i32,
}

struct AppState {
    config: RwLock<Configuration>,
    worker_name: String,
    worker_color: String,
    active_requests: AtomicI32,
    queue_semaphore: Semaphore,
    queue_size: AtomicI64,
    prometheus_handle: PrometheusHandle,
}

fn get_env_i32(key: &str, default: i32) -> i32 {
    env::var(key)
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(default)
}

fn get_env_f64(key: &str, default: f64) -> f64 {
    env::var(key)
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(default)
}

fn load_config() -> Configuration {
    Configuration {
        max_concurrent_requests: get_env_i32("MAX_CONCURRENT_REQUESTS", 10),
        response_delay_ms: get_env_i32("RESPONSE_DELAY_MS", 100),
        failure_rate: get_env_f64("FAILURE_RATE", 0.0),
        queue_size: get_env_i32("QUEUE_SIZE", 50),
    }
}

fn setup_metrics() -> PrometheusHandle {
    PrometheusBuilder::new()
        .set_buckets_for_metric(
            Matcher::Full("worker_request_duration_ms".to_string()),
            &[1.0, 2.0, 4.0, 8.0, 16.0, 32.0, 64.0, 128.0, 256.0, 512.0],
        )
        .unwrap()
        .install_recorder()
        .unwrap()
}

async fn handle_task(
    State(state): State<Arc<AppState>>,
    Json(task): Json<TaskRequest>,
) -> impl IntoResponse {
    let config = state.config.read().clone();

    // Try to acquire queue slot
    let permit = match state.queue_semaphore.try_acquire() {
        Ok(p) => {
            state.queue_size.fetch_add(1, Ordering::SeqCst);
            p
        }
        Err(_) => {
            counter!("worker_requests_total", "worker" => state.worker_name.clone(), "status" => "rejected").increment(1);
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(ErrorResponse {
                    error: "Queue full - service overloaded".to_string(),
                    worker: state.worker_name.clone(),
                }),
            )
                .into_response();
        }
    };

    // Check concurrent request limit
    let current = state.active_requests.fetch_add(1, Ordering::SeqCst) + 1;
    gauge!("worker_current_load", "worker" => state.worker_name.clone()).set(current as f64);

    if current > config.max_concurrent_requests {
        state.active_requests.fetch_sub(1, Ordering::SeqCst);
        state.queue_size.fetch_sub(1, Ordering::SeqCst);
        drop(permit);
        counter!("worker_requests_total", "worker" => state.worker_name.clone(), "status" => "overloaded").increment(1);
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(ErrorResponse {
                error: format!(
                    "Max concurrent requests exceeded ({}/{})",
                    current, config.max_concurrent_requests
                ),
                worker: state.worker_name.clone(),
            }),
        )
            .into_response();
    }

    let start = Instant::now();

    // Simulate processing with delay
    let weight = task.weight.unwrap_or(1.0).max(0.1);
    let delay = Duration::from_millis((config.response_delay_ms as f64 * weight) as u64);
    sleep(delay).await;

    let processing_time = start.elapsed().as_millis() as i64;
    histogram!("worker_request_duration_ms", "worker" => state.worker_name.clone()).record(processing_time as f64);

    // Cleanup
    state.active_requests.fetch_sub(1, Ordering::SeqCst);
    state.queue_size.fetch_sub(1, Ordering::SeqCst);
    gauge!("worker_current_load", "worker" => state.worker_name.clone())
        .set(state.active_requests.load(Ordering::SeqCst) as f64);
    drop(permit);

    // Simulate failure based on failure rate
    let mut rng = rand::thread_rng();
    if rng.gen::<f64>() < config.failure_rate {
        counter!("worker_requests_total", "worker" => state.worker_name.clone(), "status" => "failed").increment(1);
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(ErrorResponse {
                error: "Simulated failure".to_string(),
                worker: state.worker_name.clone(),
            }),
        )
            .into_response();
    }

    // Success response
    counter!("worker_requests_total", "worker" => state.worker_name.clone(), "status" => "success").increment(1);

    let response = TaskResponse {
        id: task.id,
        worker: state.worker_name.clone(),
        color: state.worker_color.clone(),
        processing_time_ms: processing_time,
        timestamp: chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Nanos, true),
    };

    Json(response).into_response()
}

async fn handle_health(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let config = state.config.read();
    let load = state.active_requests.load(Ordering::SeqCst);
    let queue_depth = state.queue_size.load(Ordering::SeqCst) as i32;

    let load_ratio = load as f64 / config.max_concurrent_requests as f64;
    let queue_ratio = queue_depth as f64 / config.queue_size as f64;

    let status = if load_ratio >= 0.9 || queue_ratio >= 0.9 {
        "unhealthy"
    } else if load_ratio >= 0.7 || queue_ratio >= 0.7 {
        "degraded"
    } else {
        "healthy"
    };

    Json(HealthResponse {
        status: status.to_string(),
        current_load: load,
        queue_depth,
    })
}

async fn handle_config_get(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let config = state.config.read().clone();
    Json(config)
}

async fn handle_config_update(
    State(state): State<Arc<AppState>>,
    Json(new_config): Json<Configuration>,
) -> impl IntoResponse {
    let mut config = state.config.write();
    if new_config.max_concurrent_requests > 0 {
        config.max_concurrent_requests = new_config.max_concurrent_requests;
    }
    if new_config.response_delay_ms >= 0 {
        config.response_delay_ms = new_config.response_delay_ms;
    }
    if new_config.failure_rate >= 0.0 && new_config.failure_rate <= 1.0 {
        config.failure_rate = new_config.failure_rate;
    }
    if new_config.queue_size > 0 {
        config.queue_size = new_config.queue_size;
    }
    tracing::info!("Config updated: {:?}", *config);
    Json(config.clone())
}

async fn handle_metrics(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    state.prometheus_handle.render()
}

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install signal handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }

    tracing::info!("Shutdown signal received");
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt::init();

    let config = load_config();
    let worker_name = env::var("WORKER_NAME").unwrap_or_else(|_| "rust-worker-1".to_string());
    let worker_color = env::var("WORKER_COLOR").unwrap_or_else(|_| "#F97316".to_string());
    let port = env::var("PORT").unwrap_or_else(|_| "8080".to_string());

    let prometheus_handle = setup_metrics();

    let queue_size = config.queue_size as usize;
    let state = Arc::new(AppState {
        config: RwLock::new(config.clone()),
        worker_name: worker_name.clone(),
        worker_color: worker_color.clone(),
        active_requests: AtomicI32::new(0),
        queue_semaphore: Semaphore::new(queue_size),
        queue_size: AtomicI64::new(0),
        prometheus_handle,
    });

    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any);

    let app = Router::new()
        .route("/task", post(handle_task))
        .route("/health", get(handle_health))
        .route("/config", get(handle_config_get).post(handle_config_update).put(handle_config_update))
        .route("/metrics", get(handle_metrics))
        .layer(cors)
        .with_state(state);

    let addr: SocketAddr = format!("0.0.0.0:{}", port).parse().unwrap();
    tracing::info!(
        "Starting {} on port {} (color: {})",
        worker_name,
        port,
        worker_color
    );
    tracing::info!(
        "Config: max_concurrent={}, delay={}ms, failure_rate={:.2}, queue_size={}",
        config.max_concurrent_requests,
        config.response_delay_ms,
        config.failure_rate,
        config.queue_size
    );

    let listener = tokio::net::TcpListener::bind(addr).await.unwrap();
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await
        .unwrap();
}
