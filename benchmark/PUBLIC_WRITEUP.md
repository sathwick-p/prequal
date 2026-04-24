# Prequal In Practice: A Regime-Specific Tail-Latency Win

This is the public-facing summary of the frozen prequal benchmark campaign. It is derived from [`REPORT.md`](REPORT.md), which remains the canonical technical conclusion. Published version on Medium: [I built a custom load balancer in Go — the hardest part wasn't the code](https://medium.com/@sathwick.p7/i-built-a-custom-load-balancer-in-go-the-hardest-part-wasnt-the-code-2774a614a062).

**What this is.** A Go reimplementation of **Prequal** (Wydrowski et al., [NSDI '24](https://www.usenix.org/system/files/nsdi24-wydrowski.pdf)), the load-balancing algorithm Google deploys across 20+ services including YouTube's serving stack, packaged as a Kubernetes ingress controller. The repo reimplements the algorithm; it is not Google's production code.

The rest of this document is the frozen benchmark result. Two things are worth flagging before the numbers: the first C2 run made the algorithm look `10×` *worse*, not better, and the investigation log at [`investigations/2026-04-19-c2-tail-spike.md`](investigations/2026-04-19-c2-tail-spike.md) walks the seven competing hypotheses before closing on methodology (a protocol fix produced a `56×` p99 reduction with zero algorithm code changed). Every parameter divergence between this implementation and the paper is tracked in [`paper-divergences.md`](paper-divergences.md).

## TL;DR

Here is the bounded claim we can support:

- In the paper-aligned regime — 16 backends, 16x capacity skew between fast and slow replicas, and I/O-bound service times — our implementation of `prequal` delivers a large and reproducible `p99` tail-latency advantage over both `round-robin` and `least-connections`.
- That advantage reproduced across two cluster topologies on the same host.
- In a small-fleet CPU-bound regime, `prequal` does not win and pays measurable overhead.

This is not a universal-superiority claim. It is a regime-specific engineering result.

## What We Tested

We ended the campaign with two primary comparison environments:

- `E-A`: single-host `kind`, controller colocated on a worker
- `E-B`: multi-node `kind`, controller isolated on the control-plane, backends spread across workers

The primary evidence is from `E-B`, with `E-A` acting as cross-environment confirmation.

The two decisive campaigns were:

- `C2`: heterogeneous open-loop, `500` rps, `300s`, `5` interleaved reps
- `C3`: heterogeneous rate ramp, `100→1500` rps, `370s`, `5` interleaved reps

Both use the paper-aligned regime:

- `16` backends total
- `14` fast replicas + `2` slow replicas
- `16x` service-time skew
- `IO_BOUND_MODE=1`

## Primary Result: E-B

### C2 heterogeneous-open-loop

| algorithm | E-B p99 ms | E-B p99.9 ms |
|-----------|-----------:|-------------:|
| **prequal** | **94.20** | **272.38** |
| round-robin | 807.32 | 887.90 |
| least-connections | 802.84 | 1006.78 |

`prequal` beats the best baseline by **8.6x on p99**.

![C2 E-B Request Overview](results/screenshots/2026-04-20-C2-eb/request-overview.png)

The request-overview dashboard shows the story clearly: throughput is flat at target, median latency stays near the fast service time, and the separation appears almost entirely in the tail.

![C2 E-B Algorithm Behavior](results/screenshots/2026-04-20-C2-eb/algorithm-behavior.png)

The algorithm-behavior dashboard supports the mechanism, not just the outcome: `prequal` keeps selecting the fast pool while avoiding the slow replicas, and `random_fallback_rate` stays at zero.

### C3 heterogeneous-ramp

| algorithm | E-B p99 ms | E-B p99.9 ms |
|-----------|-----------:|-------------:|
| **prequal** | **123.39** | **824.08** |
| round-robin | 831.58 | 1250.41 |
| least-connections | 867.93 | 1596.51 |

`prequal` beats the best baseline by **6.8x on p99**.

![C3 E-B Request Overview](results/screenshots/2026-04-20-C3-eb/request-overview.png)

Under the ramp, the result is the same shape: once the system moves into the stressed regime, `prequal` keeps the `p99` tail far below both baselines.

![C3 E-B Probe System](results/screenshots/2026-04-20-C3-eb/probe-system.png)

The probe-system dashboard shows that the win is not coming from starvation or fallback behavior. The probe pool remains active and `prequal` continues operating in its intended mode.

## Why The Win Is Real

This is not just a dashboard artifact. The supporting evidence chain is consistent:

- per-backend selection shows `prequal` effectively blackholing the two slow replicas
- `random_fallback_rate = 0` on the decisive runs
- throughput is effectively tied across all three algorithms
- `p50` stays at the fast service time for all three algorithms

That means the difference is not “prequal is doing more work overall.” The difference is that it is avoiding the slow replicas, and the gain shows up where it should: the tail.

## Honesty Section: Where Prequal Does Not Win

We kept a deliberately symmetric negative result in the final report.

In `C1` — small-fleet, CPU-bound, `4` backends, SHA256 workload, `30` VUs closed-loop — `prequal` does **not** win:

| algorithm | throughput rps | p99 ms |
|-----------|---------------:|-------:|
| prequal | 4162 | 29.86 |
| round-robin | 5366 | 20.22 |
| least-connections | 4965 | 23.48 |

That is roughly a **25% throughput deficit** versus round-robin, with worse tail latency.

![C1 Controlled Request Overview](results/screenshots/2026-04-19-C1-controlled/request-overview.png)

This matters because it keeps the conclusion honest: `prequal` is not the right default for every regime. On small fleets with CPU-bound backends, the extra probing work adds cost without enough diversity or skew to pay it back.

## Methodology Was Part Of The Result

One of the most important findings from the campaign was methodological, not algorithmic.

An earlier uncontrolled heterogeneous pass made `prequal` look catastrophically worse. That turned out to be a testing error caused by sequential run ordering and pool-state leakage. Once the protocol was fixed to use:

- interleaved run order
- controller reset before every run
- `15s` warmup before measurement
- explicit environment capture

the uncontrolled negative result disappeared. Under the controlled protocol, the algorithm either tied or won depending on the regime.

That is part of the story and should stay in the public writeup. It shows the result survived debugging rather than being accepted uncritically.

## Caveat Kept Verbatim

> **Both environments share the same Docker host.** E-A and E-B differ in cluster topology (controller placement, pod spread) but run on the same Docker Desktop VM, same kernel, same hardware. This is **strong testbed evidence, not final cross-infrastructure proof**. A Claim-Level-B writeup by `public-claim-playbook.md` section 13's standard still wants an independent cloud or bare-metal cluster.

That caveat should remain attached to any public version of the result.

## Conclusion

The strongest honest conclusion is:

- `prequal` is regime-sensitive
- in the paper-aligned regime, it delivers a large reproducible `p99` win
- in the small-fleet CPU-bound regime, it does not
- the current evidence is strong testbed evidence across two same-host cluster topologies, not final cross-infrastructure proof

That is enough for a careful public engineering writeup. It is not enough to claim universal superiority.

## Evidence Pointers

- Canonical technical conclusion: [`REPORT.md`](REPORT.md)
- Frozen matrix: [`benchmarking-matrix.md`](benchmarking-matrix.md)
- Regime pivot investigation: [`investigations/2026-04-20-regime-pivot.md`](investigations/2026-04-20-regime-pivot.md)
- Overhead profiling: [`investigations/2026-04-20-prequal-overhead-profiling.md`](investigations/2026-04-20-prequal-overhead-profiling.md)
