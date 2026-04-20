# prequal

A Kubernetes ingress controller that reproduces the NSDI '24 Prequal paper's probe-driven load-balancing algorithm, and the full benchmark evidence trail behind the claim.

The short version: in the regime the paper describes, this implementation reduces `p99` tail latency by roughly `10x` versus `round-robin` and `least-connections`. In a smaller CPU-bound regime, it pays a measurable overhead and does not win. Both results are recorded, both are reproducible, and the investigation log explains why an early version of the same experiment looked catastrophically bad before the methodology was fixed.

![Prequal vs baselines, C2 heterogeneous-open-loop on E-B](benchmark/results/screenshots/2026-04-20-C2-eb/request-overview.png)

The 15 alternating bands above are five runs each of `prequal`, `round-robin`, and `least-connections` on the same 16-backend workload. `p50` latency is identical for all three (the fast service time). Only the tail diverges: `prequal` sits near `60 ms`, both baselines hover around `800 ms`. The entire benefit is in the tail, which is exactly what the paper predicts.

---

## The Bounded Claim

What we can say today, with evidence committed in this repo:

- In the paper-aligned regime (`16` backends, `16x` service-time skew between fast and slow replicas, I/O-bound service time), `prequal` delivers a large and reproducible `p99` advantage over `round-robin` and `least-connections`. Measured `8.6x` on `C2` and `6.8x` on `C3`.
- That advantage reproduces across two cluster topologies on the same host: `E-A` (single-host `kind`, controller colocated with backends) and `E-B` (multi-node `kind`, controller isolated on the control-plane, backends spread across workers).
- In the small-fleet CPU-bound regime (`4` backends, SHA256 workload, 30-VU closed-loop), `prequal` loses by about `25%` on throughput and does not improve the tail. This is kept in the report deliberately.

What we do not claim:

- That `prequal` is universally better.
- That same-host testbed evidence replaces cross-infrastructure proof.
- That this controller is a finished production ingress.

Canonical frozen conclusion: [`benchmark/REPORT.md`](benchmark/REPORT.md). Longer narrative: [`TECHNICAL_BLOG.md`](TECHNICAL_BLOG.md). Public-facing summary: [`benchmark/PUBLIC_WRITEUP.md`](benchmark/PUBLIC_WRITEUP.md).

## Headline Numbers

### `C2` heterogeneous-open-loop, `E-B`, `500` rps, `300s`, 5 reps

| algorithm | p99 ms | p99.9 ms |
|-----------|-------:|---------:|
| **prequal** | **94.20** | **272.38** |
| round-robin | 807.32 | 887.90 |
| least-connections | 802.84 | 1006.78 |

### `C3` heterogeneous-ramp, `E-B`, `100→1500` rps, `370s`, 5 reps

| algorithm | p99 ms | p99.9 ms |
|-----------|-------:|---------:|
| **prequal** | **123.39** | **824.08** |
| round-robin | 831.58 | 1250.41 |
| least-connections | 867.93 | 1596.51 |

Per-backend selection rate on the decisive runs confirms the mechanism: `prequal` drives traffic to the 2 slow replicas down to under `0.1` selections per second each, while the 14 fast replicas share the rest.

## The Reason The Algorithm Works Here

`prequal` picks backends using two signals returned from the backends themselves: requests-in-flight and probed latency. Selection uses hot-cold lexicographic ordering: among the backends in the "cold" RIF quantile it picks the lowest-latency one, and among hot ones it picks the least loaded.

![Algorithm behavior, C2-eb](benchmark/results/screenshots/2026-04-20-C2-eb/algorithm-behavior.png)

The algorithm-behavior dashboard tells the mechanism story. Prequal selects all 14 fast replicas roughly evenly and avoids the 2 slow ones. Round-robin sends `2/16` (12.5%) of traffic into the slow pair and those requests queue to the slow service time, which drags the whole tail. Least-connections does better than round-robin because RIF tracking eventually notices the queue buildup, but late arrivals still pay the full queue cost before the tracker catches up.

![Probe system, C3-eb](benchmark/results/screenshots/2026-04-20-C3-eb/probe-system.png)

The probe-system dashboard is a sanity check. `random_fallback_rate` stays at zero across every `prequal` run. The pool never starves. The win is not an artifact of the algorithm degenerating to random.

## The Honesty Section: Where `prequal` Does Not Win

![C1 controlled, 4-backend CPU-bound, prequal loses](benchmark/results/screenshots/2026-04-19-C1-controlled/request-overview.png)

`C1` is a small-fleet CPU-bound workload: `4` backends all running a SHA256 loop, driven closed-loop by 30 VUs. Under that regime the table inverts.

| algorithm | throughput rps | p99 ms |
|-----------|---------------:|-------:|
| prequal | 4162 | 29.86 |
| round-robin | 5366 | 20.22 |
| least-connections | 4965 | 23.48 |

`prequal` pays about a `25%` throughput deficit and gets a worse tail. The overhead investigation at [`benchmark/investigations/2026-04-20-prequal-overhead-profiling.md`](benchmark/investigations/2026-04-20-prequal-overhead-profiling.md) traces the cost: controller CPU is fine, mutex contention is fine, the probe path costs `5–10 μs` per request inside the controller, and the rest of the gap is diffuse network competition on a shared backend CPU budget. On I/O-bound backends that competition goes away, which is why the pivoted regime wins cleanly.

Reporting this symmetrically matters. `prequal` is a regime-specific algorithm, not a drop-in improvement.

## Why This Repo Is Different From A "Here Is My Prequal Implementation" Repo

Three reasons.

**Methodology was part of the result.** An early 3-rep sequential `C2` run made `prequal` look catastrophically worse than baselines by a factor of `10x` in the wrong direction. That turned out to be a testing error from pool-state leakage across sequential runs. The full trail of competing hypotheses, dead ends, and eventual root cause is in [`benchmark/investigations/2026-04-19-c2-tail-spike.md`](benchmark/investigations/2026-04-19-c2-tail-spike.md). The result only reproduced under a controlled protocol: interleaved algorithm order, controller rollout-restart before every run, and `15s` warmup. That protocol is now baked into [`benchmark/scripts/run_interleaved_campaign.sh`](benchmark/scripts/run_interleaved_campaign.sh).

**Two same-host environments, two matching results.** `E-A` and `E-B` differ in cluster topology (controller placement, pod spread). The advantage ratio shifted from `10.0x` to `8.6x` on `C2`, which is within the noise of either environment. The evidence chain is therefore at least robust to cluster shape, even if not yet to independent hardware.

**Negative results are preserved on purpose.** The `C1` regression is committed, graphed, and reported alongside the positive result. So are the initial failing uncontrolled runs, preserved under their original timestamps for audit.

## Reproduce It

Prerequisites: Go `1.22+`, Docker, `kubectl`, `kind`, `k6`.

```bash
# 1. Create the E-B cluster shape (1 control-plane with extraPortMappings, 2 workers).
kind create cluster --name kind --config benchmark/kind-config-e-b.yaml

# 2. Build and load the controller and backend images into kind.
make docker-build
make kind-load
cd backend && docker build -t prequal-backend:latest . && cd -
kind load docker-image prequal-backend:latest --name kind

# 3. Deploy the benchmark stack.
kubectl apply -f benchmark/manifests/controller-benchmark.yaml
kubectl apply -f benchmark/manifests/workload-heterogeneous.yaml

# 4. Start the supervised port-forward and the observability stack.
benchmark/scripts/port_forward_scrape.sh &
cd benchmark/observability && docker compose up -d && cd -

# 5. Run the frozen C2 campaign (5 reps × 3 algorithms interleaved, ~85 min).
REPS=5 ALGORITHMS="prequal round-robin least-connections" \
SCENARIO=heterogeneous-open-loop-eb \
K6_SCRIPT=benchmark/k6/open_loop.js \
WORKLOAD_MANIFEST=benchmark/manifests/workload-heterogeneous.yaml \
RATE=500 DURATION=300s WORK_ITERATIONS=1000 \
TARGET_URL=http://127.0.0.1:31080/work HOST_HEADER=bench.local \
ENVIRONMENT=E-B-kind-multinode \
INGRESS_NAME=bench-heterogeneous NAMESPACE=prequal-benchmark \
RESET_CONTROLLER=1 POOL_RESET_WARMUP_SEC=15 \
benchmark/scripts/run_interleaved_campaign.sh
```

After the run, `benchmark/results/aggregated/` will contain the per-algorithm `median[min-max]` summaries, and Grafana will have populated dashboards you can re-render with [`benchmark/scripts/render_dashboards.sh`](benchmark/scripts/render_dashboards.sh).

For the smaller scenarios (C1 uniform, C3 ramp) see the corresponding sections in [`benchmark/benchmarking-matrix.md`](benchmark/benchmarking-matrix.md).

## What's Inside

```text
backend/         Rust benchmark backend exposing /work and /prequal/probe
benchmark/       frozen evidence, dashboards, scripts, manifests, reports, investigation logs
controller/      Kubernetes reconciliation for Ingress and EndpointSlice into in-memory router state
deploy/          controller deployment manifests
loadbalancer/    selectors (round-robin, least-connections, prequal/HCL), prober, RIF tracker, route-scoped pools
observability/   Prometheus metrics definitions
probe/           legacy sidecar probe path, kept for historical reference
server/          request-path proxy, transport config, reverse-proxy wiring
tree/            host/path route-matching trie
types/           shared model types
```

The runtime is one Go binary that does both the reconciler and the proxy, plus a small Rust backend used for benchmarking. The backend's `IO_BOUND_MODE=1` env var replaces its SHA256 loop with `tokio::time::sleep(iterations × 50μs)` so probes measure service time rather than CPU contention. That one switch is what flipped the benchmark from "prequal ties" to "prequal wins by `10x` on the tail".

## Caveat

> Both environments share the same Docker host. The current benchmark result is strong testbed evidence, not final cross-infrastructure proof. A stronger public claim would need reproduction on an independent cloud or bare-metal cluster.

That line stays attached to every statement above.

## Read Further

- Paper: [Wydrowski et al., NSDI '24 — Load is not what you should balance: Introducing Prequal](https://www.usenix.org/system/files/nsdi24-wydrowski.pdf)
- Frozen technical conclusion: [`benchmark/REPORT.md`](benchmark/REPORT.md)
- Public summary: [`benchmark/PUBLIC_WRITEUP.md`](benchmark/PUBLIC_WRITEUP.md)
- Longer narrative: [`TECHNICAL_BLOG.md`](TECHNICAL_BLOG.md)
- Full evidence matrix: [`benchmark/benchmarking-matrix.md`](benchmark/benchmarking-matrix.md)
- Why the first `C2` result was wrong: [`benchmark/investigations/2026-04-19-c2-tail-spike.md`](benchmark/investigations/2026-04-19-c2-tail-spike.md)
- How the regime pivot was constructed: [`benchmark/investigations/2026-04-20-regime-pivot.md`](benchmark/investigations/2026-04-20-regime-pivot.md)
- Where the `25%` overhead lives: [`benchmark/investigations/2026-04-20-prequal-overhead-profiling.md`](benchmark/investigations/2026-04-20-prequal-overhead-profiling.md)

## Status

The benchmark campaign is frozen. The algorithm implementation is stable. Further work would go in one of three directions:

1. Reproduction on an independent cloud or bare-metal cluster, to graduate the evidence from "same-host testbed" to full cross-infrastructure.
2. Campaigns `C4` through `C9` from the matrix (multi-route isolation, long-duration, overload, churn, fault injection, external NGINX baseline) — code is in place for all of them, none have been run under the controlled protocol.
3. The three overhead levers flagged in the profiling investigation, which only matter if `prequal` is ever to be a default rather than opt-in.

## License

[LICENSE](LICENSE)
