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

/// 環境変数からi32値を取得し、存在しないか整数に変換できない場合はデフォルト値を返す。
///
/// 指定したキーの環境変数を読み取り、UTF-8文字列をi32として解析して返します。環境変数が未設定または解析に失敗した場合は `default` を返します。
///
/// # Examples
///
/// ```
/// use std::env;
/// // 値が設定されている場合はその値を返す
/// env::set_var("TEST_INT", "42");
/// assert_eq!(get_env_i32("TEST_INT", 10), 42);
///
/// // 不正な値の場合はデフォルトを返す
/// env::set_var("TEST_INT", "not_an_int");
/// assert_eq!(get_env_i32("TEST_INT", 10), 10);
///
/// // 未設定の場合はデフォルトを返す
/// env::remove_var("TEST_INT");
/// assert_eq!(get_env_i32("TEST_INT", 10), 10);
/// ```
fn get_env_i32(key: &str, default: i32) -> i32 {
    env::var(key)
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(default)
}

/// 環境変数を読み取り、f64 に変換して返す。
///
/// 指定した `key` の環境変数を読み取り、`f64` にパースできればその値を返します。環境変数が未設定またはパースに失敗した場合は `default` を返します。
///
/// # Examples
///
/// ```
/// use std::env;
///
/// // 環境変数が数値の場合はその値を返す
/// env::set_var("TEST_F64", "3.14");
/// assert_eq!(get_env_f64("TEST_F64", 1.0), 3.14);
///
/// // 未設定または無効な値の場合はデフォルトを返す
/// env::remove_var("TEST_F64");
/// assert_eq!(get_env_f64("TEST_F64", 2.5), 2.5);
/// env::set_var("TEST_F64", "not_a_number");
/// assert_eq!(get_env_f64("TEST_F64", 4.2), 4.2);
/// ```
fn get_env_f64(key: &str, default: f64) -> f64 {
    env::var(key)
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(default)
}

/// 環境変数からランタイム設定を読み取り、Configuration構造体を生成する。
///
/// 環境変数が存在しないか解析できない場合は既定値を使用する：
/// - `MAX_CONCURRENT_REQUESTS` → 10
/// - `RESPONSE_DELAY_MS` → 100
/// - `FAILURE_RATE` → 0.0
/// - `QUEUE_SIZE` → 50
///
/// # Examples
///
/// ```
/// use std::env;
/// // 環境変数が未設定の場合はデフォルトが使われる
/// env::remove_var("MAX_CONCURRENT_REQUESTS");
/// env::remove_var("RESPONSE_DELAY_MS");
/// env::remove_var("FAILURE_RATE");
/// env::remove_var("QUEUE_SIZE");
///
/// let cfg = load_config();
/// assert_eq!(cfg.max_concurrent_requests, 10);
/// assert_eq!(cfg.response_delay_ms, 100);
/// assert_eq!(cfg.failure_rate, 0.0);
/// assert_eq!(cfg.queue_size, 50);
/// ```
fn load_config() -> Configuration {
    let max_concurrent = get_env_i32("MAX_CONCURRENT_REQUESTS", 10).max(1);
    let response_delay = get_env_i32("RESPONSE_DELAY_MS", 100).max(0);
    let failure_rate = get_env_f64("FAILURE_RATE", 0.0).clamp(0.0, 1.0);
    let queue_size = get_env_i32("QUEUE_SIZE", 50).max(1);

    Configuration {
        max_concurrent_requests: max_concurrent,
        response_delay_ms: response_delay,
        failure_rate,
        queue_size,
    }
}

/// Prometheus メトリクスを初期化してカスタムヒストグラムバケットを設定し、レンダリング用のハンドルを返す。
///
/// この関数はサービスで使用するメトリクスレコーダーをインストールし、
/// リクエスト処理時間を収集する `worker_request_duration_ms` メトリクスに対して
/// カスタムバケットを設定してからハンドルを返します。
///
/// # Returns
///
/// `PrometheusHandle` — メトリクスをレンダリング・取得するためのハンドル。
///
/// # Examples
///
/// ```
/// let handle = setup_metrics();
/// let output = handle.render();
/// assert!(output.contains("worker_request_duration_ms"));
/// ```
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

/// タスク要求を処理し、成功時は TaskResponse を、失敗時は ErrorResponse を返すハンドラ。
///
/// 必要に応じてキュー許可を取得して同時実行数を管理し、構成に基づく遅延をシミュレートし、
/// プロセッシング時間やステータス（success/failed/rejected/overloaded）をプロメテウス用メトリクスに記録する。
/// - キューが満杯の場合は 503 を返す（エラー "Queue full - service overloaded"）。
/// - 同時実行上限を超えた場合は 503 を返す（エラーに現在数と上限を含む）。
/// - 設定された failure_rate によっては 500 を返す（エラー "Simulated failure"）。
/// - 成功時は TaskResponse を JSON で返す。
///
/// 注意: 関数は State と Json の抽出済みパラメータを受け取り、内部でアトミックカウンタとセマフォを更新する。
///
/// # Examples
///
/// ```
/// use axum::Json;
/// use axum::extract::State;
/// use std::sync::Arc;
///
/// // テスト用に AppState を構築し、ルート経由で handle_task を呼び出すのが最も簡単な利用方法です。
/// // ここでは概念例として、実際の構築手順は省略しています。
///
/// // let app_state = Arc::new(AppState::new_for_test());
/// // let req = TaskRequest { id: "1".into(), weight: Some(1.0) };
/// // let resp = handle_task(State(app_state), Json(req)).await;
/// ```
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

/// ヘルスチェックを作成し、現在の負荷とキュー深度に基づいてサービスの状態を返すハンドラ。
///
/// 現在の同時処理数とキュー深度を取得し、構成の最大値に対する比率から状態を決定する：
/// - 比率が 0.9 以上なら `unhealthy`
/// - 比率が 0.7 以上なら `degraded`
/// - それ以外は `healthy`
///
/// 返却される JSON ペイロードは `HealthResponse` で、状態文字列、現在の負荷（in-flight リクエスト数）、キュー深度を含む。
///
/// # Examples
///
/// ```no_run
/// use std::sync::Arc;
/// use axum::extract::State;
///
/// #[tokio::main]
/// async fn main() {
///     // `state` は実際のアプリケーションの共有状態を指す Arc<AppState> とする
///     let state: Arc<AppState> = /* 既存の AppState を構築 */ unimplemented!();
///     let response = handle_health(State(state)).await;
///     // `response` は `HealthResponse` を含む JSON レスポンスになる
/// }
/// ```
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

/// 設定（Configuration）の現在値をJSONで返すエンドポイントハンドラ。

///

/// レスポンスとして現在の`Configuration`クローンをJSON形式で返します。

///

/// # Examples

///

/// ```no_run

/// use std::sync::Arc;

/// use axum::extract::State;

/// // `AppState` と `Configuration` は本クレートで定義されている型とする

///

/// // 既存のアプリケーション状態を持つ `state` を用意して

/// // let state: Arc<AppState> = ...;

/// // let response = handle_config_get(State(state)).await;

/// ```
async fn handle_config_get(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let config = state.config.read().clone();
    Json(config)
}

/// 設定値を受け取り、妥当なフィールドのみアプリケーションのランタイム設定に反映して更新済みの設定を返すハンドラー。
///
/// 与えられた `Configuration` の各フィールドは次の条件を満たす場合にのみ現在の設定へ適用される:
/// - `max_concurrent_requests > 0`
/// - `response_delay_ms >= 0`
/// - `0.0 <= failure_rate <= 1.0`
/// - `queue_size > 0`
///
/// 更新後の設定はログに記録され、クライアントへ JSON として返される。
///
/// # Returns
///
/// 更新後の `Configuration` を含む JSON レスポンス。
///
/// # Examples
///
/// ```ignore
/// // ハンドラーの使い方（概念例）
/// // 実際の呼び出しは Axum のルーティング経由で行われる。
/// let new_cfg = Configuration {
///     max_concurrent_requests: 5,
///     response_delay_ms: 100,
///     failure_rate: 0.1,
///     queue_size: 50,
/// };
/// // POST /config に new_cfg を送ると、更新後の設定が JSON で返る
/// ```
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
    // Handle queue_size change with semaphore adjustment
    if new_config.queue_size > 0 && new_config.queue_size != config.queue_size {
        let delta = new_config.queue_size - config.queue_size;
        if delta > 0 {
            // Increase capacity by adding permits
            state.queue_semaphore.add_permits(delta as usize);
        }
        // Note: Decreasing semaphore permits atomically is complex in Tokio;
        // for simplicity, we only support increasing. Decreasing requires
        // acquiring permits which may block. Log a warning if decrease attempted.
        if delta < 0 {
            tracing::warn!(
                "Cannot decrease queue_size from {} to {} at runtime; only increases are supported",
                config.queue_size, new_config.queue_size
            );
        } else {
            config.queue_size = new_config.queue_size;
        }
    }
    tracing::info!("Config updated: {:?}", *config);
    Json(config.clone())
}

/// Prometheus のメトリクスをレンダリングして HTTP レスポンスの本文を生成するハンドラ。
///
/// 返り値は Prometheus ハンドラがレンダリングしたメトリクス本文（テキスト）で、HTTP のレスポンス本文として返却されます。
///
/// # Examples
///
/// ```no_run
/// use std::sync::Arc;
/// use axum::extract::State;
///
/// // `state` はサーバの共有状態で、内部に `prometheus_handle` を保持している想定です。
/// # async fn example(state: Arc<AppState>) {
/// let resp = handle_metrics(State(state)).await;
/// // `resp` はレンダリング済みメトリクスを含む HTTP レスポンスとなります。
/// # }
/// ```
async fn handle_metrics(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    state.prometheus_handle.render()
}

/// Ctrl+C またはプロセス終了シグナルを待機し、受信したらシャットダウンをログに記録する。
///
/// UNIX プラットフォームでは terminate シグナルも監視する。
///
/// # Examples
///
/// ```
/// #[tokio::main]
/// async fn main() {
///     // Ctrl+C またはプロセス終了シグナルを待ち、受信時にシャットダウンを記録する。
///     shutdown_signal().await;
/// }
/// ```
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

/// アプリケーションのHTTPサーバーを初期化し、ルーティング・メトリクス・共有状態を構成して起動する。
///
/// 初期設定を環境変数から読み込み、Prometheus メトリクスをセットアップし、セマフォやアトミックカウンタを含む共有 AppState を作成します。CORS を有効にした Axum ルーターを構築し、/task、/health、/config、/metrics のエンドポイントを登録した後、指定ポートでリッスンしてグレースフルシャットダウンを待機します。
///
/// # Examples
///
/// ```no_run
/// // 簡易的な起動例（環境変数で設定を与えてから実行）
/// // WORKER_NAME=demo WORKER_COLOR="#000000" PORT=8080 cargo run
/// ```
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