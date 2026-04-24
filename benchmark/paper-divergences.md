# How this implementation diverges from the Prequal paper

**Paper:** Wydrowski, Kleinberg, Rumble, Archer. *Load is not what you should balance: Introducing Prequal.* [NSDI '24](https://www.usenix.org/system/files/nsdi24-wydrowski.pdf).

This document exists so that anyone reading the repo can tell exactly where the code follows the paper and where it doesn't — and why. The implementation is faithful on the core algorithm (HCL selection, two-signal probing, bounded pool, alternating eviction) but diverges in several parameters and some mechanics. Each divergence is listed with the paper's text, the repo's behaviour, the reason, and the observable consequences.

The paper's implementation lives inside Google's Stubby RPC framework. This repo is a Go Kubernetes ingress controller. They share an algorithm, not an environment, so a few of the differences fall out of the difference in context rather than from a design choice.

---

## At-a-glance comparison

| Item | Paper | Repo | Verdict |
|---|---|---|---|
| HCL selection rule | cold → min latency; all-hot → min RIF | `pool.Select` same | **faithful** |
| Hot/cold split at RIF quantile | yes | yes | **faithful** |
| Probe pool size `m` | 16 ("16 suffices") | `PoolMaxSize=16` | **faithful** |
| Probe age cap | 1 s | `PoolMaxAge=1s` | **faithful** |
| Random fallback when pool < 2 | "if the pool is empty … uniformly random" | `pool.go` fallback path | **faithful** |
| Async probing off critical path | yes | yes | **faithful** |
| RIF-conditioned latency median on backend | "latency values at (or near) the current RIF" | 5-bucket scheme in Rust backend | **faithful** |
| Eviction policy (oldest vs worst-load, alternating) | yes | `RemoveWorst` alternating | **faithful** |
| Two signals only (RIF + latency) | yes | yes | **faithful** |
| `Q_RIF` default | `2^(-0.25) ≈ 0.84` | `0.75` | minor, in recommended `[0.6, 0.9]` band |
| Probes per query | 3 (testbed), 5 (YouTube) | `1.0` | **major, likely consequential** |
| `b_reuse` reuse limit | derived per Eq. 1 | hardcoded `3` | major by design, minor in practice for this testbed |
| `r_remove` removal rate | per-query, 1/query | on a 100 ms maintenance tick | medium, behavioural proxy |
| Probe-target sampling | "uniformly at random without replacement" | `rand.Intn(...)` single-pick, with replacement | **latent bug** if probes/query ≥ 2 |
| Workload shape | CPU-bound hash, variable iterations, antagonist-driven overload | IO-bound `tokio::sleep`, static capacity skew | **major, affects which regime wins** |
| Sync mode | present, used in YouTube | not implemented | deliberate, out-of-scope |

---

## 1. `Q_RIF = 0.75` instead of the paper's `~0.84`

**Paper (§5 baseline):** *"Unless otherwise specified, we set `Q_RIF = 2^(−0.25) ≈ 0.84`."* §5.2 recommends `Q_RIF ∈ [0.6, 0.9]`.

**Repo:** `loadbalancer/config.go` → `QRIF: 0.75`.

**Why.** `0.75` is a round number in the middle of the paper's recommended band. No deliberate empirical reason to prefer it over `0.84`; it was set once and never swept.

**Consequence.** Expected to be small. The paper's own Figure 9 shows the latency quantiles move smoothly across `Q_RIF ∈ [0.35, 0.99]` on a 50-fast/50-slow 2× skew workload; `0.75` and `0.84` fall in the flat part of that curve. Worth a parameter sweep on C2 to confirm on this testbed.

**Fix.** Single env var: `PREQUAL_QRIF=0.84`. Track it as a campaign row in the matrix.

---

## 2. `ProbesPerQuery = 1.0` instead of the paper's `3` (or YouTube's `5`)

**Paper (§5 baseline):** *"We use 3 probes per query as our baseline probe rate to stay safely away from probe rates low enough to impact performance."* §3 reports YouTube uses *5 probes per query*.

**Paper (§5.3) explicitly warns against going lower:** *"At probing rates of `1/√2 ×` and `1/2 ×` [the query rate], the tail RIF distributions jump visibly, and this change is echoed by both latency quantiles. Anecdotally, we have observed this phenomenon across many similar experiments, always around 1 probe per query."*

**Repo:** `loadbalancer/config.go` → `ProbesPerQuery: 1.0`, which sits right at the edge the paper calls the unsafe boundary.

**Why.** The original goal was to keep probe overhead bounded on a small kind cluster. `ProbesPerQuery=1.0` was the minimum that could still keep the pool populated at 500 rps. The investigation `2026-04-20-prequal-overhead-profiling.md` independently arrived at the same concern (probes at 500 rps compete with user traffic for backend CPU on CPU-bound C1 workloads), which pushed the value *down* instead of up.

**Consequence.** Strong candidate for the C1 25% throughput loss. The paper's own threshold language puts the repo right on the cliff. On IO-bound C2/C3 the margin is so large (10×) that this parameter being slightly off doesn't threaten the claim — but it's still a departure from the paper's default and should be made visible when publishing numbers.

**Fix.** Ideally bump to `3` for fleets ≥ 8 and sweep `{1, 2, 3, 5}` on C2/C3. If CPU competition becomes the bottleneck again, the right move is not to cut probes, it's to run the paper's actual regime: more replicas per host, lower per-replica CPU allocation, antagonist workloads.

---

## 3. `b_reuse = 3` hardcoded instead of derived from Equation 1

**Paper (§4, Eq. 1):**

```
b_reuse = max { 1,  (1 + δ) / ((1 − m/n) · r_probe − r_remove) }
```

Where `m` = pool size, `n` = number of replicas, `r_probe` = probes per query, `r_remove` = probes removed per query, `δ > 0` governs net pool drift.

**Paper default values:** `m=16, n=100, r_probe=3, r_remove=1, δ=1` → `b_reuse = max{1, 2/1.52} ≈ 1.32`, stochastically rounded to `1` or `2` per-query.

**Repo:** `loadbalancer/config.go` → `PoolReuseLimit: 3`, decremented on every successful `Select`, removed when `UsesLeft ≤ 0`.

**Why.** The paper's formula assumes `n >> m`. On the repo's C2/C3 testbed, `n = 16` equals `m = 16`, which makes `1 − m/n = 0` and the denominator degenerate. For small fleets the formula doesn't apply. `3` is a fixed choice in the plausible range for the testbed scale.

**Consequence.** On 16-backend workloads, likely harmless — every backend is always in the pool anyway, so reuse limit governs churn rate, not coverage. On a paper-scale deployment (`n=100`, `m=16`) the hardcoded `3` would be close to but not matching the paper's derived `≈1.3`, probably producing slightly more stale-probe effects.

**Fix.** Teach `PoolConfig` to compute `b_reuse` from `(m, n, r_probe, r_remove, δ)` once `n > m`. Keep the constant as fallback when `n ≤ m`.

---

## 4. `r_remove` is maintenance-tick driven, not per-query

**Paper (§4):** *"We define a `r_remove` parameter, and delete that many probes from the pool with each query. … We alternate our removals between worst and oldest."*

**Repo:** Per-query removal happens implicitly through `UsesLeft` decrement on `Select`. Explicit "worst/oldest" removal happens only on the 100 ms maintenance tick in `pools.Run`, not on every request.

At 500 rps, this is ~500 `UsesLeft`-driven removals/s + ~10 `RemoveWorst` calls/s, vs the paper's 500 per-query removals/s.

**Why.** Per-request mutex work was the original concern — `RemoveWorst` requires scanning the pool and computing a threshold, which would add tens of microseconds to every request. The maintenance-tick design moves that work off the hot path. `UsesLeft` is functionally similar (eviction by use-count, not by worst-load ranking).

**Consequence.** The two churn mechanisms (`UsesLeft` and periodic `RemoveWorst`) are not exactly equivalent to the paper's per-query `r_remove`. The paper's design removes the *worst* or *oldest* probe most of the time, biasing the pool toward fresh and cold. The repo removes the *most-reused* probe most of the time, which isn't the same ranking. In steady state both converge on a similar pool composition; under transients (pool just refilled, sudden overload) they can diverge.

The overhead profiling run (`2026-04-20-prequal-overhead-profiling.md`) confirmed the controller side is cheap regardless, so the "moved work off the hot path" argument is weaker than originally thought. The per-query approach would cost a few µs per request at most — probably worth doing to match the paper.

**Fix.** Add a per-`Select` call to `RemoveWorst` gated by a random-rounded `r_remove`, alternating oldest/worst like the paper does.

---

## 5. Probe targets are sampled with replacement, single-pick

**Paper (§4, probe pool):** *"Probe destinations are sampled uniformly at random without replacement from the set of available replicas. This design choice is motivated by the theory literature on balanced allocations, which advocates sampling a random set of probe targets. It also helps to avoid the thundering herd phenomenon…"*

**Repo:** `loadbalancer/prober.go` → `ProbeRandom` does one `rand.Intn(len(backends))` per enqueued job; the same backend can appear in multiple consecutive probes.

**Why.** At `ProbesPerQuery = 1.0` this is moot: you're only drawing one target, and "with replacement for one draw" is the same as "without replacement for one draw." Implementing without-replacement adds complexity that wasn't needed under the current probe rate.

**Consequence.** **Latent bug.** The moment `ProbesPerQuery ≥ 2` (either by config change or if divergence 2 is fixed), the sampling guarantees the paper relies on (balanced-allocations, anti-thundering-herd) break. The repo would probe the same backend twice in the same query with positive probability.

**Fix.** `TriggerProbes` should draw `n` distinct backends (reservoir sample or `rand.Shuffle` + take-first-n) instead of enqueueing `n` independent work items. This pairs with divergence (2) and should land in the same change.

---

## 6. Workload regime: capacity skew instead of antagonist-induced overload

**Paper (§5):** *"All experiments use 100 client replicas and 100 server replicas, and each of the server replicas is allocated 10% of the machine's CPU. … The queries represent a very simple CPU-intensive workload: they simply iterate an expensive hash function."* The wins come from the load-ramp experiment (§5.1) where *aggregate* utilization rises from 0.75× to 1.74× of allocation while antagonist workloads absorb or release CPU on each physical host.

**Repo:** 16-backend workload with 14 fast replicas (`WORK_MULTIPLIER=1.0`) and 2 slow replicas (`WORK_MULTIPLIER=16.0`), all in IO-bound mode (`tokio::time::sleep`). No antagonist load. Service-time skew is static, not time-varying.

**Why.** Kubernetes-in-Docker (kind) cannot realistically simulate the paper's scenario. Isolating 100 replicas at 10 % CPU each on a Docker-Desktop-size VM with variable antagonist overflow is not a thing you can do on a laptop. Static capacity skew is the closest local proxy, and both produce the same mechanism (some replicas serve slowly, queue depth builds, tail latency diverges), but they are not the same experiment.

IO-bound mode is the other half of this decision. CPU-bound workloads on kind invite probe-vs-user CPU contention that the paper's multi-tenant VM isolation would mask. Moving to `tokio::sleep` isolates the algorithmic question from the kind-scale hardware noise.

**Consequence.** The repo's headline 10× result does not reproduce the paper's specific experiment. It produces a *shape-of-result-compatible* experiment in a different regime. The repo cannot claim "we reproduced the paper's result"; it can claim "in a regime that stresses the same mechanism Prequal was designed to fix, the algorithm wins as the paper's model predicts."

The repo's C1 negative result (25% prequal loss on 4-backend CPU-bound closed-loop) is actually consistent with the paper: at that scale there is no antagonist-induced overload, probe overhead is real, HCL has no sample diversity, and the algorithm has nothing to extract.

**Fix.** Running an overload variant of C2 (paper §5.1-style — push aggregate rate past backend capacity rather than varying per-backend capacity) would get closer to the paper's actual experiment on this testbed. Infrastructure already exists (`benchmark/k6/overload.js`); the run is listed as C6 in the matrix and is unexecuted.

---

## 7. Sync mode is not implemented

**Paper (§4):** *"Besides async mode, we have also implemented a sync mode of Prequal in which there is no probe pool. Instead, when a query arrives, the client issues some `d` probes to random replicas, waits to receive a sufficient number of responses (typically `d−1`), and then chooses among those using the same replica selection rule."* *"We have used sync Prequal … for part of YouTube."*

**Repo:** Async only. `Prober.TriggerProbes` fires probes and returns immediately; `ServeHTTP` selects from whatever's in the pool now.

**Why.** Sync mode's canonical use case is when the backend can *change the cost of the query* based on what the probe tells it — e.g., YouTube Homepage's cache-aware routing. That requires a richer probe-response contract (the client includes query-specific hints, the backend scales its own work accordingly) that doesn't fit a generic K8s ingress where the request body is opaque and the backend is an arbitrary service.

**Consequence.** None for the claim as written. The repo states async-only explicitly and the benchmarks are all async. The constraint is on claim scope: nothing in this repo addresses the sync-mode use case or its results from the paper.

**Fix.** Not planned. If sync mode ever becomes useful, it would need a separate code path and a backend protocol extension, probably gated behind an annotation like `lb/algo=prequal-sync`.

---

## Things the repo does that the paper didn't have to

The ingress context requires a few mechanisms the paper's RPC-framework context did not.

### Route-scoped probe pools

The paper assumes one Stubby channel = one backend set. A K8s ingress controller proxies many services; using a single global probe pool would let observations from route A's backends bias selections on route B. `loadbalancer/pool/pools.go` keeps independent pools per route key. This is a correctness prerequisite for any multi-route deployment.

### Staleness-gated probe timestamps

Every probe response carries a backend-side timestamp; the controller rejects probes where `time.Since(ts) > MaxProbeAge`. The paper's `b_reuse` + age-cap handles this implicitly through reuse bookkeeping. The explicit check is more robust against clock skew between controller and backend pods (not zero on kind; very-not-zero on a real fleet with drifted clocks) and against backend-side stalls that delay a response without the controller noticing.

### `random_fallback_rate` as an explicit metric

`observability/metrics.go` exports `prequal_selection_algorithm_total{algorithm="random_fallback"}`. The paper acknowledges the fallback exists ("if the pool is empty, Prequal simply falls back to selecting a uniformly random replica") but doesn't call out the monitoring implication. Without this metric, pool starvation silently degrades HCL into random selection and you won't notice from p99 alone. The investigation `2026-04-19-c2-tail-spike.md` (hypothesis H7) would have been harder to rule out without this counter being in place.

---

## What to close first if you want to match the paper

In priority order:

1. **Divergence 2** — `ProbesPerQuery: 1.0 → 3`. Matches paper default, addresses the main suspect for the C1 overhead, doesn't need any code change.
2. **Divergence 5** — switch `Prober` to without-replacement sampling *before* bumping probe rate, or the rate bump makes the bug worse.
3. **Divergence 1** — `QRIF: 0.75 → 0.84`. Trivial. Run a sweep on C2 and publish both.
4. **Divergence 6 (partial)** — run C6 (overload variant) on E-B under the controlled protocol. The scripts are already there; this is a multi-hour kind run, not new code.
5. **Divergence 4** — fold `r_remove` into the per-request path. Lowest leverage; won't move headline numbers but closes the last behavioural gap with the paper.
6. **Divergence 3** — derive `b_reuse` from Eq. 1 when `n > m`. Only relevant on large fleets; not benchmark-visible here.

Divergences 6 (fully) and 7 are out-of-scope for the current testbed: the paper's full regime needs more machines than kind provides, and sync mode doesn't fit the ingress context.

---

## Primary references

- Paper: [https://www.usenix.org/system/files/nsdi24-wydrowski.pdf](https://www.usenix.org/system/files/nsdi24-wydrowski.pdf)
- Canonical repo conclusion: [`REPORT.md`](REPORT.md)
- Investigation trail: [`investigations/2026-04-19-c2-tail-spike.md`](investigations/2026-04-19-c2-tail-spike.md), [`investigations/2026-04-20-regime-pivot.md`](investigations/2026-04-20-regime-pivot.md), [`investigations/2026-04-20-prequal-overhead-profiling.md`](investigations/2026-04-20-prequal-overhead-profiling.md)
