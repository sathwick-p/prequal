# Investigation: regime pivot — does prequal win in the paper-predicted regime?

**Status:** open. **Owner:** ralph-session. **Opened:** 2026-04-20.

This log pairs with [`2026-04-19-c2-tail-spike.md`](2026-04-19-c2-tail-spike.md), which closed with the finding that prequal does not win in any regime tested on 4 CPU-bound SHA256 backends at kind-local scale. This log documents the pivot to a regime closer to the Prequal paper's assumptions and the decision rule for whether we can declare the algorithm validated.

Any engineer picking this up cold should be able to read this top-to-bottom and understand what changed, why, and what result to expect.

---

## 1. Why a pivot, not a tuning sweep

The closed-out C2 investigation identified four reasons our local testbed is outside the band where Prequal is expected to win:

1. **Backend count.** Paper uses 100+ backends; we had 4. HCL's probe-sampling only adds value when the pool has meaningful diversity.
2. **Capacity skew.** Paper exercises 8–16× skew between fast and slow; we had 4×.
3. **Workload shape.** Paper assumes probed latency reflects realistic service time. Our SHA256 backend is tightly CPU-bound, so probes under load measure CPU contention noise instead of backend latency.
4. **Cluster shape.** Single kind host shares Docker Desktop CPU across controller, k6, Prometheus, and all backends. Noisy neighbours pollute the tail equally across algorithms.

Parameter tuning on that regime has no "gap to close" — the median numbers were already tied within noise. The regime is the constraint, not the tuning.

## 2. Four changes we're making

### 2.1 Environment trim

Everything unrelated to the benchmark is deleted to free Docker Desktop CPUs:

- Namespaces deleted: `gprxy`, `emergent-system`, `test-app-alpha`, `test-app-beta`, `test-app-gamma`, `xyz-test`.
- Old `default`-namespace prequal stuff deleted: `prequal-backend` (3 pods), `prequal-controller` (1 pod), `prequal-proxy` svc (the one that held NodePort 30080/30081), `postgres-postgresql`.
- `prequal-benchmark/prequal-controller` scaled from 3 → 1 replica (benchmark doesn't need HA).
- `default/svc/kubernetes` was accidentally deleted during the sweep; kube-apiserver auto-recreates it immediately.

### 2.2 I/O-bound backend mode

New env vars in the Rust backend (`backend/src/main.rs`):

- `IO_BOUND_MODE`: `"1"`/`"true"` → I/O-bound path, anything else → default CPU-bound SHA256 path.
- `IO_BOUND_BASE_US`: microseconds per unit of `iterations`. Default `50` — so `WORK_ITERATIONS=1000` → 50 ms sleep per request. Realistic for a service call with downstream RPC + serialization.

When `IO_BOUND_MODE=1`, `work_handler` replaces the SHA256 loop with `tokio::time::sleep(iterations × IO_BOUND_BASE_US)`. Zero CPU per request — the pod is purely scheduler-blocked. RIF accounting and latency-bucket updates are preserved, so probe semantics are identical.

This is the single biggest change. With CPU out of the picture, 16 backends schedule cleanly on a 4-8-CPU Docker host, and probes measure actual per-request service time instead of CPU-queueing noise.

### 2.3 16-backend heterogeneous workload with 16× capacity skew

`benchmark/manifests/workload-heterogeneous.yaml`:

- **14 fast replicas** (`bench-fast`, `WORK_MULTIPLIER=1.0`, `IO_BOUND_MODE=1`) → ~50 ms service time.
- **2 slow replicas** (`bench-slow`, `WORK_MULTIPLIER=16.0`, `IO_BOUND_MODE=1`) → ~800 ms service time.
- Both share the `bench-heterogeneous` Service via `app.kubernetes.io/part-of` label.
- Resource requests dropped to `cpu=10m, memory=32Mi` (no CPU work), limits `cpu=200m, memory=128Mi`.

Fleet size 16 gives HCL real sampling diversity. 16× skew makes every "wrong" routing decision cost 16 requests-worth of fast time. If the algorithm has a visible advantage anywhere, this is the regime.

`workload-uniform.yaml` keeps 4 replicas (C1 is specifically about the overhead gap on a small fleet — we don't want to change both variables) but gets `IO_BOUND_MODE=1` for consistency with the new backend default.

### 2.4 No cluster node changes

Flagged but not acted on: adding kind worker nodes does not add host CPUs on Docker Desktop; kind nodes are containers on the same VM. With I/O-bound backends this no longer matters — 16 sleeping pods schedule fine.

## 3. Re-run plan

Single sitting:

1. Apply the new 16-backend heterogeneous manifest, wait for rollout.
2. Run **C2-heterogeneous-open-loop** under the controlled protocol (5 reps interleaved, controller reset + 15 s warmup, 500 rps, 300 s).
3. Run **C3-heterogeneous-ramp** under the controlled protocol (5 reps interleaved, 100 → 1500 rps ramp over 370 s).
4. Aggregate, render dashboards, update matrix, update this investigation log with the outcome.

Scope intentionally narrow: only C2 and C3 on heterogeneous. C1 overhead is a known engineering issue deferred to the next investigation if the algorithm-validation question resolves positively.

## 4. Decision rule

After the re-run, apply this rule to the C2 heterogeneous + C3 ramp data:

- **A. Prequal clearly wins (tail p99/p99.9 median lower than both baselines by ≥2× on C3 past-saturation):** algorithm validated on this testbed in its predicted regime. Publish the testbed config alongside any public claim. Close this investigation.
- **B. Prequal ties medians, baselines match on variance (worst-case reps):** regime is better but still not sufficient. Next is larger skew (`WORK_MULTIPLIER=32`) or longer per-rep duration to see if the advantage accumulates.
- **C. Prequal ties medians, has worse variance (like C3 did before):** the tail-variance failure is intrinsic to the implementation, not the regime. Open a new investigation on "prequal pool churn variance under saturation." Profile controller under C3 to identify the cost source.
- **D. Prequal loses medians in this regime too:** the implementation has something wrong beyond config. Escalate to CPU/mutex profiling (Track B from the previous close-out).

## 5. What the evidence will have that the previous round didn't

- Per-backend selection distribution across 16 backends (instead of 4). We can see whether HCL distributes fast-backend load evenly or concentrates on 2-3 of them.
- Probe response times that reflect service time (thanks to I/O-bound mode), not CPU contention.
- Random-fallback rate per run (already added to `collect_results.sh`).
- Continuous `kubectl top` TSV showing the controller's CPU fraction isolated from backend CPU.

## 6. Status log

| Date (UTC) | Event | Commit |
|------------|-------|--------|
| 2026-04-19 | C1 / C2 / C3 closed with prequal not winning in any tested regime | `31395cd` |
| 2026-04-20 | Regime pivot opened — this file | (pending commit) |
| 2026-04-20 | Backend `IO_BOUND_MODE` implemented and loaded into kind | `7a12c62` |
| 2026-04-20 | 16-backend IO-bound heterogeneous workload manifest | `f32db5e` |
| 2026-04-20 | Environment trim — unused namespaces deleted, controller 3→1 | (manual, state only) |
| TBD | C2 heterogeneous-open-loop re-run under pivoted regime | TBD |
| TBD | C3 heterogeneous-ramp re-run under pivoted regime | TBD |
| TBD | Decision rule applied, section 7 outcome written | TBD |

## 7. Outcome (to be filled in after the re-run)

_Pending._
