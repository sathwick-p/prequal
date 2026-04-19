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

| campaign_id | scenario           | environment | algorithm        | script                                  | manifest                                           | rate_model   | duration | repetitions | owner        | status       | results_dir |
|-------------|--------------------|-------------|------------------|-----------------------------------------|----------------------------------------------------|--------------|----------|-------------|--------------|--------------|-------------|
| C1-smoke-pq | uniform-smoke      | E-A         | prequal          | `benchmark/k6/steady_state.js`          | `benchmark/manifests/workload-uniform.yaml`        | closed-loop  | 60s      | 5           | ralph-session| done         | [aggregate](results/aggregated/2026-04-19-C1-controlled-uniform-smoke.json) |
| C1-smoke-rr | uniform-smoke      | E-A         | round-robin      | `benchmark/k6/steady_state.js`          | `benchmark/manifests/workload-uniform.yaml`        | closed-loop  | 60s      | 5           | ralph-session| done         | [aggregate](results/aggregated/2026-04-19-C1-controlled-uniform-smoke.json) |
| C1-smoke-lc | uniform-smoke      | E-A         | least-connections| `benchmark/k6/steady_state.js`          | `benchmark/manifests/workload-uniform.yaml`        | closed-loop  | 60s      | 5           | ralph-session| done         | [aggregate](results/aggregated/2026-04-19-C1-controlled-uniform-smoke.json) |

### Campaign 1 controlled results (2026-04-19, E-A kind-local, 5 reps interleaved, pool reset between every run)

Closed-loop 30 VUs, 60 s steady-state, `WORK_ITERATIONS=1000`, uniform workload (4 backend replicas, `WORK_MULTIPLIER=1.0`). Median [min-max] across 5 runs per algorithm, controller reset + 15 s warmup before every run. All latencies in ms.

| algorithm         | reps | rps               | avg ms          | p50 ms          | p95 ms               | p99 ms                 | p99.9 ms                  | rf/s |
|-------------------|-----:|-------------------|-----------------|-----------------|----------------------|------------------------|---------------------------|-----:|
| prequal           | 5    | 4162 [3566-5108]  | 7.15 [5.81-8.35]| 5.55 [4.96-6.72]| 15.31 [11.77-18.31]  | 29.86 [19.10-38.32]    | 81.72 [40.87-139.35]      | 0    |
| round-robin       | 5    | 5366 [4217-6046]  | 5.54 [4.91-7.05]| 4.56 [4.14-5.38]| 11.67 [9.90-16.69]   | 20.22 [16.74-32.07]    | 49.01 [39.87-74.56]       | 0    |
| least-connections | 5    | 4965 [4095-6012]  | 5.98 [4.94-7.26]| 4.87 [4.16-5.73]| 12.95 [9.78-16.53]   | 23.48 [16.97-30.18]    | 48.15 [38.43-74.14]       | 0    |

Screenshots: [`results/screenshots/2026-04-19-C1-controlled/`](results/screenshots/2026-04-19-C1-controlled/). Aggregate: [`results/aggregated/2026-04-19-C1-controlled-uniform-smoke.json`](results/aggregated/2026-04-19-C1-controlled-uniform-smoke.json).

Observations — **prequal does not win here**.

- **Throughput:** prequal sustains 4162 rps vs round-robin 5366 (≈29% faster) and least-connections 4965 (≈19% faster). Prequal is the slowest on median throughput by a meaningful margin, in every rep.
- **Tail latency:** prequal is worse on p95 (15.3 vs 11.7 vs 13.0 ms), worse on p99 (29.9 vs 20.2 vs 23.5 ms), and worse on p99.9 (81.7 vs 49.0 vs 48.1 ms). The worst prequal p99.9 rep hits 139 ms; round-robin's worst is 74 ms.
- `random_fallback_rate = 0` across every prequal run — pool is never starving, HCL is always active.
- Per-backend selection for prequal on uniform: 4 backends at ~600-660/s each (total ≈ 2500/s of prequal's 4162 rps including re-probes). The load is evenly distributed — so the throughput gap is not caused by skewed selection. It's caused by per-request overhead in the prequal path itself.

This supersedes the earlier 2026-04-19 3-rep C1 pass (sequential, no pool reset) that reported prequal winning on tail medians. Under the controlled protocol prequal is strictly worse on every summary percentile. The earlier 3-rep pass is preserved under `results/2026-04-19T09-*-uniform-smoke-*/` for audit trail.

**Candidate explanations for the prequal throughput gap:**

1. Probe path costs more than the paper's model. `ProbesPerQuery=1.0` fires one async probe per request; at 4000 rps that's 4000 probes/sec with 100 ms timeouts, which saturates a single Go HTTP client's worker pool (`ProbeWorkers=16`, `TriggerQueueSize=1024`). The probes are async but they compete for controller CPU and the network budget.
2. HCL selection overhead. On every request the controller sorts the pool by RIF, picks the QRIF quantile, then picks min-latency among cold entries. With `PoolMaxSize=16`, that's 16-entry operations per request. On a CPU-bound SHA256 backend, this adds non-trivial per-request latency.
3. Request-path lock contention. `pool.Select` takes a mutex; at 4000 rps the lock is held for microseconds, and contention can show up on p99+.

Not a bug, but a real implementation cost that the paper's model does not include. See investigation log section 8.5 for context.

## Campaign 2 — Algorithm comparison under controlled load

Purpose: main comparison set (uniform + heterogeneous, open-loop).

| campaign_id    | scenario                 | environment | algorithm        | script                               | manifest                                                 | rate_model | duration | repetitions | owner      | status  | results_dir |
|----------------|--------------------------|-------------|------------------|--------------------------------------|----------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C2-uni-pq      | uniform-open-loop        | E-A         | prequal          | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-uniform.yaml`              | open-500   | 300s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C2-controlled-uniform-open-loop.json) |
| C2-uni-rr      | uniform-open-loop        | E-A         | round-robin      | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-uniform.yaml`              | open-500   | 300s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C2-controlled-uniform-open-loop.json) |
| C2-uni-lc      | uniform-open-loop        | E-A         | least-connections| `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-uniform.yaml`              | open-500   | 300s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C2-controlled-uniform-open-loop.json) |
| C2-het-pq      | heterogeneous-open-loop  | E-A         | prequal          | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-heterogeneous.yaml`        | open-500   | 300s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C2-controlled-heterogeneous-open-loop.json) |
| C2-het-rr      | heterogeneous-open-loop  | E-A         | round-robin      | `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-heterogeneous.yaml`        | open-500   | 300s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C2-controlled-heterogeneous-open-loop.json) |
| C2-het-lc      | heterogeneous-open-loop  | E-A         | least-connections| `benchmark/k6/open_loop.js`          | `benchmark/manifests/workload-heterogeneous.yaml`        | open-500   | 300s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C2-controlled-heterogeneous-open-loop.json) |

### Campaign 2 controlled re-run results (2026-04-19, E-A kind-local, 5 reps per algorithm, interleaved, pool reset between every run)

Protocol (see [`investigations/2026-04-19-c2-tail-spike.md`](investigations/2026-04-19-c2-tail-spike.md)):

- Interleaved order (pq-rr-lc-pq-rr-lc-...) instead of sequential, so no algorithm inherits pool warmth from prior runs of the same algorithm.
- `kubectl rollout restart deploy/prequal-controller` between every run, followed by 15 s warmup.
- `controller_env` and `backend_env` captured per run in `run-metadata.json`.
- Prometheus range queries for `backend_selection_rate` and `selection_algorithm_rate` collected per run.
- Same traffic profile as the initial pass: open-loop 500 rps, 300 s, `WORK_ITERATIONS=1000`, same two workload manifests.

**Headline — the gap closed on both phases.** prequal is now statistically tied with both baselines, and selection skew data confirms prequal is correctly avoiding the slow replica.

#### Phase 1: uniform-open-loop (3 fast backends, 4 replicas, `WORK_MULTIPLIER=1.0`)

| algorithm         | reps | rps          | avg ms           | p50 ms          | p95 ms          | p99 ms                | p99.9 ms              | random_fallback/s |
|-------------------|-----:|--------------|------------------|-----------------|-----------------|-----------------------|-----------------------|------------------:|
| prequal           | 5    | 500 [500-500]| 1.65 [1.62-1.66] | 1.16 [1.10-1.27]| 2.80 [2.75-2.99]| 12.27 [9.94-14.00]    | 44.57 [36.83-68.25]   | 0.00              |
| round-robin       | 5    | 500 [500-500]| 1.64 [1.59-1.77] | 1.19 [1.14-1.25]| 3.03 [2.86-3.08]| 12.36 [11.10-13.58]   | 44.03 [37.84-60.62]   | 0.00              |
| least-connections | 5    | 500 [500-500]| 1.61 [1.52-1.65] | 1.14 [1.12-1.25]| 2.90 [2.76-3.36]| 10.58 [9.64-13.78]    | 37.10 [33.65-52.47]   | 0.00              |

Screenshots: [`results/screenshots/2026-04-19-C2-controlled-uniform/`](results/screenshots/2026-04-19-C2-controlled-uniform/). Aggregated: [`results/aggregated/2026-04-19-C2-controlled-uniform-open-loop.json`](results/aggregated/2026-04-19-C2-controlled-uniform-open-loop.json).

#### Phase 2: heterogeneous-open-loop (3 fast + 1 slow, `WORK_MULTIPLIER=4.0` on slow)

| algorithm         | reps | rps          | avg ms           | p50 ms          | p95 ms          | p99 ms                | p99.9 ms              | random_fallback/s |
|-------------------|-----:|--------------|------------------|-----------------|-----------------|-----------------------|-----------------------|------------------:|
| prequal           | 5    | 500 [500-500]| 1.85 [1.64-2.09] | 1.17 [1.11-1.25]| 3.27 [3.19-3.53]| 15.22 [14.43-17.95]   | 54.40 [40.91-145.17]  | 0.00              |
| round-robin       | 5    | 500 [500-500]| 1.75 [1.71-2.72] | 1.17 [1.12-1.20]| 3.25 [3.19-4.52]| 14.40 [13.13-27.66]   | 49.88 [44.54-282.79]  | 0.00              |
| least-connections | 5    | 500 [500-500]| 1.79 [1.62-1.90] | 1.15 [1.11-1.20]| 3.37 [3.00-3.50]| 15.30 [12.10-16.73]   | 59.19 [34.73-91.75]   | 0.00              |

Screenshots: [`results/screenshots/2026-04-19-C2-controlled-heterogeneous/`](results/screenshots/2026-04-19-C2-controlled-heterogeneous/). Aggregated: [`results/aggregated/2026-04-19-C2-controlled-heterogeneous-open-loop.json`](results/aggregated/2026-04-19-C2-controlled-heterogeneous-open-loop.json).

#### Per-backend selection skew — prequal correctly avoids the slow replica

Median selection rate across the 5 prequal reps on heterogeneous (backend IPs; 3 fast + 1 slow deployed at phase-2 start):

| backend IP        | median selections/s | range            | interpretation                       |
|-------------------|--------------------:|------------------|--------------------------------------|
| 10.244.1.49:8080  | 157.49              | [149.64, 171.32] | fast — receives traffic              |
| 10.244.1.51:8080  | 145.78              | [121.88, 170.13] | fast — receives traffic              |
| 10.244.2.42:8080  | 162.96              | [125.75, 167.47] | fast — receives traffic              |
| **10.244.1.50:8080** | **0.08**         | [0.03, 0.16]     | **slow — effectively blackholed**    |

prequal routes ~99.95% of requests to the three fast replicas. The old backend IPs (10.244.1.20/21/2.22/23 from phase-1 workload) have median 0/s, as expected. This is the behavior the algorithm is supposed to produce.

#### Comparison — initial C2 pass (sequential, no pool reset) vs controlled re-run

The initial 2026-04-19 C2 pass recorded `prequal` p95=126.37 ms, p99=854.26 ms, p99.9=1821.88 ms on heterogeneous. After methodology fixes the same algorithm produces p95=3.27 ms, p99=15.22 ms, p99.9=54.40 ms. Reduction factors: **~40× p95, ~56× p99, ~34× p99.9**. No code change, no tuning change. The failure was pool-state leakage between sequential runs + absence of reset. That writes off the initial C2 tail-spike result as a methodology artefact, not an algorithm defect. The initial pass is preserved below for audit trail.

### Campaign 2 heterogeneous-open-loop results (2026-04-19, E-A kind-local, 3 reps per algorithm) — superseded by controlled re-run above

Open-loop constant-arrival-rate, 500 rps, 300s per run, WORK_ITERATIONS=1000. Backend topology: 3 fast replicas (`WORK_MULTIPLIER=1.0`) + 1 slow replica (`WORK_MULTIPLIER=4.0`) behind a single service. Each cell shows `median [min-max]` across 3 runs; all latencies in ms.

| algorithm         | reps | rps           | avg ms             | p50 ms          | p95 ms                | p99 ms                 | p99.9 ms                   | err rate |
|-------------------|-----:|---------------|--------------------|-----------------|-----------------------|------------------------|----------------------------|---------:|
| prequal           | 3    | 496 [491-498] | 40.15 [14.13-41.71]| 1.80 [1.19-2.38]| **126.37 [17.16-196.60]** | **854.26 [327.72-854.30]** | **1821.88 [1566.97-3279.88]** | 0        |
| round-robin       | 3    | 498 [496-499] | 17.90 [15.32-36.46]| 1.95 [1.95-2.14]| 57.98 [51.68-171.48]  | 284.04 [277.78-668.80] | 1801.82 [1074.55-2600.03]  | 0        |
| least-connections | 3    | 499 [498-499] | 8.92 [7.73-23.92]  | 1.57 [1.57-1.85]| **34.46 [25.24-94.55]**   | **165.03 [147.11-474.04]** | **561.50 [528.44-1977.87]**   | 0        |

Screenshots (full 47-minute campaign window across all three algorithm phases): [`results/screenshots/2026-04-19-C2-heterogeneous/`](results/screenshots/2026-04-19-C2-heterogeneous/)

**Finding — this is the opposite of the Prequal paper's prediction.**

Under the tested configuration, `least-connections` beats `round-robin` beats `prequal` on every tail-latency percentile. Throughput is effectively tied (~500 rps, matching the target arrival rate). The request-overview screenshot makes the effect visually obvious: prequal's 15-min window (09:30–09:45) shows p95 spikes past 500 ms, round-robin's (09:45–10:00) shows spikes to ~300 ms, and least-connections' (10:00–10:15) is mostly flat under 50 ms.

This does not invalidate the Prequal algorithm; it is an honest observation that our current implementation + configuration defaults do not reproduce the paper's result on this workload. Candidate explanations, in rough order of likelihood:

1. **QRIF too lax.** `QRIF=0.75` lets HCL treat the slow replica as "cold" as long as its RIF is in the lower 75% of the pool. With only 4 backends and aggressive probing, the slow replica can appear cold for long stretches, get selected, and then pile up requests.
2. **RIF-conditioned latency bucket hides the slow replica.** The Rust backend reports `latency_median_ms` from the bucket matching current RIF. A fresh probe to the slow replica at low RIF returns a low latency — the probe tells the controller "this backend is fast" until the slow replica accumulates a queue. By the time the probe reveals the truth, many requests are already dispatched.
3. **Pool reuse window.** `PoolReuseLimit=3` with `MaxProbeAge=2s` lets a single slow-replica probe be reused up to 3 times across 2 seconds, amplifying (2).
4. **Probes-per-query rate vs observation window.** `ProbesPerQuery=1.0` + `BackgroundInterval=100ms` may be under-sampling the fast backends relative to the slow one, especially when selection itself skews toward the slow one.
5. **Small-pool edge.** `PoolMaxSize=16` with only 4 backends and probe reuse gives at most ~4 entries per unique backend. HCL picks the lowest-latency entry among a small set; variance is high.

Next step is a narrow parameter sweep on C2-het-pq only — vary `QRIF ∈ {0.5, 0.75, 0.9}`, `MaxProbeAge ∈ {500ms, 2s}`, `PoolReuseLimit ∈ {1, 3}` — and see which combination (if any) closes the gap. If none does, the likely culprit is the backend's RIF-bucketed `latency_median_ms` semantics, which would be an algorithm-fidelity issue, not a tuning one.

Until the investigation completes, downstream campaigns (C3 ramp, C4 multi-route isolation, C5 long-duration, C6 overload, C7 churn) should treat "prequal" as running under its current, underperforming default configuration. Those campaigns still produce useful evidence — they test stability and ingress behavior, not just the algorithm comparison.

## Campaign 3 — Saturation and tail-latency ramp

Purpose: where each algorithm breaks down.

| campaign_id   | scenario           | environment | algorithm        | script                           | manifest                                                 | rate_model | duration | repetitions | owner      | status  | results_dir |
|---------------|--------------------|-------------|------------------|----------------------------------|----------------------------------------------------------|------------|----------|-------------|------------|---------|-------------|
| C3-ramp-pq    | heterogeneous-ramp | E-A         | prequal          | `benchmark/k6/rate_ramp.js`      | `benchmark/manifests/workload-heterogeneous.yaml`        | ramp 100→1500 | 370s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C3-heterogeneous-ramp.json) |
| C3-ramp-rr    | heterogeneous-ramp | E-A         | round-robin      | `benchmark/k6/rate_ramp.js`      | `benchmark/manifests/workload-heterogeneous.yaml`        | ramp 100→1500 | 370s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C3-heterogeneous-ramp.json) |
| C3-ramp-lc    | heterogeneous-ramp | E-A         | least-connections| `benchmark/k6/rate_ramp.js`      | `benchmark/manifests/workload-heterogeneous.yaml`        | ramp 100→1500 | 370s     | 5           | ralph-session| done    | [aggregate](results/aggregated/2026-04-19-C3-heterogeneous-ramp.json) |

### Campaign 3 controlled results (2026-04-19, E-A kind-local, 5 reps interleaved, pool reset between every run)

Ramping-arrival-rate: `START_RATE=100`, `STEP_RATE=200`, `STEPS=8`, `STEP_DURATION=45s` — ramps through 100, 300, 500, 700, 900, 1100, 1300, 1500 rps over 370 s. Heterogeneous workload (3 fast + 1 slow, `WORK_MULTIPLIER=4.0`). Controller reset + 15 s warmup before every run. Median [min-max] across 5 runs per algorithm; latencies in ms.

| algorithm         | reps | rps (sustained)   | avg ms            | p50 ms          | p95 ms                | p99 ms                    | p99.9 ms                      | rf/s |
|-------------------|-----:|-------------------|-------------------|-----------------|-----------------------|---------------------------|-------------------------------|-----:|
| prequal           | 5    | 696 [683-696]     | 2.65 [1.81-42.31] | 0.90 [0.85-1.27]| 6.23 [4.32-**154.56**]| 39.30 [18.62-**1239.07**] | 150.23 [65.37-**2010.66**]    | 0    |
| round-robin       | 5    | 696 [693-696]     | 2.59 [2.17-9.37]  | 1.10 [1.01-1.11]| 6.36 [4.75-14.09]     | 34.78 [28.22-228.20]      | 132.96 [102.80-1058.88]       | 0    |
| least-connections | 5    | 695 [691-696]     | 3.17 [1.96-21.48] | 1.05 [0.91-1.21]| 7.91 [4.82-37.76]     | 45.62 [24.58-498.25]      | 240.71 [63.02-1905.43]        | 0    |

Screenshots: [`results/screenshots/2026-04-19-C3-ramp/`](results/screenshots/2026-04-19-C3-ramp/). Aggregate: [`results/aggregated/2026-04-19-C3-heterogeneous-ramp.json`](results/aggregated/2026-04-19-C3-heterogeneous-ramp.json).

Observations — **prequal does not show a paper-predicted advantage; its worst-case tail is dramatically worse than baselines**.

- **Sustained throughput is capped at ~696 rps** (well below the 800-rps average ramp target), identical for all three algorithms. At `WORK_ITERATIONS=1000` per request the SHA256 work on 3 fast backends saturates around 700 rps; the top ramp steps (900–1500 rps) degrade into steady-state-at-saturation rather than achieving the target.
- **Medians are tied** (prequal p99 = 39.3, round-robin 34.8, least-connections 45.6 — within the same order of magnitude).
- **Worst-case tail is where prequal loses badly.** Prequal's worst rep hit p95 = 154.56 ms, p99 = **1239 ms**, p99.9 = 2010 ms. Round-robin's worst rep topped out at p99 = 228 ms. Least-connections' worst was p99 = 498 ms. Under saturation with pool churn, prequal's HCL selection is making occasional catastrophic-tail choices that the baselines do not make.
- **Per-backend selection confirms HCL is working correctly at steady-state.** Across the 5 prequal reps, the three fast backends get 188-212 sel/s each (~28% each); the slow backend gets 1.11 sel/s (<0.2%). The algorithm IS identifying and avoiding the slow replica. The tail regressions are not caused by wrong selections on average — they are caused by rare but catastrophic selections at transient pool staleness.
- `random_fallback_rate = 0` throughout. Pool never starves.
- Dashboard visual: 15 clean ramp sawtooths (one per run) with one prominent p95 spike to 1.25 s around 18:38 UTC (prequal's worst rep). Otherwise flat.

**This does not invalidate the Prequal algorithm**, but it does mean our implementation does not reproduce the paper's tail-latency advantage in the 4-backend, CPU-bound, kind-local regime. Candidate regimes that might reveal it:

- Larger backend fleet (16+). The paper's scenario had ~100 backends, where HCL's sample diversity dominates.
- Larger capacity skew (`WORK_MULTIPLIER=8` or `16`). At 4× the fast/slow gap is already small relative to request variance.
- I/O-bound workload instead of SHA256-CPU. The paper assumes probed latency reflects realistic service time; SHA256 is tightly CPU-bound.
- Dedicated multi-node cluster (E-B) instead of single-host kind, which has shared noisy neighbours.

**Not recommended next:** parameter tuning on the current regime. The median numbers are tied within noise; there is no "gap to close" that a QRIF/ReuseLimit sweep would help. The constraint is the regime, not the tuning.

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
