# Run Report

## Metadata

| Field | Value |
|-------|-------|
| Run ID | 2026-04-19T08-39-20Z-uniform-smoke-round-robin |
| Date (UTC) | 2026-04-19T08:39:20Z |
| Git SHA | 2db41c40bf43b8b3d05052debab7d9adef714746 |
| Environment | kind-local |

## Scenario

| Field | Value |
|-------|-------|
| Scenario | uniform-smoke |
| Algorithm | round-robin |
| Load script | benchmark/k6/steady_state.js |
| Workload manifest | benchmark/manifests/workload-uniform.yaml |

## Load Profile

| Field | Value |
|-------|-------|
| Target rate (RPS) | 0 |
| Duration (seconds) | 60 |

## Key Metrics

| Metric | Value |
|--------|-------|
| Total requests | n/a |
| Success rate (%) | 100.00 |
| Error rate (%) | 0.00 |
| p50 latency (ms) | n/a |
| p95 latency (ms) | n/a |
| p99 latency (ms) | n/a |

## Anomalies

ralph-session smoke run, 1 rep, kind-local

## Raw Artifacts

- [k6 summary](k6-summary.json)
- [kubectl top](kubectl-top.txt)
- [Prometheus export](prometheus-export/)
- [Controller logs](controller-logs.txt)
- [Backend logs](backend-logs.txt)
- [Routes snapshot](routes.json)
- [Collection notes](notes.md)
