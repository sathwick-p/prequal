# Prequal: Benchmarking Campaign Matrix

Source documents:

- [`benchmark/public-claim-playbook.md`](public-claim-playbook.md) section 5 (campaign definitions)
- [`benchmark/evidence-asset-spec.md`](evidence-asset-spec.md) section 4.1 (matrix format)
- [`benchmarking.md`](../benchmarking.md) section 20 (recommended execution order)

This matrix enumerates every planned run for a Claim-Level-B public writeup. Each row is one scenario × algorithm. Mark `status` as one of: `planned`, `in-progress`, `done`, `blocked`. Record the output directory under `benchmark/results/` once the run starts.

Environments referenced:

- **E-A** — local `kind` single-node (smoke, harness validation)
- **E-B** — multi-node cluster (primary engineering comparison)
- **E-C** — independent second environment (cross-environment sanity)

Rate model shorthand:

- `closed-loop`: VU-driven (steady_state.js)
- `open-N`: constant arrival at N rps (open_loop.js / multi_route.js)
- `ramp`: staged increasing rate (rate_ramp.js)
- `burst`: alternating idle/burst (burst.js)
- `soak`: long-duration constant arrival (long_duration.js)
- `overload`: rate × overload_multiplier (overload.js)

Algorithms (`A`) per row: `prequal`, `round-robin`, `least-connections` unless a row explicitly restricts to a subset.

---

## Campaign 1 — Baseline correctness and smoke

Purpose: validate harness, manifests, metrics collection.

| campaign_id | scenario           | environment | algorithm        | script                                  | manifest                                           | rate_model   | duration | repetitions | owner        | status  | results_dir |
|-------------|--------------------|-------------|------------------|-----------------------------------------|----------------------------------------------------|--------------|----------|-------------|--------------|---------|-------------|
| C1-smoke-pq | uniform-smoke      | E-A         | prequal          | `benchmark/k6/steady_state.js`          | `benchmark/manifests/workload-uniform.yaml`        | closed-loop  | 60s      | 3           | unassigned   | planned |             |
| C1-smoke-rr | uniform-smoke      | E-A         | round-robin      | `benchmark/k6/steady_state.js`          | `benchmark/manifests/workload-uniform.yaml`        | closed-loop  | 60s      | 3           | unassigned   | planned |             |
| C1-smoke-lc | uniform-smoke      | E-A         | least-connections| `benchmark/k6/steady_state.js`          | `benchmark/manifests/workload-uniform.yaml`        | closed-loop  | 60s      | 3           | unassigned   | planned |             |

## Campaign 2 — Algorithm comparison under controlled load

Purpose: main comparison set (uniform + heterogeneous, open-loop).

| campaign_id    | scenario                 | environment | algorithm        | script                               | manifest                                                 | rate_model | duration | repetitions | owner      | status  | results_dir |
|----------------|--------------------------|-------------|------------------|--------------------------------------|----------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C2-uni-pq      | uniform-open-loop        | E-B         | prequal          | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-uniform.yaml`              | open-500   | 300s     | 5           | unassigned | planned |             |
| C2-uni-rr      | uniform-open-loop        | E-B         | round-robin      | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-uniform.yaml`              | open-500   | 300s     | 5           | unassigned | planned |             |
| C2-uni-lc      | uniform-open-loop        | E-B         | least-connections| `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-uniform.yaml`              | open-500   | 300s     | 5           | unassigned | planned |             |
| C2-het-pq      | heterogeneous-open-loop  | E-B         | prequal          | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-heterogeneous.yaml`        | open-500   | 300s     | 5           | unassigned | planned |             |
| C2-het-rr      | heterogeneous-open-loop  | E-B         | round-robin      | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-heterogeneous.yaml`        | open-500   | 300s     | 5           | unassigned | planned |             |
| C2-het-lc      | heterogeneous-open-loop  | E-B         | least-connections| `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-heterogeneous.yaml`        | open-500   | 300s     | 5           | unassigned | planned |             |

## Campaign 3 — Saturation and tail-latency ramp

Purpose: where each algorithm breaks down.

| campaign_id   | scenario           | environment | algorithm        | script                           | manifest                                                 | rate_model | duration | repetitions | owner      | status  | results_dir |
|---------------|--------------------|-------------|------------------|----------------------------------|----------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C3-ramp-pq    | heterogeneous-ramp | E-B         | prequal          | `benchmark/k6/rate_ramp.js`      | `benchmark/manifests/workload-heterogeneous.yaml`        | ramp       | 360s     | 3           | unassigned | planned |             |
| C3-ramp-rr    | heterogeneous-ramp | E-B         | round-robin      | `benchmark/k6/rate_ramp.js`      | `benchmark/manifests/workload-heterogeneous.yaml`        | ramp       | 360s     | 3           | unassigned | planned |             |
| C3-ramp-lc    | heterogeneous-ramp | E-B         | least-connections| `benchmark/k6/rate_ramp.js`      | `benchmark/manifests/workload-heterogeneous.yaml`        | ramp       | 360s     | 3           | unassigned | planned |             |

## Campaign 4 — Multi-route isolation

Purpose: verify route-scoped state isolation.

| campaign_id   | scenario              | environment | algorithm        | script                              | manifest                                                     | rate_model | duration | repetitions | owner      | status  | results_dir |
|---------------|-----------------------|-------------|------------------|-------------------------------------|--------------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C4-mr-pq      | multiroute-weighted   | E-B         | prequal          | `benchmark/k6/multi_route.js`       | `benchmark/manifests/workload-multiroute.yaml`               | open-200   | 300s     | 3           | unassigned | planned |             |
| C4-mr-rr      | multiroute-weighted   | E-B         | round-robin      | `benchmark/k6/multi_route.js`       | `benchmark/manifests/workload-multiroute.yaml`               | open-200   | 300s     | 3           | unassigned | planned |             |
| C4-mr-lc      | multiroute-weighted   | E-B         | least-connections| `benchmark/k6/multi_route.js`       | `benchmark/manifests/workload-multiroute.yaml`               | open-200   | 300s     | 3           | unassigned | planned |             |
| C4-rscale-pq  | route-scale-8         | E-B         | prequal          | `benchmark/k6/multi_route.js`       | `benchmark/manifests/workload-route-scale.yaml`              | open-400   | 300s     | 3           | unassigned | planned |             |
| C4-rscale-rr  | route-scale-8         | E-B         | round-robin      | `benchmark/k6/multi_route.js`       | `benchmark/manifests/workload-route-scale.yaml`              | open-400   | 300s     | 3           | unassigned | planned |             |
| C4-rscale-lc  | route-scale-8         | E-B         | least-connections| `benchmark/k6/multi_route.js`       | `benchmark/manifests/workload-route-scale.yaml`              | open-400   | 300s     | 3           | unassigned | planned |             |

## Campaign 5 — Long-duration stability

Purpose: memory drift, queue buildup, pool behavior over time.

| campaign_id   | scenario             | environment | algorithm   | script                              | manifest                                                 | rate_model | duration | repetitions | owner      | status  | results_dir |
|---------------|----------------------|-------------|-------------|-------------------------------------|----------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C5-1h-uni-pq  | uniform-soak-1h      | E-B         | prequal     | `benchmark/k6/long_duration.js`     | `benchmark/manifests/workload-long-duration.yaml`        | soak       | 3600s    | 1           | unassigned | planned |             |
| C5-1h-uni-rr  | uniform-soak-1h      | E-B         | round-robin | `benchmark/k6/long_duration.js`     | `benchmark/manifests/workload-long-duration.yaml`        | soak       | 3600s    | 1           | unassigned | planned |             |
| C5-6h-het-pq  | heterogeneous-soak-6h| E-B         | prequal     | `benchmark/k6/long_duration.js`     | `benchmark/manifests/workload-heterogeneous.yaml`        | soak       | 21600s   | 1           | unassigned | planned |             |
| C5-6h-het-rr  | heterogeneous-soak-6h| E-B         | round-robin | `benchmark/k6/long_duration.js`     | `benchmark/manifests/workload-heterogeneous.yaml`        | soak       | 21600s   | 1           | unassigned | planned |             |
| C5-24h-pq     | heterogeneous-soak-24h| E-C        | prequal     | `benchmark/k6/long_duration.js`     | `benchmark/manifests/workload-heterogeneous.yaml`        | soak       | 86400s   | 1           | unassigned | planned |             |
| C5-24h-rr     | heterogeneous-soak-24h| E-C        | round-robin | `benchmark/k6/long_duration.js`     | `benchmark/manifests/workload-heterogeneous.yaml`        | soak       | 86400s   | 1           | unassigned | planned |             |

## Campaign 6 — Extreme stress

Purpose: maximum throughput, failure mode, graceful degradation.

| campaign_id     | scenario             | environment | algorithm        | script                            | manifest                                                 | rate_model       | duration | repetitions | owner      | status  | results_dir |
|-----------------|----------------------|-------------|------------------|-----------------------------------|----------------------------------------------------------|------------------|----------|-------------|------------|---------|-------------|
| C6-ovl-uni-pq   | uniform-overload     | E-B         | prequal          | `benchmark/k6/overload.js`        | `benchmark/manifests/workload-uniform.yaml`              | overload         | 300s     | 3           | unassigned | planned |             |
| C6-ovl-uni-rr   | uniform-overload     | E-B         | round-robin      | `benchmark/k6/overload.js`        | `benchmark/manifests/workload-uniform.yaml`              | overload         | 300s     | 3           | unassigned | planned |             |
| C6-ovl-uni-lc   | uniform-overload     | E-B         | least-connections| `benchmark/k6/overload.js`        | `benchmark/manifests/workload-uniform.yaml`              | overload         | 300s     | 3           | unassigned | planned |             |
| C6-burst-het-pq | heterogeneous-burst  | E-B         | prequal          | `benchmark/k6/burst.js`           | `benchmark/manifests/workload-heterogeneous.yaml`        | burst            | 300s     | 3           | unassigned | planned |             |
| C6-burst-het-rr | heterogeneous-burst  | E-B         | round-robin      | `benchmark/k6/burst.js`           | `benchmark/manifests/workload-heterogeneous.yaml`        | burst            | 300s     | 3           | unassigned | planned |             |
| C6-burst-het-lc | heterogeneous-burst  | E-B         | least-connections| `benchmark/k6/burst.js`           | `benchmark/manifests/workload-heterogeneous.yaml`        | burst            | 300s     | 3           | unassigned | planned |             |

## Campaign 7 — Churn and control-plane stability

Purpose: validate ingress-controller behavior during change.

| campaign_id     | scenario           | environment | algorithm        | script                              | manifest                                                 | rate_model | duration | repetitions | owner      | status  | results_dir |
|-----------------|--------------------|-------------|------------------|-------------------------------------|----------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C7-churn-uni-pq | churn-uniform      | E-B         | prequal          | `benchmark/k6/open_loop.js` + `benchmark/scripts/churn.sh` | `benchmark/manifests/workload-uniform.yaml` | open-300   | 600s     | 3           | unassigned | planned |             |
| C7-churn-uni-rr | churn-uniform      | E-B         | round-robin      | `benchmark/k6/open_loop.js` + `benchmark/scripts/churn.sh` | `benchmark/manifests/workload-uniform.yaml` | open-300   | 600s     | 3           | unassigned | planned |             |
| C7-churn-uni-lc | churn-uniform      | E-B         | least-connections| `benchmark/k6/open_loop.js` + `benchmark/scripts/churn.sh` | `benchmark/manifests/workload-uniform.yaml` | open-300   | 600s     | 3           | unassigned | planned |             |

## Campaign 8 — Probe degradation and fault handling

Purpose: validate probe-path resilience. Rebuild the backend image before applying fault manifests: `cd backend && docker build -t prequal-backend:latest .`

| campaign_id        | scenario            | environment | algorithm | script                          | manifest                                                           | rate_model | duration | repetitions | owner      | status  | results_dir |
|--------------------|---------------------|-------------|-----------|---------------------------------|--------------------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C8-timeout-pq      | fault-probe-timeout | E-B         | prequal   | `benchmark/k6/open_loop.js`     | `benchmark/manifests/fault-probe-timeout.yaml`                     | open-200   | 300s     | 3           | unassigned | planned |             |
| C8-500-pq          | fault-probe-500     | E-B         | prequal   | `benchmark/k6/open_loop.js`     | `benchmark/manifests/fault-probe-500.yaml`                         | open-200   | 300s     | 3           | unassigned | planned |             |
| C8-malformed-pq    | fault-probe-malformed| E-B        | prequal   | `benchmark/k6/open_loop.js`     | `benchmark/manifests/fault-probe-malformed.yaml`                   | open-200   | 300s     | 3           | unassigned | planned |             |
| C8-stale-pq        | fault-probe-stale   | E-B         | prequal   | `benchmark/k6/open_loop.js`     | `benchmark/manifests/fault-probe-stale-timestamp.yaml`             | open-200   | 300s     | 3           | unassigned | planned |             |

---

## Configuration sweeps (cross-cutting)

Sweeps listed in `benchmarking.md` section 11 (ProbesPerQuery, BackgroundInterval, PoolMaxAge, PoolMaintenanceInterval, QRIF, PoolReuseLimit, ProbeWorkers). Run the sweep by repeating C2-het-pq with each value in turn; capture the sweep variable in `run-metadata.json` under `controller_env`. Status: `planned` for all.

## Execution order

Follow `benchmarking.md` section 20:

1. Campaign 1 (smoke)
2. Campaign 2 (uniform + heterogeneous open-loop)
3. Campaign 4 (multi-route isolation)
4. Campaign 3 (ramp)
5. Campaign 7 (churn)
6. Campaign 6 (overload/burst)
7. Campaign 5 (long-duration, 1h → 6h → 24h)
8. Campaign 8 (fault injection) — rebuild backend image first (`cd backend && docker build -t prequal-backend:latest .`)

## Campaign 9 — External baseline (NGINX Ingress)

Purpose: proxy-overhead comparison against a mainstream ingress controller for
Claim-Level-B evidence. Same backend image, same k6 script, same work payload.
See `benchmark/public-claim-playbook.md` section 13 and `benchmark/README.md`
"External baseline: NGINX Ingress" for install and fairness notes.

Prerequisites: `bash benchmark/scripts/install_nginx_baseline.sh` and
`kubectl apply -f benchmark/manifests/baseline-nginx-controller.yaml`.

| campaign_id   | scenario      | environment | algorithm | script                          | manifest                                                     | rate_model | duration | repetitions | owner      | status  | results_dir |
|---------------|---------------|-------------|-----------|---------------------------------|--------------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C9-uni-nginx  | uniform       | E-B         | nginx     | `benchmark/k6/open_loop.js`     | `benchmark/manifests/baseline-nginx-workload.yaml`           | open-500   | 300s     | 5           | unassigned | planned |             |
| C9-het-nginx  | heterogeneous | E-B         | nginx     | `benchmark/k6/open_loop.js`     | `benchmark/manifests/baseline-nginx-heterogeneous.yaml`      | open-500   | 300s     | 5           | unassigned | planned |             |

---

## Updating this matrix

- When a run starts: set `status=in-progress`, `owner=<agent-or-engineer>`, and the path under `benchmark/results/` in `results_dir`.
- When a run completes: set `status=done`, confirm `results_dir` points to the archived run, and link the rendered report.
- Do not delete rows. Add new rows for sweep variants; do not rewrite planned rows in place.
