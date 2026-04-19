# Benchmarking

This directory contains reproducible benchmark assets for the ingress proxy:

- benchmark-grade controller manifests
- workload manifests for uniform, heterogeneous, and multi-route tests
- `k6` scripts for closed-loop and open-loop traffic
- a churn helper script for control-plane stability tests
- a local Prometheus + Grafana observability stack
- a campaign runner, result collector, archiver, and report templates

The full campaign plan (scenarios × algorithms × environments) is tracked in
[`benchmark/benchmarking-matrix.md`](benchmarking-matrix.md).

## Manifests

- `benchmark/manifests/controller-benchmark.yaml`
  Deploys the ingress controller with 3 replicas, benchmark-mode logging, and tunable probe settings.
- `benchmark/manifests/workload-uniform.yaml`
  Single-service workload for baseline comparisons.
- `benchmark/manifests/workload-heterogeneous.yaml`
  Mixed fast/slow backends behind one service for algorithm comparisons.
- `benchmark/manifests/workload-multiroute.yaml`
  Multiple services and paths to validate route isolation and route-scale behavior.

## Closed-loop smoke test

```bash
k6 run \
  -e TARGET_URL=http://127.0.0.1:30080/work \
  -e HOST_HEADER=bench.local \
  -e WORK_ITERATIONS=1000 \
  benchmark/k6/steady_state.js
```

Useful environment overrides:

- `TARGET_URL`: full URL routed through the ingress
- `HOST_HEADER`: host header used for ingress matching
- `WORK_ITERATIONS`: backend work payload
- `VUS`: virtual users, default `50`
- `DURATION`: steady-state duration, default `60s`

## Open-loop fixed-rate test

```bash
k6 run \
  -e TARGET_URL=http://127.0.0.1:30080/work \
  -e HOST_HEADER=bench.local \
  -e WORK_ITERATIONS=1000 \
  -e RATE=500 \
  -e PRE_ALLOCATED_VUS=100 \
  -e MAX_VUS=500 \
  benchmark/k6/open_loop.js
```

Useful environment overrides:

- `RATE`: requests per second, default `200`
- `DURATION`: constant-arrival-rate window, default `60s`
- `PRE_ALLOCATED_VUS`: initial VU pool, default `64`
- `MAX_VUS`: max VUs allowed to keep target rate, default `256`

## Multi-route test

```bash
k6 run \
  -e BASE_URL=http://127.0.0.1:30080/work \
  -e ROUTE_A_HOST=route-a.bench.local \
  -e ROUTE_B_HOST=route-b.bench.local \
  -e ROUTE_A_WEIGHT=8 \
  -e ROUTE_B_WEIGHT=2 \
  benchmark/k6/multi_route.js
```

This script drives the same backend path with different host headers to validate that route-local balancing state stays isolated.

## Churn test

Apply the benchmark manifests, then run:

```bash
benchmark/scripts/churn.sh prequal-benchmark bench-uniform 3 6 5 20
```

Arguments are:

1. namespace
2. deployment name
3. low replica count
4. high replica count
5. pause seconds between changes
6. number of iterations

## Rate ramp

Drives a linearly increasing arrival rate to find the saturation point of the proxy. Useful for capacity-planning: watch p99 and error rate climb as the target rate exceeds backend capacity.

```bash
k6 run \
  -e TARGET_URL=http://127.0.0.1:30080/work \
  -e HOST_HEADER=bench.local \
  -e WORK_ITERATIONS=1000 \
  -e START_RATE=50 \
  -e STEP_RATE=50 \
  -e STEPS=6 \
  -e STEP_DURATION=60s \
  -e SUMMARY_PATH=./summary-rate_ramp.json \
  benchmark/k6/rate_ramp.js
```

- `START_RATE`: initial requests/sec after warmup, default `50`
- `STEP_RATE`: additional requests/sec added each step, default `50`
- `STEP_DURATION`: how long each step lasts, default `60s`
- `STEPS`: number of rate steps, default `6`
- `PRE_ALLOCATED_VUS`: initial VU pool, default `64`
- `MAX_VUS`: VU ceiling, default `512`
- `SUMMARY_PATH`: output JSON path, default `./summary-rate_ramp.json`

## Burst

Alternates quiet idle windows with high-rate burst windows to test the proxy's ability to absorb sudden traffic spikes and recover quickly.

```bash
k6 run \
  -e TARGET_URL=http://127.0.0.1:30080/work \
  -e HOST_HEADER=bench.local \
  -e WORK_ITERATIONS=1000 \
  -e IDLE_RATE=10 \
  -e BURST_RATE=500 \
  -e IDLE_DURATION=30s \
  -e BURST_DURATION=15s \
  -e CYCLES=5 \
  -e SUMMARY_PATH=./summary-burst.json \
  benchmark/k6/burst.js
```

- `IDLE_RATE`: requests/sec during quiet windows, default `10`
- `BURST_RATE`: requests/sec during burst windows, default `500`
- `IDLE_DURATION`: length of each idle window, default `30s`
- `BURST_DURATION`: length of each burst window, default `15s`
- `CYCLES`: number of idle/burst cycle pairs, default `5`
- `PRE_ALLOCATED_VUS`: initial VU pool, default `64`
- `MAX_VUS`: VU ceiling, default `512`
- `SUMMARY_PATH`: output JSON path, default `./summary-burst.json`

## Long-duration

Runs a sustained constant-arrival-rate load for an extended period (default 1 hour) to surface slow memory leaks, connection-pool exhaustion, or drift in the balancing algorithm over time. Emits a console marker every `MARKER_INTERVAL_SEC` seconds for Prometheus correlation.

```bash
k6 run \
  -e TARGET_URL=http://127.0.0.1:30080/work \
  -e HOST_HEADER=bench.local \
  -e WORK_ITERATIONS=1000 \
  -e RATE=200 \
  -e DURATION=3600s \
  -e MARKER_INTERVAL_SEC=300 \
  -e SUMMARY_PATH=./summary-long_duration.json \
  benchmark/k6/long_duration.js
```

- `RATE`: constant requests/sec, default `200`
- `DURATION`: total test duration, default `3600s`
- `MARKER_INTERVAL_SEC`: seconds between console marker lines, default `300`
- `PRE_ALLOCATED_VUS`: initial VU pool, default `64`
- `MAX_VUS`: VU ceiling, default `512`
- `SUMMARY_PATH`: output JSON path, default `./summary-long_duration.json`

## Overload

Intentionally targets the proxy well above its expected saturation point (`RATE * OVERLOAD_MULTIPLIER`) to measure graceful degradation, queue depth behavior, and recovery. No error-rate threshold is enforced — high failure rates are expected and informative.

```bash
k6 run \
  -e TARGET_URL=http://127.0.0.1:30080/work \
  -e HOST_HEADER=bench.local \
  -e WORK_ITERATIONS=1000 \
  -e RATE=1000 \
  -e OVERLOAD_MULTIPLIER=2.0 \
  -e DURATION=300s \
  -e SUMMARY_PATH=./summary-overload.json \
  benchmark/k6/overload.js
```

- `RATE`: base requests/sec before multiplier, default `1000`
- `OVERLOAD_MULTIPLIER`: factor applied to RATE for effective load, default `2.0`
- `DURATION`: test duration, default `300s`
- `PRE_ALLOCATED_VUS`: initial VU pool, default `256`
- `MAX_VUS`: VU ceiling, default `2048`
- `SUMMARY_PATH`: output JSON path, default `./summary-overload.json`

## Benchmark discipline

- Keep host, path, request body, runtime, and cluster shape constant across algorithm comparisons.
- Prefer `benchmark/k6/open_loop.js` for saturation and tail-latency work.
- Use `benchmark/k6/steady_state.js` only for fast smoke checks.
- Run `benchmark/scripts/churn.sh` during traffic to evaluate reconciliation stability.

## Additional manifests

### Workload manifests

- `benchmark/manifests/workload-route-scale.yaml` — 8 independent routes (`route-scale-1..8.bench.local`), 2 replicas each, for validating per-route balancing state isolation at scale. Pair with `benchmark/k6/multi_route.js` (set `ROUTE_SCALE_HOSTS` to the 8 hosts) or a `rate_ramp.js` config that cycles through all 8 hosts.
- `benchmark/manifests/workload-long-duration.yaml` — 6-replica stable deployment on `long.bench.local` with conservative resource limits (cpu 50m–500m, memory 64Mi–256Mi) for 1h+ soak runs. Pair with `benchmark/k6/long_duration.js`.

### Fault-injection manifests

> **These four manifests require a fault-injection-aware backend image that does not yet exist.**
> `prequal-backend:latest` does NOT implement `FAULT_PROBE_MODE`. The manifests define the deployment shape, ingress wiring, and env var contract so they are ready when a fault backend is built. See [benchmark/evidence-asset-spec.md section 10](evidence-asset-spec.md) for the full dependency description.

- `benchmark/manifests/fault-probe-timeout.yaml` — `FAULT_PROBE_MODE=timeout`; host `fault-timeout.bench.local`. Pair with `benchmark/k6/open_loop.js`.
- `benchmark/manifests/fault-probe-500.yaml` — `FAULT_PROBE_MODE=500` (probe returns HTTP 500); host `fault-500.bench.local`. Pair with `benchmark/k6/open_loop.js`.
- `benchmark/manifests/fault-probe-malformed.yaml` — `FAULT_PROBE_MODE=malformed` (probe returns malformed body); host `fault-malformed.bench.local`. Pair with `benchmark/k6/open_loop.js`.
- `benchmark/manifests/fault-probe-stale-timestamp.yaml` — `FAULT_PROBE_MODE=stale_timestamp` (probe returns stale timestamp); host `fault-stale.bench.local`. Pair with `benchmark/k6/open_loop.js`.

### Apply commands

```bash
# Route-scale workload
kubectl apply -f benchmark/manifests/workload-route-scale.yaml

# Long-duration soak workload
kubectl apply -f benchmark/manifests/workload-long-duration.yaml

# Fault-injection manifests (requires fault-injection backend image — see evidence-asset-spec.md §10)
kubectl apply -f benchmark/manifests/fault-probe-timeout.yaml
kubectl apply -f benchmark/manifests/fault-probe-500.yaml
kubectl apply -f benchmark/manifests/fault-probe-malformed.yaml
kubectl apply -f benchmark/manifests/fault-probe-stale-timestamp.yaml
```

## Observability stack

A local Prometheus + Grafana stack lives in [`benchmark/observability/`](observability/README.md).

Start it with:

```bash
cd benchmark/observability && docker compose up -d
```

Prometheus scrapes the controller metrics endpoint via NodePort **30081** (`host.docker.internal:30081`). Six dashboards are provisioned automatically:

| Dashboard | Description |
|-----------|-------------|
| **Request Overview** | req/s, error rate, latency p50-p99.9, no-route/no-backend events |
| **Algorithm Behavior** | selection counts by algorithm, backend selections, active backends, pool occupancy |
| **Probe System** | probes sent/succeeded/failed/dropped, queue depth, success ratio |
| **Control Plane** | reconciliation count + latency percentiles, active backends, no-backend events |
| **Resource Usage** | controller process memory, CPU rate, goroutines (node-exporter optional for full node metrics) |
| **Long-Duration Stability** | latency, request rate, error rate, queue depth, dropped probes over 6-24h |

See [benchmark/observability/README.md](observability/README.md) for prerequisites, verification steps, and node-exporter integration.

## Campaign runner and reporting

All scripts live in `benchmark/scripts/` and are executable. They produce deterministic
output under `benchmark/results/<DATE_UTC>-<SCENARIO>-<ALGORITHM>/`.

### Full flow

```
benchmark/scripts/deploy_benchmark_stack.sh
  -> benchmark/scripts/run_campaign.sh
     (internally calls collect_results.sh)
  -> benchmark/scripts/archive_results.sh
  -> benchmark/scripts/render_report.sh
```

### 1. Deploy the benchmark stack

```bash
NAMESPACE=prequal-benchmark \
MANIFESTS_DIR=benchmark/manifests \
WORKLOAD=workload-uniform.yaml \
benchmark/scripts/deploy_benchmark_stack.sh
```

Applies `controller-benchmark.yaml` and the chosen workload manifest, then waits
for `deployment/prequal-controller` to roll out (timeout 180s). Idempotent.

### 2. Run a campaign scenario

```bash
SCENARIO=uniform-open-loop \
ALGORITHM=prequal \
K6_SCRIPT=benchmark/k6/open_loop.js \
RATE=500 \
DURATION=300s \
NOTES="first comparison run" \
benchmark/scripts/run_campaign.sh
```

Writes to `benchmark/results/<DATE_UTC>-uniform-open-loop-prequal/`:
- `run-metadata.json` — machine-readable run schema (see `benchmark/results/schema/run-metadata.json`)
- `k6-summary.json` — raw k6 output
- `command.txt` — exact k6 command
- artifacts from `collect_results.sh` (see below)

Canonical k6 scripts for each scenario:
- Steady state: `benchmark/k6/steady_state.js`
- Open loop: `benchmark/k6/open_loop.js`
- Multi-route: `benchmark/k6/multi_route.js`
- Rate ramp: `benchmark/k6/rate_ramp.js`
- Burst: `benchmark/k6/burst.js`
- Long duration: `benchmark/k6/long_duration.js`
- Overload: `benchmark/k6/overload.js`

### 3. Collect results (called automatically by run_campaign.sh)

```bash
benchmark/scripts/collect_results.sh benchmark/results/<run-dir>
```

Best-effort; skips any tool that is missing and logs to `notes.md`. Collects:
`kubectl-top.txt`, `routes.json`, `controller-logs.txt`, `backend-logs.txt`,
and `prometheus-export/*.json` (if Prometheus is reachable at `http://localhost:9090`).

### 4. Archive a run

```bash
benchmark/scripts/archive_results.sh benchmark/results/<run-dir>
```

Produces `benchmark/results/archive/<run-dir-basename>.tar.gz` with paths
relative to the results root.

### 5. Render a Markdown report

```bash
benchmark/scripts/render_report.sh benchmark/results/<run-dir>
```

Reads `run-metadata.json` and `k6-summary.json`, substitutes values into
`benchmark/report/templates/run-report.md`, and writes `report.md` into the
run directory. Requires `jq` for full metric extraction; falls back to grep/sed
with reduced accuracy and notes the limitation in `notes.md`.

For campaign-level comparison across multiple runs, fill in
`benchmark/report/templates/campaign-report.md` manually or with a chart script.

## External baseline: NGINX Ingress

**Purpose:** Isolate proxy-overhead from prequal-specific algorithmic gains. By running
the same backend image, same k6 scripts, and same workload shapes through a mainstream
ingress controller, you get a proxy-cost floor that makes prequal tail-latency results
interpretable as algorithm effect rather than ingress-layer overhead. This is Claim-Level-B
evidence per `benchmark/public-claim-playbook.md` section 13.

### Install the NGINX controller

```bash
# Downloads controller-v1.11.2 from the official kind-compatible upstream,
# verifies SHA256, and applies it to the ingress-nginx namespace.
bash benchmark/scripts/install_nginx_baseline.sh

# Patch the NodePorts to 30180 (http) and 30143 (https) — avoids collision
# with the prequal controller on 30080/30443.
kubectl apply -f benchmark/manifests/baseline-nginx-controller.yaml
```

Wait for the controller to be ready:

```bash
kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --timeout=120s
```

### Apply baseline workloads

```bash
# Uniform workload (mirrors workload-uniform.yaml, host bench-nginx.local)
kubectl apply -f benchmark/manifests/baseline-nginx-workload.yaml

# Heterogeneous workload (mirrors workload-heterogeneous.yaml, host bench-nginx-het.local)
kubectl apply -f benchmark/manifests/baseline-nginx-heterogeneous.yaml
```

### Target NGINX from k6

Uniform:

```bash
TARGET_URL=http://127.0.0.1:30180/work \
HOST_HEADER=bench-nginx.local \
k6 run benchmark/k6/open_loop.js
```

Heterogeneous:

```bash
TARGET_URL=http://127.0.0.1:30180/work \
HOST_HEADER=bench-nginx-het.local \
k6 run benchmark/k6/open_loop.js
```

### Fairness notes

- Same backend image (`prequal-backend:latest`) with identical `WORK_MULTIPLIER` values.
- Same work payload (`/work` path, same k6 script and env vars).
- Same measurement window (300s, 5 repetitions per Campaign 9 rows).
- Record the NGINX Ingress controller version in `run-metadata.json` under `notes`
  (e.g. `"nginx_version": "controller-v1.11.2"`).

### Limitations

NGINX Ingress has no algorithm equivalent to prequal's HCL probe-driven selection.
Its upstream selection for a given service defaults to round-robin with keepalive.
Comparing tail latency to prequal is valid only as an **ingress-overhead baseline** —
it shows what a production-grade proxy costs without algorithmic awareness — not as a
direct algorithm comparison. Use Campaign 2 (prequal vs round-robin vs least-connections
within the same controller) for the algorithm comparison.
