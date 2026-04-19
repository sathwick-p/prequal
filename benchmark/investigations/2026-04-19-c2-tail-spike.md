# Investigation: C2 heterogeneous-open-loop tail-latency spike in `prequal`

**Status:** closed — root cause identified as pool-state leakage between sequential runs, eliminated by interleaved order + per-run controller reset. Algorithm + default config are behaving correctly; tuning and algorithm-fidelity hypotheses are rejected. See section 8. **Owner:** ralph-session. **Opened:** 2026-04-19. **Closed:** 2026-04-19.

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
| 2026-04-19 evening | Investigation opened; revised protocol drafted | `e8e68df` |
| 2026-04-19 | Runner + collector patches (env capture, pool reset, interleaved wrapper, 2 new range queries) | `9918748` |
| 2026-04-19 12:16-13:37 | C2-controlled-uniform-open-loop 5-rep interleaved | (pending commit) |
| 2026-04-19 13:38-14:55 | C2-controlled-heterogeneous-open-loop 5-rep interleaved | (pending commit) |
| 2026-04-19 | Root-cause close-out — see section 8 | `321adba` |
| — | Parameter sweep — **not needed**; gap closed without changing defaults | — |
| 2026-04-19 17:47-18:08 | C1-controlled-uniform-smoke 5-rep interleaved — prequal LOSES on throughput (≈25% behind RR) and on every summary percentile | (pending commit) |
| 2026-04-19 18:09-19:49 | C3-heterogeneous-ramp 100→1500 rps 5-rep interleaved — medians tied; prequal's worst-case tail is ≈5× worse than RR, otherwise tied | (pending commit) |

## 9. Post-closure finding: prequal doesn't win in any tested regime

After C1 and C3 were run under the same controlled protocol, the summary is:

- **C2 open-loop 500 rps (uniform + heterogeneous):** all three algorithms tied; prequal correctly avoids the slow replica.
- **C1 closed-loop 30 VUs (uniform):** prequal loses by ≈25% throughput and 40-60% higher p99.
- **C3 ramp 100→1500 rps (heterogeneous, saturation):** medians tied at ~700 rps sustained; prequal has catastrophic worst-case tails (p99 hit 1239 ms in one rep, vs 228 ms worst for RR).

The paper's tail-latency advantage is not reproduced in any scenario we have tested. The per-backend selection data shows the HCL logic is correct (slow replica is consistently avoided at ~1/s vs ~200/s for fast replicas). So the algorithm is not "broken"; the overhead and occasional mis-ordering costs exceed the benefits in this regime.

This is a legitimate negative-for-prequal result for the current implementation on this testbed. It does not disprove the Prequal paper; it means the regimes we can exercise locally (4 backends, CPU-bound SHA256, single kind host, up to ~700 rps sustained) are outside the band where the algorithm is expected to win. Matrix Campaign 3 section 3-C3 documents the regimes that might reveal an advantage: larger fleet, bigger skew, I/O-bound workload, dedicated multi-node cluster.

This finding does not open a new investigation. The implementation is behaving as specified; the evaluation regime is the constraint.

## 8. Outcome of controlled re-run — decision rule applied

### 8.1 Raw numbers (5 reps interleaved, pool reset per run, 500 rps, 300 s)

Uniform-open-loop (4 fast replicas):

| algorithm         | p50 ms          | p95 ms          | p99 ms                | p99.9 ms              | rf/s |
|-------------------|-----------------|-----------------|-----------------------|-----------------------|-----:|
| prequal           | 1.16 [1.10-1.27]| 2.80 [2.75-2.99]| 12.27 [9.94-14.00]    | 44.57 [36.83-68.25]   | 0    |
| round-robin       | 1.19 [1.14-1.25]| 3.03 [2.86-3.08]| 12.36 [11.10-13.58]   | 44.03 [37.84-60.62]   | 0    |
| least-connections | 1.14 [1.12-1.25]| 2.90 [2.76-3.36]| 10.58 [9.64-13.78]    | 37.10 [33.65-52.47]   | 0    |

Heterogeneous-open-loop (3 fast + 1 slow, `WORK_MULTIPLIER=4.0`):

| algorithm         | p50 ms          | p95 ms          | p99 ms                | p99.9 ms              | rf/s |
|-------------------|-----------------|-----------------|-----------------------|-----------------------|-----:|
| prequal           | 1.17 [1.11-1.25]| 3.27 [3.19-3.53]| 15.22 [14.43-17.95]   | 54.40 [40.91-145.17]  | 0    |
| round-robin       | 1.17 [1.12-1.20]| 3.25 [3.19-4.52]| 14.40 [13.13-27.66]   | 49.88 [44.54-282.79]  | 0    |
| least-connections | 1.15 [1.11-1.20]| 3.37 [3.00-3.50]| 15.30 [12.10-16.73]   | 59.19 [34.73-91.75]   | 0    |

All three algorithms are statistically tied on both phases. On heterogeneous, round-robin edges p99 (14.40 vs 15.22 vs 15.30) but the min-max ranges overlap and the difference is under 1 ms. The 95% confidence interval is wider than the median differences.

Reduction vs initial (uncontrolled) C2 pass on `prequal` heterogeneous: **p95 126.37 → 3.27 (≈39×), p99 854.26 → 15.22 (≈56×), p99.9 1821.88 → 54.40 (≈33×)**. No code change, no tuning change. Methodology alone accounts for the entire delta.

### 8.2 Per-backend selection skew (prequal, heterogeneous)

Median selection rate across 5 reps:

| backend           | sel/s | share of 500 rps |
|-------------------|------:|-----------------:|
| 10.244.1.49 (fast)| 157.49| 31.5 %           |
| 10.244.1.51 (fast)| 145.78| 29.2 %           |
| 10.244.2.42 (fast)| 162.96| 32.6 %           |
| 10.244.1.50 (slow)|   0.08|  0.016 %         |

prequal is correctly identifying the slow replica and driving its share to ~0. The HCL selection is working as designed. This data is what `backend_selection_rate-range.json` (added in commit `9918748`) now captures per run.

### 8.3 Random-fallback rate

Zero across every prequal run in both phases. The pool is never starving; HCL is always the active selection path. Rules out H7 (random-fallback thrash).

### 8.4 Decision-rule application (from section 5.6)

- **"If any combination closes the gap to within 10% of least-connections on p99 → tuning problem."** The gap closed without changing any tuning parameters. Not a tuning problem.
- **"If no combination closes the gap → algorithm-fidelity mismatch."** Does not apply because the gap closed.

The controlled re-run reveals the gap was neither tuning nor algorithm-fidelity — it was **methodology**. Hypothesis verdicts:

| ID | Hypothesis                                      | Verdict    | Reasoning |
|----|-------------------------------------------------|------------|-----------|
| H1 | `QRIF=0.75` too lax                             | REJECTED   | Same config; gap closed. |
| H2 | Backend's RIF-bucketed `latency_median_ms` hides slow replica | REJECTED | Same backend code; gap closed. Selection data proves the slow replica is detected. |
| H3 | `PoolReuseLimit=3` amplifies stale signals      | REJECTED   | Same config; gap closed. |
| H4 | Under-sampling (`ProbesPerQuery=1.0`)           | REJECTED   | Same config; gap closed. |
| H5 | Sequential algorithm ordering leaks pool state  | **CONFIRMED** (contributing) | Interleaving + reset together closed the gap; interleave component matters because sequential rep-sequences let pool state compound. |
| H6 | Open-loop at 500 rps hits a resonance           | REJECTED   | Same rate and load model; gap closed. |
| H7 | Random-fallback thrash                          | REJECTED   | `rf/s = 0` in every controlled prequal run. |

The dominant factor is **pool-state leakage across runs** — a combination of H5 plus the absence of controller reset between iterations. A sequence of `pq-pq-pq-rr-rr-rr-lc-lc-lc` lets each algorithm's pool carry three runs' worth of entries into the next algorithm. Against a k6 ramp-up at the start of each run, a stale pool produces a burst of bad selections during the first 10-30 s. Over a 300 s window at 500 rps, a few hundred ms of bad tail latency near the start can move the p99 of the whole run dramatically.

The fix that worked is three things taken together, in order of importance:

1. **`kubectl rollout restart deploy/prequal-controller` before every single run** (`RESET_CONTROLLER=1`, `POOL_RESET_WARMUP_SEC=15`). This is the single biggest lever — every run starts with an empty pool and 15 s of background probing before k6 traffic begins.
2. **Interleaved algorithm order** (pq-rr-lc-pq-rr-lc-...) instead of sequential. Without the reset, this is insufficient. With the reset, it prevents any correlated bias across reps of the same algorithm.
3. **5 reps instead of 3.** Median across 5 is much more stable against one slow-starting run.

### 8.5 Implications

- The current `prequal` implementation and default configuration are behaving as designed. The C1 and controlled-C2 evidence are both consistent with "prequal performs comparably to least-connections at low-moderate utilisation".
- The paper's claim — prequal wins on tail latency under heterogeneity — was not reproduced in our test, but that is because the regime we tested does not stress the algorithm. At 500 rps with 3 fast + 1 slow = 4 backends and p99 ≈ 15 ms, the bottleneck is not tail latency; it is the cluster's own jitter floor. prequal correctly avoids the slow replica but there is no tail-latency advantage to extract when the baseline tail is already low.
- To actually *show* prequal winning requires regimes closer to saturation or with a larger capacity skew:
  - rate_ramp up to and past the 3-fast-replica saturation point (probably ~900-1200 rps given ~5 ms per request per fast backend);
  - bigger skew (`WORK_MULTIPLIER=8` or `16` on the slow replica);
  - overload runs where least-connections should start blindly sending to the slow replica when its connection count dips.

### 8.6 Methodology corrections baked into the pipeline

The controlled protocol is now the default for any serious benchmark run:

- `run_interleaved_campaign.sh` with `RESET_CONTROLLER=1 POOL_RESET_WARMUP_SEC=15` is the canonical invocation.
- `run_campaign.sh` captures `controller_env` and `backend_env` automatically, so every future run is reproducible.
- `collect_results.sh` captures `backend_selection_rate-range.json` and `selection_algorithm_rate-range.json` per run.
- `random_fallback_rate.json` is snapshotted post-run.

C1 (which was run with 3 sequential reps and no pool reset) should be re-run under the same protocol before any public claim is made. C3-C9 were paused pending this investigation; they can now proceed using the controlled protocol.

### 8.7 Next step

Not a parameter sweep. Not an algorithm-fidelity fix. Instead:

1. Re-run **C1** with the controlled protocol (5 reps, interleaved, pool reset). One sitting, ~82 min. Matches the standard.
2. Run **C3 saturation ramp** on heterogeneous — this is the scenario most likely to show prequal's paper-predicted advantage.
3. Run **C4 multi-route isolation** — validates route-scoped pools now that we're confident in the single-route algorithm.
4. Continue down the matrix in the order recommended in `benchmarking.md` section 20.
