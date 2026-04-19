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

## Benchmark discipline

- Keep host, path, request body, runtime, and cluster shape constant across algorithm comparisons.
- Prefer `benchmark/k6/open_loop.js` for saturation and tail-latency work.
- Use `benchmark/k6/steady_state.js` only for fast smoke checks.
- Run `benchmark/scripts/churn.sh` during traffic to evaluate reconciliation stability.
