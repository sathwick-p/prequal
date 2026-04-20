# prequal

`prequal` is a custom Kubernetes ingress controller for experimenting with route-level load-balancing policies, including a Kubernetes adaptation of the Prequal paper's probe-driven hot-cold lexicographic selection.

The short version of the current result is:

- in a paper-aligned regime, `prequal` delivers a large reproducible `p99` latency win over `round-robin` and `least-connections`
- in a small-fleet CPU-bound regime, it does not win and pays overhead
- the current evidence is strong same-host testbed evidence, not final cross-infrastructure proof

The frozen benchmark conclusion lives in [benchmark/REPORT.md](benchmark/REPORT.md). The public-facing benchmark writeup lives in [benchmark/PUBLIC_WRITEUP.md](benchmark/PUBLIC_WRITEUP.md). A longer technical narrative for the repo lives in [TECHNICAL_BLOG.md](TECHNICAL_BLOG.md).

## What This Repo Contains

- a Kubernetes ingress controller written in Go
- route matching by host and path
- pluggable selection policies:
  - `round-robin`
  - `least-connections`
  - `prequal`
- a Rust backend that exposes `/prequal/probe` for server-local probe signals
- Prometheus metrics and benchmark dashboards
- reproducible benchmark assets under [`benchmark/`](benchmark/)

## Current Bounded Claim

What we can support today:

- In the paper-aligned regime, our implementation of `prequal` reduces `p99` latency by `6.8x-8.6x` versus `round-robin` and `least-connections` on the primary `E-B` environment.
- That result reproduced across two same-host cluster topologies.
- On small-fleet CPU-bound workloads, `prequal` is slower and does not improve the tail.

What we are not claiming:

- that `prequal` is universally better
- that this is final production proof across independent environments
- that this controller is a finished ingress product

## Architecture

At a high level, the system has four parts:

1. `controller/`
   Watches `Ingress` and `EndpointSlice` objects and builds in-memory routing state.
2. `server/`
   Matches requests to routes and proxies them to selected backends.
3. `loadbalancer/`
   Implements the selection policies, RIF tracking, latency tracking, probing, and route-scoped probe pools.
4. `backend/`
   Provides a benchmark backend and `/prequal/probe` endpoint used by the `prequal` policy.

The current implementation is intentionally focused on `HTTP/1.1` backends and benchmark-driven evaluation rather than full ingress feature completeness.

## Benchmark Snapshot

Primary evidence is the `E-B` environment: multi-node `kind`, controller isolated on the control-plane, backends spread across workers.

### C2: Heterogeneous Open-Loop

| algorithm | E-B p99 ms | E-B p99.9 ms |
|-----------|-----------:|-------------:|
| **prequal** | **94.20** | **272.38** |
| round-robin | 807.32 | 887.90 |
| least-connections | 802.84 | 1006.78 |

### C3: Heterogeneous Ramp

| algorithm | E-B p99 ms | E-B p99.9 ms |
|-----------|-----------:|-------------:|
| **prequal** | **123.39** | **824.08** |
| round-robin | 831.58 | 1250.41 |
| least-connections | 867.93 | 1596.51 |

![E-B C2 Request Overview](benchmark/results/screenshots/2026-04-20-C2-eb/request-overview.png)

![E-B C3 Request Overview](benchmark/results/screenshots/2026-04-20-C3-eb/request-overview.png)

For the full narrative, caveats, and methodology, read:

- [benchmark/REPORT.md](benchmark/REPORT.md)
- [benchmark/PUBLIC_WRITEUP.md](benchmark/PUBLIC_WRITEUP.md)
- [benchmark/benchmarking-matrix.md](benchmark/benchmarking-matrix.md)

## Quick Start

### Prerequisites

- Go
- Docker
- `kubectl`
- `kind`

### Run tests

```bash
go test ./...
```

### Build the controller

```bash
make build
```

### Build and load into `kind`

```bash
make docker-build
make kind-load
```

### Deploy the controller

```bash
make deploy
```

### Deploy a simple test workload

```bash
make deploy-test
```

### Send a test request through the proxy

```bash
make test-proxy
```

For benchmark-specific setup, use the assets documented in [benchmark/README.md](benchmark/README.md).

## Repo Layout

```text
backend/         Rust benchmark backend with /prequal/probe
benchmark/       frozen evidence, dashboards, scripts, manifests, reports
controller/      Kubernetes reconciliation and route state
deploy/          controller deployment manifests
loadbalancer/    selectors, prober, trackers, route-scoped pools
observability/   Prometheus metrics
probe/           sidecar-related code from earlier experiments
server/          proxy server and transport logic
tree/            route-matching trie
types/           shared model types
```

## Caveat

The most important caveat stays attached to the repo:

> Both environments share the same Docker host. The current benchmark result is strong testbed evidence, not final cross-infrastructure proof.

## Status

The benchmark campaign is currently frozen around `C1`, `C2`, and `C3`. The technical conclusion is stable. Further work, if resumed later, should be framed as strengthening or generalizing the claim, not as establishing the initial result.

## License

[LICENSE](LICENSE)
