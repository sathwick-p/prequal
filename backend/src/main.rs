use axum::{
    extract::State,
    routing::{get, post},
    Json, Router,
};
use sha2::{Digest, Sha256};
use std::collections::VecDeque;
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use std::time::{Instant, SystemTime, UNIX_EPOCH};
use tokio::sync::Mutex;
use tower_http::cors::CorsLayer;

const NUM_BUCKETS: usize = 5;

struct AppState {
    rif: AtomicI64,
    latency_buckets: Vec<Mutex<VecDeque<f64>>>,
    work_multiplier: f64,
    max_samples_per_bucket: usize,
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
    if sorted.len() % 2 == 0 {
        (sorted[mid - 1] + sorted[mid]) / 2.0
    } else {
        sorted[mid]
    }
}

async fn work_handler(
    State(state): State<Arc<AppState>>,
    Json(payload): Json<WorkRequest>,
) -> Json<WorkResponse> {
    let arrival_rif = state.rif.fetch_add(1, Ordering::Relaxed);
    let start = Instant::now();

    let iterations =
        ((payload.iterations.unwrap_or(1000) as f64) * state.work_multiplier) as u64;

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

    Json(WorkResponse { result, duration_ms })
}

async fn health_handler() -> Json<HealthResponse> {
    Json(HealthResponse { status: "ok" })
}

async fn probe_handler(State(state): State<Arc<AppState>>) -> Json<ProbeResponse> {
    let rif = state.rif.load(Ordering::Relaxed);
    let target_bucket = rif_bucket(rif);

    // Try the exact bucket first, then search outward for nearest non-empty bucket.
    // Candidates are ordered: target, target-1, target+1, target-2, target+2, ...
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

    let timestamp_ms = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as u64;

    Json(ProbeResponse {
        rif,
        latency_median_ms: median,
        timestamp_ms,
    })
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

    let latency_buckets = (0..NUM_BUCKETS)
        .map(|_| Mutex::new(VecDeque::new()))
        .collect();

    let state = Arc::new(AppState {
        rif: AtomicI64::new(0),
        latency_buckets,
        work_multiplier,
        max_samples_per_bucket: 32,
    });

    let app = Router::new()
        .route("/work", post(work_handler))
        .route("/health", get(health_handler))
        .route("/prequal/probe", get(probe_handler))
        .layer(CorsLayer::permissive())
        .with_state(state);

    let addr = format!("0.0.0.0:{}", port);
    println!("prequal-backend listening on {}", addr);

    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
