use axum::{
    extract::State,
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use sha2::{Digest, Sha256};
use std::collections::VecDeque;
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};
use tokio::sync::Mutex;
use tower_http::cors::CorsLayer;

const NUM_BUCKETS: usize = 5;

#[derive(Debug, Clone, Copy, PartialEq)]
enum FaultProbeMode {
    None,
    Timeout,
    Http500,
    Malformed,
    Stale,
}

impl FaultProbeMode {
    fn from_env(val: &str) -> Self {
        match val {
            "" => FaultProbeMode::None,
            "timeout" => FaultProbeMode::Timeout,
            "500" => FaultProbeMode::Http500,
            "malformed" => FaultProbeMode::Malformed,
            "stale_timestamp" => FaultProbeMode::Stale,
            other => {
                eprintln!(
                    "WARNING: unknown FAULT_PROBE_MODE={:?}, defaulting to None",
                    other
                );
                FaultProbeMode::None
            }
        }
    }
}

struct AppState {
    rif: AtomicI64,
    latency_buckets: Vec<Mutex<VecDeque<f64>>>,
    work_multiplier: f64,
    max_samples_per_bucket: usize,
    fault_mode: FaultProbeMode,
    fault_timeout_ms: u64,
    fault_stale_offset_ms: u64,
}

#[derive(serde::Deserialize)]
struct WorkRequest {
    iterations: Option<u64>,
}

#[derive(serde::Serialize)]
struct WorkResponse {
    result: String,
    duration_ms: f64,
}

#[derive(serde::Serialize)]
struct HealthResponse {
    status: &'static str,
}

#[derive(serde::Serialize)]
struct ProbeResponse {
    rif: i64,
    latency_median_ms: f64,
    timestamp_ms: u64,
}

fn rif_bucket(rif: i64) -> usize {
    match rif {
        0 => 0,
        1 => 1,
        2..=3 => 2,
        4..=7 => 3,
        _ => 4,
    }
}

fn compute_median(latencies: &VecDeque<f64>) -> f64 {
    if latencies.is_empty() {
        return 0.0;
    }
    let mut sorted: Vec<f64> = latencies.iter().copied().collect();
    sorted.sort_by(|a, b| a.partial_cmp(b).unwrap());
    let mid = sorted.len() / 2;
    #[allow(clippy::manual_is_multiple_of)]
    if sorted.len() % 2 == 0 {
        (sorted[mid - 1] + sorted[mid]) / 2.0
    } else {
        sorted[mid]
    }
}

fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as u64
}

async fn work_handler(
    State(state): State<Arc<AppState>>,
    Json(payload): Json<WorkRequest>,
) -> Json<WorkResponse> {
    let arrival_rif = state.rif.fetch_add(1, Ordering::Relaxed);
    let start = std::time::Instant::now();

    let iterations = ((payload.iterations.unwrap_or(1000) as f64) * state.work_multiplier) as u64;

    let mut hash = vec![0u8; 32];
    for _ in 0..iterations {
        let mut hasher = Sha256::new();
        hasher.update(&hash);
        hash = hasher.finalize().to_vec();
    }

    let duration_ms = start.elapsed().as_secs_f64() * 1000.0;

    {
        let bucket_idx = rif_bucket(arrival_rif);
        let mut bucket = state.latency_buckets[bucket_idx].lock().await;
        if bucket.len() >= state.max_samples_per_bucket {
            bucket.pop_front();
        }
        bucket.push_back(duration_ms);
    }

    state.rif.fetch_sub(1, Ordering::Relaxed);

    let result: String = hash.iter().map(|b| format!("{:02x}", b)).collect();

    Json(WorkResponse {
        result,
        duration_ms,
    })
}

async fn health_handler() -> Json<HealthResponse> {
    Json(HealthResponse { status: "ok" })
}

async fn probe_handler(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let rif = state.rif.load(Ordering::Relaxed);
    let target_bucket = rif_bucket(rif);

    let mut median = 0.0_f64;
    let candidates: Vec<usize> = {
        let mut v = vec![target_bucket];
        for d in 1..NUM_BUCKETS {
            if target_bucket >= d {
                v.push(target_bucket - d);
            }
            if target_bucket + d < NUM_BUCKETS {
                v.push(target_bucket + d);
            }
        }
        v
    };
    'outer: for idx in candidates {
        let bucket = state.latency_buckets[idx].lock().await;
        if !bucket.is_empty() {
            median = compute_median(&bucket);
            break 'outer;
        }
    }

    let timestamp_ms = now_ms();

    match state.fault_mode {
        FaultProbeMode::None => {
            let resp = ProbeResponse {
                rif,
                latency_median_ms: median,
                timestamp_ms,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        FaultProbeMode::Timeout => {
            tokio::time::sleep(tokio::time::Duration::from_millis(state.fault_timeout_ms)).await;
            let resp = ProbeResponse {
                rif,
                latency_median_ms: median,
                timestamp_ms,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        FaultProbeMode::Http500 => {
            let resp = ProbeResponse {
                rif,
                latency_median_ms: median,
                timestamp_ms,
            };
            (StatusCode::INTERNAL_SERVER_ERROR, Json(resp)).into_response()
        }
        FaultProbeMode::Malformed => (
            StatusCode::OK,
            [("content-type", "application/json")],
            "this is not json",
        )
            .into_response(),
        FaultProbeMode::Stale => {
            let stale_timestamp_ms = timestamp_ms.saturating_sub(state.fault_stale_offset_ms);
            let resp = ProbeResponse {
                rif,
                latency_median_ms: median,
                timestamp_ms: stale_timestamp_ms,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
    }
}

#[tokio::main]
async fn main() {
    let port: u16 = std::env::var("PORT")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(8080);

    let work_multiplier: f64 = std::env::var("WORK_MULTIPLIER")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(1.0);

    let fault_mode =
        FaultProbeMode::from_env(&std::env::var("FAULT_PROBE_MODE").unwrap_or_default());

    let fault_timeout_ms: u64 = std::env::var("FAULT_TIMEOUT_MS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(300000);

    let fault_stale_offset_ms: u64 = std::env::var("FAULT_STALE_OFFSET_MS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(10000);

    let latency_buckets = (0..NUM_BUCKETS)
        .map(|_| Mutex::new(VecDeque::new()))
        .collect();

    let state = Arc::new(AppState {
        rif: AtomicI64::new(0),
        latency_buckets,
        work_multiplier,
        max_samples_per_bucket: 32,
        fault_mode,
        fault_timeout_ms,
        fault_stale_offset_ms,
    });

    println!(
        "prequal-backend listening on 0.0.0.0:{} fault_mode={:?} fault_timeout_ms={} fault_stale_offset_ms={}",
        port, fault_mode, fault_timeout_ms, fault_stale_offset_ms
    );

    let app = Router::new()
        .route("/work", post(work_handler))
        .route("/health", get(health_handler))
        .route("/prequal/probe", get(probe_handler))
        .layer(CorsLayer::permissive())
        .with_state(state);

    let addr = format!("0.0.0.0:{}", port);
    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fault_mode_parse_none_empty() {
        assert_eq!(FaultProbeMode::from_env(""), FaultProbeMode::None);
    }

    #[test]
    fn fault_mode_parse_timeout() {
        assert_eq!(FaultProbeMode::from_env("timeout"), FaultProbeMode::Timeout);
    }

    #[test]
    fn fault_mode_parse_500() {
        assert_eq!(FaultProbeMode::from_env("500"), FaultProbeMode::Http500);
    }

    #[test]
    fn fault_mode_parse_malformed() {
        assert_eq!(
            FaultProbeMode::from_env("malformed"),
            FaultProbeMode::Malformed
        );
    }

    #[test]
    fn fault_mode_parse_stale_timestamp() {
        assert_eq!(
            FaultProbeMode::from_env("stale_timestamp"),
            FaultProbeMode::Stale
        );
    }

    #[test]
    fn fault_mode_parse_unknown_defaults_to_none() {
        assert_eq!(FaultProbeMode::from_env("bogus"), FaultProbeMode::None);
    }

    #[test]
    fn compute_median_empty() {
        assert_eq!(compute_median(&VecDeque::new()), 0.0);
    }

    #[test]
    fn compute_median_single() {
        let mut d = VecDeque::new();
        d.push_back(42.0);
        assert_eq!(compute_median(&d), 42.0);
    }

    #[test]
    fn compute_median_even() {
        let mut d = VecDeque::new();
        d.push_back(1.0);
        d.push_back(3.0);
        assert_eq!(compute_median(&d), 2.0);
    }

    #[test]
    fn compute_median_odd() {
        let mut d = VecDeque::new();
        d.push_back(10.0);
        d.push_back(1.0);
        d.push_back(5.0);
        assert_eq!(compute_median(&d), 5.0);
    }
}
