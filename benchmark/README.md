# Benchmarking

This directory contains reproducible benchmark assets for the ingress proxy:

- benchmark-grade controller manifests
- workload manifests for uniform, heterogeneous, and multi-route tests
- `k6` scripts for closed-loop and open-loop traffic
- a churn helper script for control-plane stability tests

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
