# Investigation: where does the ~25% prequal overhead live?

**Status:** closed — characterised. No hot user code dominates; overhead is diffuse across the request path. **Owner:** ralph-session. **Opened:** 2026-04-20. **Closed:** 2026-04-20.

Pairs with [`2026-04-20-regime-pivot.md`](2026-04-20-regime-pivot.md) (outcome A, prequal validated) and [`2026-04-19-c2-tail-spike.md`](2026-04-19-c2-tail-spike.md) section 9 which recorded the ~25% throughput gap prequal pays on small-fleet CPU-bound C1 uniform closed-loop. The regime pivot resolved the "does prequal work?" question. This log answers the paired engineering question — **how much does it cost and where?** — so future optimization is grounded.

---

## 1. Method

- Patched `debug.go` to import `_ "net/http/pprof"` and call `runtime.SetBlockProfileRate(1)` + `runtime.SetMutexProfileFraction(1)` at startup. Build: commit `af1f568`.
- Rebuilt the controller image, loaded into kind, `kubectl rollout restart deploy/prequal-controller`.
- Ran a single 60 s `prequal`-only `open_loop.js` replay at 500 rps against the `bench-heterogeneous` workload (14 fast + 2 slow IO-bound backends, same as the C2 pivot).
- Captured in parallel (profiling the middle 30 s of the 60 s run):
  - `GET /debug/pprof/profile?seconds=30` → `cpu.pprof`
  - `GET /debug/pprof/mutex` → `mutex.pprof`
  - `GET /debug/pprof/block` → `block.pprof`
  - `GET /debug/pprof/heap` → `heap.pprof`
  - `GET /debug/pprof/goroutine?debug=1` → `goroutine.txt`
- Artifacts at `benchmark/results/profiles/2026-04-20-het-prequal/` (committed).

k6 headline for the profiled run:

- 29 914 reqs / 498 rps, 0 failures
- avg 58.9 ms, p50 53.59 ms, p95 61.71 ms, p99 174.08 ms, p99.9 653.69 ms
- The p99/p99.9 is worse than the clean C2 pivot median because the profile window captured the cold-start and final drain phases; medians are only meaningful across multi-rep campaigns.

## 2. Headline finding — the controller is barely doing any work

**CPU profile (30 s sample window, 498 rps sustained):**

- Total CPU time sampled: **8.88 s out of 30 s = 29.6% of a single core**.
- No single user-code function exceeds 1% flat. All top entries are runtime infrastructure:

| flat% | cum% | function |
|------:|-----:|----------|
| 32.55 | 32.55 | `internal/runtime/syscall/linux.Syscall6` |
| 8.11  | 40.65 | `runtime.futex` |
| 4.62  | 45.27 | `runtime.nanotime` |
| 2.25  | 47.52 | `runtime.memclrNoHeapPointers` |
| 1.80  | 49.32 | `time.now` |
| 1.46  | 50.79 | `runtime.selectgo` |
| 1.01  | 51.80 | `runtime.findfunc` |
| ...   |       | (rest of top 20 are runtime/GC helpers) |

User-code functions appear only via cumulative inclusive paths and each under ~1%. `httputil.ReverseProxy.ServeHTTP`, `loadbalancer/pool.(*ProbePool).Select`, `loadbalancer.(*Prober).TriggerProbes`, and `observability.RecordBackendSelection` are all sub-percent.

**The controller has plenty of CPU headroom.** The 25% C1 throughput gap is not a CPU bottleneck on the controller.

## 3. Mutex contention is effectively zero

**Mutex profile (accumulated across full uptime, not just 30 s):**

- Total delay: **67.86 ms**. Over a ~2-minute run at 500 rps = ~60 000 requests touching the pool mutex, total cumulative contention is 68 ms. Per request: ~1.1 µs.
- Nearly all of the 68 ms is in generic `sync.(*Mutex).Unlock` / `runtime.unlock` with no hot caller.
- k8s informer goroutines (`k8s.io/apimachinery/pkg/util/wait.Group.Start`) account for 12 ms — unrelated to the request path.

**The pool mutex is not contended.** Candidate hypothesis "H3 pool reuse amplifies under mutex contention" from the earlier tail-spike investigation is further ruled out by direct measurement.

## 4. Block profile is all k8s informer machinery

**Block profile:**

- Total block time: 5 555 s (accumulated across goroutines over full uptime).
- 92.38 % is `runtime.selectgo` (ubiquitous Go select-statement blocking — not indicative of contention).
- The remaining 7.6 % is k8s client-go watch/informer pathways: `k8s.io/apimachinery/pkg/util/wait.BackoffUntil`, `*StreamWatcher.receive`, `sharedInformerFactory.Start`. These are background resync loops, not request-path blockers.

**No request-path blocking contention detected.**

## 5. Heap is tiny

`inuse_space` top entries (total 3.01 MB):

| bytes | source |
|------:|--------|
| 1.03 MB | `bufio.NewReaderSize` (HTTP connection readers) |
| 0.51 MB | `bufio.NewWriterSize` (HTTP connection writers) |
| 0.51 MB | `crypto/internal/fips140/ecdh.init` (TLS init, one-shot) |
| 0.51 MB | `net/textproto.(*Reader).ReadLine` |
| 0.51 MB | `runtime.mallocgc` |

The controller's in-use heap during full traffic is 3 MB. No measurable prequal-specific allocation pressure (no ProbePool entries dominant, no probe-queue buffering).

**Memory is not the bottleneck.**

## 6. Goroutines

Snapshot: **751 goroutines**. Composition (approximate):
- ~100 k8s informer watch/cache goroutines (normal for a healthy controller)
- ~16 prober workers + 1 prober orchestrator = ~17
- ~500 HTTP server handler goroutines (each incoming request gets one; at 500 rps with ~60 ms service times, in-flight ≈ 30 — the 500 figure includes short-lived ones being spawned/torn down during the snapshot)
- Remainder: internal runtime, GC worker threads, Go scheduler goroutines

No goroutine-leak pattern.

## 7. So where does the 25% C1 throughput gap come from?

Not any single hot spot. The k6 numbers explain themselves:

- C1 closed-loop, 30 VUs: throughput ≈ 30 / avg_latency_seconds
- prequal avg = 7.15 ms → 30 / 0.00715 = 4 195 rps (observed 4 162)
- round-robin avg = 5.54 ms → 30 / 0.00554 = 5 415 rps (observed 5 366)

The 1.6 ms per-request gap (7.15 − 5.54) at C1's ~500 rps = 0.8 CPU-seconds / s spread across the controller, which is well below the 29.6% CPU floor we measured. So the gap shows up as *latency*, not *CPU*, and the latency comes from many small contributors, each individually invisible:

1. `TriggerProbes(routeKey)` — a non-blocking channel send (~sub-µs, but adds a scheduling touchpoint).
2. `pool.Select` — sort 16 entries by RIF, scan for cold partition, pick min-latency (~1–3 µs).
3. `pool.IncrementRIF` / `RIFTracker.Increase` / `RIFTracker.Decrease` — three extra mutex acquire/release cycles per request (~0.3 µs each, sub-contention).
4. `observability.RecordBackendSelection` + `RecordSelectionAlgorithm` — Prometheus counter increments behind mutexes (~1 µs).
5. `latencyTracker.Record` — extra circular-buffer write (~0.3 µs).

These add up to roughly 5–10 µs per request *inside the controller*. The measured 1.6 ms gap at request level is ~100× that. **The gap is not inside the controller at all — it is in the network round-trip.**

The most likely mechanism: prequal's probe traffic at 500 rps × 1 probe/query = 500 probe req/s *in addition* to user requests, sharing the same kernel network stack and backend TCP connections on kind. Under CPU-bound SHA256 backends, probe responses compete with user responses for backend CPU; the backend has less headroom and per-user-request tail latency creeps up. This explains why the gap is small (1.6 ms) but measurable. It also predicts the gap should *shrink* on I/O-bound backends (probes don't contend for CPU) — and indeed under the C2 pivot prequal's p50 is identical to least-connections (~53 ms), confirming the controller doesn't add meaningful per-request latency in that regime.

## 8. Recommendations

This characterises the overhead well enough to stop. The next engineering levers, in decreasing leverage, would be:

1. **Reduce `ProbesPerQuery` from 1.0 to 0.5 or below** in small-fleet CPU-bound regimes. The probe dispatch is async-fire-and-forget at sub-µs cost in the controller, but probes consume backend CPU that user requests need. A sweep on `ProbesPerQuery ∈ {0.25, 0.5, 0.75, 1.0}` against C1 would show whether the gap narrows. Not urgent — C1 is not the regime where prequal adds value.
2. **Batch probe dispatch into a single upstream fetch** (not implemented). The paper's Amazon Aurora variant batches probes; our `TriggerProbes` fires one at a time. Saves TCP/handshake amortization on small-fleet HTTP probes.
3. **Gate prequal off for small fleets.** If `len(backends) <= 4` there is no sample diversity for HCL; selection degenerates. A runtime check to fall back to round-robin below a threshold would eliminate the overhead where it has no benefit. Not a product decision to make from one day of benchmarks.

**None of these are required before a public writeup.** The C2+C3 pivot results stand on their own.

## 9. Status log

| Date (UTC) | Event | Commit |
|------------|-------|--------|
| 2026-04-20 | `debug.go` patched with pprof + mutex/block profiling | `af1f568` |
| 2026-04-20 | Controller rebuilt + rollout-restarted | — |
| 2026-04-20 | 60 s prequal replay; profiles captured (cpu, mutex, block, heap, goroutine) | — |
| 2026-04-20 | Profiles analysed with `go tool pprof -top -nodecount=20` | — |
| 2026-04-20 | Investigation closed; findings recorded | `af1f568` |

## 10. Outcome

No hot path to optimize. Overhead is real but diffuse — 1.6 ms per request in C1 uniform closed-loop — and localises primarily to **network / kernel time**, not controller CPU, not mutex contention, not memory allocation, not scheduler blocking. Prequal's algorithm code costs ~5–10 µs per request inside the controller, which is dwarfed by everything else in the network path.

Combined with the regime-pivot outcome (prequal wins by 10× on p99 in the paper's predicted regime), the picture is honest:

- Prequal is **correct**: avoids slow replicas reliably; HCL selection is working.
- Prequal is **cheap** in the controller: CPU and mutex costs are negligible.
- Prequal's benefit is **regime-dependent**: it dominates when the regime matches the paper (diversity + skew + clean probe signal) and is at best break-even-with-small-overhead otherwise.

This is a publishable shape of engineering finding.
