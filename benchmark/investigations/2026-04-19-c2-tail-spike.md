# Investigation: C2 heterogeneous-open-loop tail-latency spike in `prequal`

**Status:** open. **Owner:** ralph-session. **Opened:** 2026-04-19.

This log is the single source of truth for (a) what went wrong in Campaign 2, (b) why the initial interpretation was weaker than it looked, (c) the methodological gaps we found in the run plan, and (d) the revised protocol and decision rule we're using to re-run.

Any engineer picking this up cold should be able to read this doc top to bottom and understand what to do next without any additional context.

---

## 1. What happened

We ran Campaign 2 (heterogeneous-open-loop) on 2026-04-19 in kind-local (E-A). Configuration:

- Workload: `benchmark/manifests/workload-heterogeneous.yaml` — 3 fast backends (`WORK_MULTIPLIER=1.0`) + 1 slow backend (`WORK_MULTIPLIER=4.0`) behind one service.
- Script: `benchmark/k6/open_loop.js`, constant-arrival-rate 500 rps, 300s per run.
- 3 reps per algorithm, 3 algorithms (`prequal`, `round-robin`, `least-connections`), sequential order: pq × 3, rr × 3, lc × 3.
- Continuous Prometheus scrape + kubectl-top TSV sampler per run.

Median [min-max] across 3 reps:

| algorithm         | rps | p95 ms | p99 ms | p99.9 ms | err |
|-------------------|----:|-------:|-------:|---------:|----:|
| prequal           | 496 | **126.37** [17.16-196.60] | **854.26** [327.72-854.30] | **1821.88** [1566.97-3279.88] | 0   |
| round-robin       | 498 | 57.98 [51.68-171.48] | 284.04 [277.78-668.80] | 1801.82 [1074.55-2600.03] | 0   |
| least-connections | 499 | 34.46 [25.24-94.55] | 165.03 [147.11-474.04] | 561.50 [528.44-1977.87] | 0   |

Under our current implementation and default configuration, `prequal` is the **worst** algorithm on every tail percentile. The paper predicts the opposite. Raw artifacts:
- `benchmark/results/2026-04-19T09-{30-44,35-51,40-59}Z-heterogeneous-open-loop-prequal/`
- `benchmark/results/2026-04-19T09-{46-11,51-17,56-25}Z-heterogeneous-open-loop-round-robin/`
- `benchmark/results/2026-04-19T10-{01-37,06-43,11-49}Z-heterogeneous-open-loop-least-connections/`
- `benchmark/results/aggregated/2026-04-19-C2-heterogeneous-open-loop.json`
- `benchmark/results/screenshots/2026-04-19-C2-heterogeneous/` (6 PNGs)

## 2. What the data actually proves (and doesn't)

### What the data does prove

- Under this specific scenario, config, and run plan, `prequal` has materially worse tail latency than both baselines.
- Throughput is effectively tied at ~500 rps (matching the open-loop target), and error rate is zero. The dashboards back this up visually.
- The failure mode is **tail-spike at low utilisation**, not proxy overload. p50 stays low (1-2 ms) for all three algorithms; only the tail diverges.
- This rules out "prequal is saturating": it's not a capacity failure, it's a bad-selection failure. Somewhere the HCL selection is landing on a backend that's going to be slow, repeatedly enough to dominate p99+.

### What the data does not prove

- We do **not** know whether the loss is heterogeneity-specific, open-loop-specific, tuning-specific, or probe-signal-specific. C1 (uniform, closed-loop, 60 s) and C2 (heterogeneous, open-loop, 300 s) differ on every axis, so "the ranking flipped between C1 and C2" is a weak inference — any of those four changes could independently cause the flip.
- We do not have per-backend selection distribution in our collected evidence. We can see "something bad happened at the tail" but we cannot see "how often `prequal` picked the slow backend" because `collect_results.sh` does not capture `rate(prequal_proxy_backend_selection_total)` as a range query.
- We do not have `prequal_selection_algorithm_total{algorithm="random_fallback"}` captured per run. If the pool is starving and falling back to random, that would produce tail spikes on any algorithm but especially `prequal` (because the fallback defeats HCL entirely). We cannot confirm or rule that out.
- The run-metadata `controller_env` and `backend_env` fields are `{}` — we did not record the `PREQUAL_*` env knobs in effect when the data was taken, so the run is not strictly reproducible.

### Correction to the 2026-04-19 first-pass framing

The earlier summary framed this as "prequal's lead inverted between C1 and C2". That framing was over-stated. Only the ranking inverted. The absolute comparison is apples-to-oranges because the two campaigns differ in workload shape, load model, and duration simultaneously. The honest description is: **C1 uniform gave prequal the lowest tail median; C2 heterogeneous gave prequal the highest tail median. We do not know which of the four independent variable changes caused the flip.**

## 3. Competing hypotheses

Ordered rough-probability-first based on the data we have.

### H1 — `QRIF` too lax

`QRIF=0.75` with a 4-backend pool means 3 of 4 entries qualify as "cold". HCL within the cold partition picks the lowest-latency probe, which is exactly the slow backend when it has low RIF. This is an **algorithm-by-configuration** failure, not an implementation bug.

### H2 — Backend's RIF-bucketed `latency_median_ms` hides the slow replica

The Rust backend reports `latency_median_ms` from the bucket matching current RIF (`backend/src/main.rs` — `rif_bucket` + `probe_handler`). When the slow backend's RIF is low, the reported latency is low, so probes say "this backend is fast." By the time the backend accumulates a queue, many requests have already been dispatched. This is an **algorithm-fidelity** mismatch vs the paper — the paper uses unconditioned latency.

### H3 — Pool reuse amplifies stale signals

`PoolReuseLimit=3` + `MaxProbeAge=2s` means a single probe result can be reused up to 3 times across a 2-second window. If that probe was wrong (H2), the error compounds.

### H4 — Under-sampling vs target arrival rate

`ProbesPerQuery=1.0` + `BackgroundInterval=100ms` may be under-sampling the fast backends relative to the slow one, especially when selection itself is skewed toward the slow one (H1 feedback loop).

### H5 — Sequential algorithm ordering leaks pool state

Running pq × 3, rr × 3, lc × 3 in sequence means each algorithm's later reps inherit pool warmth from the earlier reps. That alone does not explain why prequal's first rep already had p95=196 ms, but it adds noise. This is a **methodology** issue.

### H6 — Open-loop at 500 rps hits a resonance

Lower on the list. We haven't run C2-uniform-open-loop, so we can't rule out a generic open-loop issue independent of heterogeneity.

### H7 — Random-fallback thrash

If the pool starves and falls back to `random_fallback`, prequal degenerates to random selection — which picks the slow backend 1/4 of the time. Not captured in the current evidence. Diagnostic metric exists (`prequal_selection_algorithm_total{algorithm="random_fallback"}`) but we didn't snapshot it per run.

## 4. Methodology gaps in the run plan (to be fixed before re-run)

| Gap | Evidence-quality cost | Fix |
|-----|-----------------------|-----|
| `controller_env`/`backend_env` empty in run-metadata | Results not reproducible; can't link a tuning sweep to its config | Capture `PREQUAL_*` and `WORK_*` env from live pods in run_campaign.sh |
| No pool reset between runs | Pool state leaks across algorithms; early reps differ from late reps | Optional `RESET_CONTROLLER=1` path that `rollout restart`s the controller and warms up 15 s before k6 |
| Sequential algorithm order | Can't distinguish warmup from algorithm effect | Interleave: pq-rr-lc-pq-rr-lc-... via `run_interleaved_campaign.sh` |
| No per-backend selection rate captured | Can't prove which backend prequal over-selected | Add `rate(prequal_proxy_backend_selection_total)` range query to `collect_results.sh` |
| No selection-algorithm rate captured | Can't detect `random_fallback` thrash | Add `rate(prequal_selection_algorithm_total)` range query |
| 3 reps | Statistically thin; one noisy rep dominates | 5 reps |

## 5. Revised protocol for C2 re-run

1. Land the four infrastructure changes (capture env, pool reset, interleaved runner, two new Prometheus range queries).
2. Run **C2-uniform-open-loop** (new, previously not run): same open_loop.js, 500 rps, 300 s, uniform workload, 5 reps, interleaved, controller reset between every run.
3. Run **C2-heterogeneous-open-loop** again with the same controls: 5 reps, interleaved, controller reset between every run.
4. Compare the rankings:
   - If prequal loses in both → open-loop-specific or tuning-specific (not heterogeneity-specific).
   - If prequal wins uniform-open-loop but loses heterogeneous-open-loop → heterogeneity-specific (the slow-replica detection issue).
   - If prequal loses both with random-fallback rate > 0 → starvation; fix pool warmup / ProbesPerQuery.
5. Only after step 4 finishes, run a narrow **prequal-only parameter sweep** on whichever scenario shows the gap. Starting order (highest leverage first):
   1. `QRIF ∈ {0.25, 0.5}` (addresses H1 directly).
   2. `PoolReuseLimit=1` (addresses H3).
   3. `MaxProbeAge ∈ {250 ms, 500 ms}` (addresses H2 by forcing fresher probes).
   4. `ProbesPerQuery ∈ {2.0, 3.0}` + `BackgroundInterval=50 ms` (addresses H4).
6. Decision rule:
   - If any combination closes the gap to within 10 % of least-connections on p99 → tuning problem; update defaults; re-run broader matrix.
   - If no combination closes the gap → algorithm-fidelity mismatch (H2 is root cause); fix the backend to report unconditioned latency, re-verify.

## 6. Pause on downstream campaigns

C3 (ramp), C4 (multi-route), C5 (long-duration), C6 (overload), C7 (churn), C8 (fault), C9 (NGINX baseline) are **paused** until steps 2-4 above complete. Running those on a mis-tuned prequal produces evidence that has to be discarded and re-run later. We are not throwing away C1 or the current C2 — both are valid preliminary evidence, clearly marked as such.

## 7. Status log

| Date (UTC) | Event | Commit |
|------------|-------|--------|
| 2026-04-19 09:05-09:14 | C1 uniform-smoke 3-rep re-run — prequal wins tail medians | `62debb1` |
| 2026-04-19 09:30-10:16 | C2 heterogeneous-open-loop 3-rep — prequal loses tail medians | `e5e1232` |
| 2026-04-19 evening | Investigation opened; revised protocol drafted | (this file) |
| TBD | Runner + collector patches (capture env, pool reset, interleaved, new range queries) | TBD |
| TBD | C2-uniform-open-loop 5-rep interleaved | TBD |
| TBD | C2-heterogeneous-open-loop 5-rep interleaved | TBD |
| TBD | Parameter sweep (if gap persists) | TBD |
| TBD | Root-cause close-out | TBD |

Update this table as each step lands.
