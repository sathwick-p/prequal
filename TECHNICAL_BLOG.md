# Building `prequal`: a Kubernetes ingress controller, a failed benchmark, and a regime-specific `10x` tail-latency win

This is the long-form narrative behind the repo. The [README](README.md) has the headline numbers and a reproduce-it path. The [locked-down technical conclusion](benchmark/REPORT.md) has the bounded claim in its most compressed form. This post is what sits between those two.

The paper the whole thing is built from is [Wydrowski et al., NSDI '24, "Load is not what you should balance: Introducing Prequal"](https://www.usenix.org/system/files/nsdi24-wydrowski.pdf).

## Why build this

I wanted to see whether the Prequal paper's central claim would reproduce inside a Kubernetes ingress controller I wrote myself. Most ingress controllers ship with `round-robin` or `least-connections`, and both are reasonable defaults. They're also blind to a specific class of failure: a backend can be technically healthy but materially slower than its peers, or locally congested in ways CPU balancing doesn't capture. When that happens, the symptom usually shows up in the tail rather than the median.

The paper's idea is that active probes, combined with requests-in-flight and measured latency per backend, can beat blind distribution in the right regime. A request-path proxy looks at recently probed backends, applies hot-cold lexicographic (HCL) selection across them, and routes accordingly. On Google-scale heterogeneous workloads, the paper shows a large tail-latency win.

The question I cared about was whether that win survives being reimplemented from scratch. Different language, different deployment shape, much smaller cluster. And if it doesn't, why.

## What the system looks like

The controller is one Go binary with two jobs.

![prequal System Overview](benchmark/diagrams/system-overview.png)

Control plane: it watches Kubernetes `Ingress` and `EndpointSlice` objects and materialises them into an in-memory router and backend IP store. Route matching is a host-and-path trie. Route replacement has to be atomic from the proxy's perspective or reconciliation under churn exposes half-updated routing state. An earlier version of this code had transient gaps during updates, and fixing that was a prerequisite for any benchmark number to mean anything.

Data plane: the proxy receives HTTP requests, matches them against the router, picks a backend using the policy annotated on the route (`round-robin`, `least-connections`, or `prequal`), and forwards through `httputil.ReverseProxy`. The first two policies are straightforward. For `prequal`, each route keeps its own probe pool populated by async probes to backends, and selection is an HCL pass over that pool.

The algorithm's pieces all live under `loadbalancer/`. `prober.go` is an async prober that fires probes to random backends through a bounded worker pool, decodes a JSON response from each backend's `/prequal/probe`, and pushes the result into the appropriate route's pool. `pool/pool.go` is the pool itself: bounded size, age-based eviction, reuse-limit bookkeeping, plus the `Select` call that does HCL over live entries. `pool/pools.go` is the route-scoped map wrapper that keeps pools isolated so probes for route A don't contaminate selections on route B. `latency.go` and `rif_tracker.go` track latency and RIF inside the controller as a fallback signal if the probe pool is empty.

![How prequal Chooses a Backend](benchmark/diagrams/backend-selection.png)

The backend serving `/prequal/probe` is a small Rust server in `backend/`. It reports current RIF, a local-median latency estimate from a RIF-conditioned bucket, and a server-side timestamp so the controller can drop stale probe responses.

Three implementation choices ended up mattering more than they looked at first.

Route-scoped probe state, for one. An earlier version used a global pool and probe observations leaked across routes: route B's requests could get a backend that had only ever been observed under route A's load. This was fixed well before I trusted any benchmark result. Then there's the bounded probing story: the prober has a fixed-size worker pool, a fixed-size trigger queue, a configurable `ProbesPerQuery` rate, and a periodic pool-maintenance ticker. Without those bounds the act of measuring the system starts to dominate the system. On a small kind cluster that isn't a theoretical concern, it just happens. And finally, metrics that describe what the algorithm is doing rather than just whether it's up. `observability/metrics.go` exports per-backend selection rate, per-reason probe failure counts, probe queue depth, per-route pool occupancy, and a `random_fallback_rate` counter that increments when the pool is empty and the proxy has to fall back to random selection. That last one turned out to be decisive during debugging.

## The first benchmark result was catastrophic

The first real run was `Campaign 2 (heterogeneous open-loop)`. Four backends, three fast and one slow at `WORK_MULTIPLIER=4`, constant-arrival-rate 500 rps for 5 minutes, 3 reps per algorithm, run sequentially. All three prequal reps first, then all three round-robin, then all three least-connections.

The numbers made me stop whatever I was about to do next.

| algorithm | p95 ms | p99 ms | p99.9 ms |
|-----------|-------:|-------:|---------:|
| prequal | 126 | **854** | **1822** |
| round-robin | 58 | 284 | 1802 |
| least-connections | 34 | 165 | 562 |

`prequal` wasn't just losing, it was losing by an order of magnitude on `p99`. Same throughput, zero errors, medians basically tied, and somewhere the algorithm was making occasional catastrophic selections that dominated the tail.

This is the point where a short investigation could have concluded "the implementation is wrong" and gone hunting inside HCL. That's where I wanted to go. What stopped me was per-run variance. One prequal rep had `p95` of 17 ms, another had 196 ms. A bug in the algorithm shouldn't produce that much swing between identical runs.

## The hypothesis tree

Before touching code I wrote down every explanation I could think of and what evidence would distinguish them. Full version lives in [`benchmark/investigations/2026-04-19-c2-tail-spike.md`](benchmark/investigations/2026-04-19-c2-tail-spike.md). Short version:

- H1: `QRIF` is too lax. With 4 backends and `QRIF=0.75`, 3 of 4 pool entries qualify as "cold," so HCL degenerates into "pick the lowest-latency entry unconditionally." If that's the slow backend (because it briefly looks idle before its queue fills), prequal keeps sending to it. Tuning problem.
- H2: The backend's RIF-bucketed `latency_median_ms` hides the slow replica. The Rust backend reports the median from whichever RIF bucket corresponds to its current in-flight count. At low RIF the slow backend reports low latency because it hasn't accumulated queue depth yet. By the time RIF rises, the controller has committed. Algorithm-fidelity problem.
- H3: Pool reuse (`PoolReuseLimit=3`) amplifies stale observations. A single probe reading can be reused up to 3 times over 2 seconds. If that reading was wrong (H2), the error compounds.
- H4: Under-sampling relative to arrival rate. `ProbesPerQuery=1.0` might not keep fast-backend observations fresh at 500 rps.
- H5: Sequential algorithm ordering is leaking pool state between runs. Each run's pool inherits state from the previous run of the same algorithm, and prequal-to-prequal pool warmth differs from round-robin-to-round-robin. Methodology, not algorithm.
- H6: Open-loop at 500 rps hits a resonance. Least likely, not ruled out.
- H7: Random-fallback thrash. If the pool empties and the controller falls back to random, prequal silently degenerates into random. Should show up as `random_fallback_rate > 0`, but I wasn't capturing that per-run yet.

I needed data to distinguish these. Specifically, I couldn't tell H5 from H2 without an interleaved pass, and I couldn't tell H7 from anything without a captured fallback rate.

![Benchmark Story: From Wrong Result to Bounded Conclusion](benchmark/diagrams/benchmarking.png)

## The methodology fix

I patched `run_campaign.sh` and its collector to capture `controller_env`, `backend_env`, per-backend selection rate, and random-fallback rate on every run. I also wrote `run_interleaved_campaign.sh`. Algorithm order is interleaved now rather than sequential (prequal, round-robin, least-connections, prequal, round-robin, and so on), and before every single run the script issues `kubectl rollout restart deploy/prequal-controller` and waits 15 seconds for pools to repopulate cold.

Then I reran `C2`. Same 4-backend heterogeneous workload. Same script. Same rate. Same duration. 5 reps this time, controlled protocol.

| algorithm | p95 ms | p99 ms | p99.9 ms |
|-----------|-------:|-------:|---------:|
| prequal | 3.27 | 15.22 | 54.40 |
| round-robin | 3.25 | 14.40 | 49.88 |
| least-connections | 3.37 | 15.30 | 59.19 |

39x reduction on `p95`. 56x reduction on `p99`. No code change, no tuning change. The prior tail-spike had been pool-state leakage between sequential runs, interacting with prequal's reuse behavior in a way that gave prequal the worst of it. Once every run started cold, all three algorithms converged.

This is the thing I wish I'd internalised earlier. Benchmark methodology fails in ways that look like algorithm failure. If I'd gone straight to a code hunt in HCL I'd have lost days "fixing" a perfectly correct implementation.

## All three algorithms tie, which is also a problem

The fix resolved the disaster and produced a different one. Once the noise was gone, none of the three algorithms won. Prequal wasn't losing anymore, but it wasn't winning either. The paper predicts a large tail advantage under heterogeneous capacity and I wasn't seeing one.

The numbers were internally consistent, per-backend selection showed prequal correctly avoiding the slow replica, and `random_fallback_rate` was zero. The algorithm was doing what it said it did. There just wasn't a gap for it to exploit.

The small-fleet CPU-bound `C1` regime (uniform workload, 4 backends running SHA256, closed-loop 30 VUs) went the other way. Prequal was ~25% slower than round-robin on throughput and worse on `p99`. I spent a while staring at profiles looking for a hot path.

![C1 controlled, prequal loses the small-fleet CPU-bound regime](benchmark/results/screenshots/2026-04-19-C1-controlled/request-overview.png)

pprof refused to cooperate with that hypothesis. The controller was barely doing anything. At 498 rps it was using 29.6% of a single core total. No user-code function cleared 1% flat. Mutex contention was about 1.1 µs per request. Heap was 3 MB.

The arithmetic did cooperate, though. `C1` is closed-loop with 30 VUs, so throughput is `30 / avg_latency`. Prequal's average was 7.15 ms, predicting about 4195 rps (observed 4162). Round-robin averaged 5.54 ms, predicting about 5415 rps (observed 5366). The gap is 1.6 ms of latency per request, which at this scale is about 100x what the controller itself could plausibly be adding. Whatever was costing time was outside the controller.

That's the explanation that ended up in the final writeup. On CPU-bound backends, prequal's probe traffic competes with user traffic for the same backend CPU budget. Each probe is cheap on the controller side and non-trivial on the backend side, and the aggregate effect is a diffuse 1.6 ms per user request that no single function ever shows up as. Full analysis in [`benchmark/investigations/2026-04-20-prequal-overhead-profiling.md`](benchmark/investigations/2026-04-20-prequal-overhead-profiling.md).

The thing I liked about that explanation was that it made a prediction. If the backends stopped being CPU-bound, the probe traffic wouldn't compete for the same budget, the overhead should disappear, and whatever regime advantage prequal actually has should start showing up.

## The regime pivot

Three changes, all on the workload side. An I/O-bound backend: a new `IO_BOUND_MODE=1` env var in the Rust backend that replaces the SHA256 loop with `tokio::time::sleep(iterations × 50µs)`. Zero CPU per request, deterministic service time, `WORK_ITERATIONS=1000` becomes a 50 ms sleep. Sixteen backends instead of four, split as 14 fast replicas at `WORK_MULTIPLIER=1.0` and 2 slow replicas at `WORK_MULTIPLIER=16.0` — the paper's scenarios run on much larger fleets and four backends give HCL almost no sampling room. And a capacity skew of 16x rather than 4x, so a slow-replica request now costs as much as 16 fast-replica requests.

The first post-pivot `C2` run looked like this:

| algorithm | p95 ms | p99 ms | p99.9 ms |
|-----------|-------:|-------:|---------:|
| prequal | 59.48 | **80.60** | 127.49 |
| round-robin | 803.69 | 807.12 | 834.90 |
| least-connections | 62.85 | 802.46 | 808.82 |

10x on `p99`. Round-robin sends `2/16` of traffic blindly into the slow pair, those requests queue to ~800 ms service time, and the tail pins at that floor. Least-connections does better on `p95` because RIF tracking eventually pulls traffic away from slow backends, but anything that arrived before RIF caught up still pays the queue cost, which pins least-connections' `p99` at the same floor. Prequal never sends the request. Per-backend selection shows the two slow replicas getting fewer than 0.1 selections per second each.

![C2 pivot, the moment the algorithm win showed up](benchmark/results/screenshots/2026-04-20-C2-eb/request-overview.png)

The dashboard is the clearest visual evidence. Fifteen alternating runs under identical conditions, five each of three algorithms. `p50` is the same flat line for everyone. Only `p95` and `p99` separate, and they separate cleanly along the algorithm phase boundaries.

![Per-backend selection confirms the mechanism](benchmark/results/screenshots/2026-04-20-C2-eb/algorithm-behavior.png)

The algorithm-behavior panel shows the mechanism, not just the outcome. During prequal windows the selection distribution across backends stays dense. During round-robin windows it stays uniform, slow replicas included. During least-connections windows it skews modestly toward the fast replicas but never eliminates the slow ones.

`C3`, a rate ramp from 100 to 1500 rps over 370 seconds on the same topology, showed the same shape with smaller margins. Prequal `p99` of 117 ms, round-robin 834, least-connections 802. A 6.8x win, less dramatic than `C2` but through a harder range of load conditions.

## Does it survive cluster-topology changes

`E-A` is the environment I'd been running on: single-host `kind`, controller colocated on a worker node, sharing CPU with some of the backends. The first pivot results were both `E-A`.

`E-B` is a stronger cluster shape on the same Docker host. I added a toleration and `nodeSelector` to the controller manifest so it schedules exclusively on `kind-control-plane` (tainted, otherwise idle), and added `topologySpreadConstraints` to `bench-fast` and `bench-slow` so backend pods spread across `kind-worker` and `kind-worker2`. Controller CPU and backend CPU are now separated at the node level.

`C2` `p99` went from 80.60 ms to 94.20 ms for prequal on `E-B`. I read that as the control-plane node being a noisier scheduling neighbor: `kube-apiserver`, `etcd`, and `kube-scheduler` all live there and all want CPU. Round-robin and least-connections numbers barely moved. The advantage ratio shrank from 10.0x to 8.6x, which is well inside the min-max range of either environment's own variance.

`C3` shifted less. Prequal `p99` from 117.34 to 123.39, advantage ratio from 7.1x to 6.8x.

The advantage reproduces. Two cluster topologies on the same host isn't cross-infrastructure proof, but it does rule out quite a few cluster-shape artifacts.

## Why I believe the win

A few things, mostly.

Per-backend selection rate shows prequal driving traffic to the two slow replicas down to effectively zero while keeping fast-replica distribution nonzero across the board. So the win isn't accidental.

`random_fallback_rate` is zero on every locked-down run. The pool is never starving, prequal is running HCL the whole time, and it's not quietly degenerating into random.

The result reproduces across two cluster topologies that differ on whether the controller shares CPU with backends. Not independent hardware, but not a single data point either.

And the controller itself is cheap. The profiling investigation rules out hot CPU paths, mutex contention, heap pressure, and request-path blocking as explanations for the `C1` deficit. Whatever cost the algorithm pays when it pays one lives in the network, not in the implementation.

## What this repo can and can't claim

In this implementation, on this testbed, in the paper-aligned regime, prequal delivers a reproducible `p99` win over round-robin and least-connections. That's the strongest honest claim I have. It's bounded by an equally important negative: on small-fleet CPU-bound workloads, prequal doesn't win, and it pays overhead. The public summary, the internal report, the matrix, and the investigation logs all say both things.

The caveat stays attached:

> Both benchmark environments share the same Docker host. The current result is strong testbed evidence, not final cross-infrastructure proof.

There are other limits worth saying. The backend is a simulator, not a real service. The positive regime is specific, 14 fast plus 2 slow with 16x skew. The controller is not a finished production ingress. And the evidence doesn't yet cover independent cloud or bare-metal reproduction. Any of those could meaningfully shift the numbers.

## What I actually learned

Route-local correctness has to come before performance claims. An earlier version of the controller shared probe state across routes, and under single-route benchmarking that bug was invisible. If I hadn't fixed it before running serious loads it would have invalidated the whole campaign. Per-route isolation wasn't a feature request, it was a correctness prerequisite.

Benchmark methodology is an adversarial surface. The 3-rep sequential pass that made prequal look catastrophically bad wasn't malicious. It was sloppy. I ran things in whatever order, didn't reset state between runs, didn't capture environment knobs in metadata, and had no way to tell whether a result was real or an artifact until I fixed all of those. The time I spent building [`run_interleaved_campaign.sh`](benchmark/scripts/run_interleaved_campaign.sh) was more valuable than any single code change.

Load-balancing policies are regime-sensitive, and the honest answer is usually conditional. Prequal isn't universally better than round-robin. In some regimes it's worse. The question isn't "which algorithm wins" but "under what conditions does each algorithm win, and by how much." Framing it that way got me to a defensible public claim. Trying to shoehorn a universal answer out of the data didn't.

## What's next

Three directions the work could go, none of which I'm planning to do right now.

An independent cluster. Cloud or bare-metal, ideally one I don't own. The same-host caveat is the single biggest constraint on how hard this evidence can be cited, and lifting it would move the result out of testbed-study territory.

Campaigns `C4` through `C9` from the matrix: multi-route isolation, long-duration stability, overload, churn, fault injection, and an external NGINX baseline. Code and infrastructure for each of them is already in the repo. None have been run under the controlled protocol.

The three overhead levers from the profiling investigation: a `ProbesPerQuery` sweep in small-fleet CPU-bound regimes, batched probe dispatch, and small-fleet runtime gating (fall back to round-robin when `len(backends) <= N`). None of these matter if prequal only ever ships as opt-in. They do matter if it becomes a default.

## Read next

- [README](README.md) for the short version with headline screenshots
- [benchmark/REPORT.md](benchmark/REPORT.md) for the locked-down technical conclusion
- [benchmark/PUBLIC_WRITEUP.md](benchmark/PUBLIC_WRITEUP.md) for a shorter public summary
- [benchmark/benchmarking-matrix.md](benchmark/benchmarking-matrix.md) for the row-by-row campaign record
- [benchmark/investigations/2026-04-19-c2-tail-spike.md](benchmark/investigations/2026-04-19-c2-tail-spike.md) for the methodology-failure debugging trail
- [benchmark/investigations/2026-04-20-regime-pivot.md](benchmark/investigations/2026-04-20-regime-pivot.md) for the pivot that produced the positive result
- [benchmark/investigations/2026-04-20-prequal-overhead-profiling.md](benchmark/investigations/2026-04-20-prequal-overhead-profiling.md) for the pprof analysis of the controller under load
