# Building `prequal`: A Kubernetes Ingress Controller For Probe-Driven Load Balancing

This post explains what `prequal` is, how the controller is built, how the benchmark campaign evolved, where the initial results were misleading, and what the final evidence actually supports.

It is a technical narrative for the repo, not a paper. The canonical frozen benchmark conclusion is still [benchmark/REPORT.md](benchmark/REPORT.md).

## Why Build This

Most ingress controllers ship with simple policies like round-robin or least-connections. Those policies are often good enough, but they are also blind to an important class of problems:

- a backend can be technically healthy but materially slower than its peers
- a backend can be locally congested in ways that CPU balancing does not capture
- the bad outcome shows up in the tail, not the median

The Prequal paper's core idea is that in the right regime, active probes plus requests-in-flight and latency signals can do better than blind distribution.

This repo is a Kubernetes-oriented implementation of that idea:

- watch `Ingress` and `EndpointSlice`
- build route state in memory
- proxy requests directly
- let routes choose a balancing algorithm
- let `prequal` use backend-exposed probe signals rather than only local request feedback

The interesting engineering question was never just “can we implement the algorithm?” It was:

- does it work in this controller shape?
- under what regime does it help?
- what does it cost when it does not help?

## What The System Looks Like

At runtime, `prequal` is one process doing two jobs.

### 1. Control plane

The controller watches Kubernetes resources, mainly:

- `Ingress`
- `EndpointSlice`

It turns them into an in-memory router and backend store:

- host and path matching
- service-to-backend resolution
- route metadata, including the selected algorithm

The important property is that route updates are atomic from the proxy's point of view. Earlier in the project we had transient update gaps; those were fixed before the serious benchmark campaign.

### 2. Data plane

The proxy receives incoming requests, matches them to a route, selects a backend, and forwards the request. The route chooses one of:

- `round-robin`
- `least-connections`
- `prequal`

For the first two, the behavior is straightforward. For `prequal`, the server uses route-scoped probe pools fed by asynchronous backend probes.

## How `prequal` Works Here

The implementation in this repo uses a backend endpoint that exposes probe data at `/prequal/probe`.

The controller:

- triggers probes asynchronously
- stores probe observations in route-scoped pools
- uses hot-cold lexicographic selection over requests-in-flight and latency
- avoids falling back to random unless the pool is empty

Two implementation details matter a lot.

### Route-scoped probe state

One of the biggest correctness fixes was making probe state route-local. Earlier, a global pool could contaminate route `B` with observations gathered for route `A`. That was fixed before the final benchmark campaign and is part of why the evidence is now trustworthy.

### Bounded probing

Probe generation is bounded:

- worker pool
- queue
- configurable probe rate
- configurable pool maintenance

Without that, the act of measuring backend state becomes the thing that dominates the system.

## The Benchmark Campaign Did Not Start Cleanly

One of the useful parts of this repo is that it keeps the investigation trail instead of only the happy ending.

The first heterogeneous pass made `prequal` look terrible. It lost badly on the tail. That could have led to the wrong conclusion:

- either the algorithm was wrong
- or the implementation was bad

Neither turned out to be the main issue.

The real problem was methodology:

- sequential algorithm ordering
- no controller reset between runs
- stale pool state leaking across phases

Once the campaign moved to:

- interleaved ordering
- controller reset before every run
- explicit warmup
- environment capture

the catastrophic negative result disappeared.

That is not a side note. It is an important systems lesson:

benchmark methodology can be wrong in ways that look like algorithm failure.

The full debugging trail is in [benchmark/investigations/2026-04-19-c2-tail-spike.md](benchmark/investigations/2026-04-19-c2-tail-spike.md).

## The First Stable Conclusion: Regime Sensitivity

Once the methodology was fixed, the next result was still uncomfortable:

- on small-fleet CPU-bound workloads, `prequal` did not win
- it was slower than the simpler baselines

That result stayed in the final report intentionally.

### C1: Small-fleet CPU-bound

On a small fleet with CPU-bound SHA256 work:

- `prequal` sustained less throughput
- `prequal` had worse p99 than round-robin
- `prequal` had no meaningful regime advantage to offset the cost of probing

This is the negative result that keeps the repo honest.

It also explains an important design point: if backend diversity is low and the signal is noisy, a more complex policy has less room to beat a simpler one.

## Where The Positive Result Appeared

The benchmark pivot changed the regime to more closely match the paper's favorable conditions:

- `16` backends instead of `4`
- `14` fast + `2` slow
- `16x` service-time skew
- I/O-bound backend instead of CPU-bound backend

This changes the problem materially.

### Why that regime matters

With larger fleet size and stronger skew:

- there is more backend diversity
- a bad selection hurts much more
- steering away from slow replicas has room to pay off

With an I/O-bound backend:

- probe measurements track service-time differences more cleanly
- probe traffic does not compete with user requests for backend CPU in the same way

That is exactly the regime where `prequal` should have a chance to show up.

## The Final Primary Evidence

The benchmark campaign ultimately froze around three campaigns:

- `C1`: small-fleet CPU-bound honesty check
- `C2`: heterogeneous open-loop comparison
- `C3`: heterogeneous ramp comparison

And two environments:

- `E-A`: single-host `kind`
- `E-B`: multi-node `kind` with controller isolation

The primary public evidence is `E-B`.

### C2 on E-B

| algorithm | E-B p99 ms | E-B p99.9 ms |
|-----------|-----------:|-------------:|
| **prequal** | **94.20** | **272.38** |
| round-robin | 807.32 | 887.90 |
| least-connections | 802.84 | 1006.78 |

That is an `8.6x` `p99` win versus the best baseline.

![C2 E-B Request Overview](benchmark/results/screenshots/2026-04-20-C2-eb/request-overview.png)

![C2 E-B Algorithm Behavior](benchmark/results/screenshots/2026-04-20-C2-eb/algorithm-behavior.png)

### C3 on E-B

| algorithm | E-B p99 ms | E-B p99.9 ms |
|-----------|-----------:|-------------:|
| **prequal** | **123.39** | **824.08** |
| round-robin | 831.58 | 1250.41 |
| least-connections | 867.93 | 1596.51 |

That is a `6.8x` `p99` win versus the best baseline.

![C3 E-B Request Overview](benchmark/results/screenshots/2026-04-20-C3-eb/request-overview.png)

![C3 E-B Probe System](benchmark/results/screenshots/2026-04-20-C3-eb/probe-system.png)

Throughput is effectively tied. The win is not in the median. It is in the tail.

That is exactly the shape we wanted to test for.

## Why We Believe The Win

There are a few reasons the final result is credible.

### 1. The selection mechanism behaves correctly

Per-backend selection shows `prequal` effectively blackholing the slow replicas while continuing to send traffic across the fast pool.

### 2. It is not winning by starving or falling back

`random_fallback_rate` stays at zero on the decisive runs. The pool is active and the algorithm is operating in its intended mode.

### 3. The result reproduces across two environments

The `E-B` result is the primary one, but the same shape appears on `E-A`. That does not make it universal, but it does make it harder to dismiss as a one-off cluster artifact.

### 4. The controller itself is cheap

We profiled the controller under `prequal` load.

The result:

- no hot user-code CPU path
- negligible mutex contention
- tiny heap
- no request-path blocking hotspot

The `C1` penalty is real, but it is not because the controller is computationally collapsing. The evidence points much more toward diffuse network/probe-path costs in the wrong regime.

That investigation is documented in [benchmark/investigations/2026-04-20-prequal-overhead-profiling.md](benchmark/investigations/2026-04-20-prequal-overhead-profiling.md).

## What This Repo Can Honestly Claim

The strongest honest claim is narrow:

- in this implementation
- on this testbed
- in a paper-aligned regime
- `prequal` delivers a large reproducible `p99` win over `round-robin` and `least-connections`

The result is also bounded by an equally important negative statement:

- on small-fleet CPU-bound workloads, `prequal` does not win

That symmetry matters. It keeps the repo from pretending the algorithm is universally better.

## What This Repo Does Not Yet Prove

The most important caveat is unchanged:

> Both benchmark environments share the same Docker host. The current result is strong testbed evidence, not final cross-infrastructure proof.

There are other limits too:

- the backend is a simulator
- the positive result is regime-specific
- the controller is not a full production ingress implementation
- the evidence does not yet cover independent cloud or bare-metal reproduction

So this is not the final word on the algorithm. It is a strong engineering result with clearly documented boundaries.

## Why I Think The Repo Is Interesting Anyway

Even with the caveats, this repo is useful for a few reasons:

- it does not hide the failed first pass
- it keeps the investigation trail
- it includes both positive and negative results
- it ties the benchmark conclusion to concrete implementation work

That makes it more valuable than a polished benchmark chart without a debugging history.

For people working on ingress, balancing, or benchmark methodology, the repo shows three practical lessons:

1. route-local correctness matters before performance claims do
2. benchmark protocol mistakes can invert an apparent algorithm result
3. load-balancing policies are regime-sensitive, and the honest answer is often conditional rather than universal

## Where To Read Next

- [README.md](README.md) for the public repo overview
- [benchmark/REPORT.md](benchmark/REPORT.md) for the canonical technical conclusion
- [benchmark/PUBLIC_WRITEUP.md](benchmark/PUBLIC_WRITEUP.md) for the shorter benchmark summary
- [benchmark/benchmarking-matrix.md](benchmark/benchmarking-matrix.md) for the frozen campaign record
- [benchmark/investigations/2026-04-19-c2-tail-spike.md](benchmark/investigations/2026-04-19-c2-tail-spike.md) for the methodology failure and fix
- [benchmark/investigations/2026-04-20-regime-pivot.md](benchmark/investigations/2026-04-20-regime-pivot.md) for the regime pivot and cross-environment confirmation
