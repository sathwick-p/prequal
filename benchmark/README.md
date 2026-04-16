# Benchmarking

This directory contains reproducible load-test entry points for the ingress proxy.

## k6 steady state

Run against the ingress service with a fixed host/path and JSON workload:

```bash
k6 run \
  -e TARGET_URL=http://127.0.0.1:30080/work \
  -e HOST_HEADER=test.example.com \
  -e WORK_ITERATIONS=1000 \
  benchmark/k6/steady_state.js
```

Useful environment overrides:

- `TARGET_URL`: full URL routed through the ingress
- `HOST_HEADER`: host header used for ingress matching
- `WORK_ITERATIONS`: backend work payload
- `VUS`: virtual users, default `50`
- `DURATION`: steady-state duration, default `60s`

For serious comparisons, keep `TARGET_URL`, `HOST_HEADER`, request body, and runtime identical across algorithms.
