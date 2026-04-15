# Prequal: Updated Architecture Review and Detailed Implementation Plan

## 1. Executive Summary

This project is moving in a good direction, but the right framing is now:

- build a standards-aware custom Kubernetes ingress controller first
- make the data plane support pluggable backend-selection algorithms
- implement a Kubernetes-friendly adaptation of the Prequal paper on top of that core
- use backend-exposed probe endpoints for server-local signals
- scope the first serious implementation to `HTTP/1.1` backends

That is a valid and ambitious systems project.

The architecture is sound as a foundation:

- control plane watches Kubernetes objects and reconciles desired routing state
- data plane matches requests and proxies them to selected backends
- backend selection can evolve from round robin to RIF-aware and then Prequal-style selection
- backend probe endpoints can upgrade signal quality from proxy-local approximation to server-local measurements

The current repository is no longer just a toy prototype:

- it watches `Ingress` and `EndpointSlice`
- it builds an in-memory route table
- it maintains backend endpoint state
- it proxies live traffic to discovered backends
- it already has a selector abstraction
- it already supports multiple algorithm paths
- it already has basic Prometheus metrics
- it already has unit and integration-style tests

But it is still not a real ingress controller yet, and it is not a real Prequal implementation yet.

The most important conclusion is:

- the project idea is good
- the architecture is valid
- the end goal is realistic
- the next priority is moving from proxy-local approximation toward real backend probing while continuing to improve ingress-controller completeness

---

## 2. Correct Assessment Of The Current Repository

### What exists today

- `main.go` wires informers, queue, controller, proxy server, selectors, trackers, and debug server
- `controller/controller.go` reconciles `Ingress` and `EndpointSlice` into:
  - router state
  - backend endpoint state
  - service-to-ingress mappings for resyncs
- `controller/router.go` performs host and path matching using a segment-aware trie in `tree/tree.go`
- `loadbalancer/selector.go` defines a selector interface
- `loadbalancer/roundrobin/rr.go` implements round-robin backend selection
- `loadbalancer/leastconn.go` implements least-connections backend selection using RIF
- `loadbalancer/rif.go` tracks per-backend requests-in-flight via atomic counters
- `loadbalancer/latency.go` tracks per-backend latency via circular buffer with median estimation
- `loadbalancer/pool/pool.go` implements a bounded probe pool with HCL-inspired selection
- `server/server.go` dispatches backend selection by route algorithm and uses pool-based HCL selection for the `prequal` path
- `observability/metrics.go` exports Prometheus metrics
- controller, router, and proxy tests exist

### What is already good

- informer plus workqueue controller model is correct
- `EndpointSlice` usage is the right modern Kubernetes choice
- the split between controller, router, backend store, selector, and proxy is sensible
- segment-aware trie handles Kubernetes `Prefix` matching correctly
- ingress class handling now supports:
  - `spec.ingressClassName`
  - legacy annotation `kubernetes.io/ingress.class`
  - non-standard label fallback for compatibility
- per-backend RIF tracking and latency estimation are implemented
- HCL-inspired selection with bounded probe pool is operational
- route-level algorithm selection exists
- tests and metrics exist across controller, router, and proxy

### What was recently added

- async probing (`loadbalancer/prober.go`) wired into request path and background loop
- Rust backend (`backend/`) with `/prequal/probe` returning server-local RIF and latency
- the `prequal` path uses both virtual probes (from completed requests) and real backend probes (async)
- probe freshness checked via backend-reported timestamps
- prober rejects non-200 probe responses

### What is still incomplete or incorrect

- routing state and backend state are published as separate mutable structures
- there is no full ingress-controller operator story yet:
  - no `IngressClass` resource handling
  - no ingress status updates
  - no TLS support
  - no production-oriented service exposure model
- no RIF-conditioned latency estimation matching the paper
- no configurable probe rates and removal policies matching the paper
- no explicit stale-probe ingest policy beyond timestamp-based pool cleanup
- no test coverage for the probing path (probe generation, response parsing, freshness handling, failure behavior)
- no probing observability metrics (probe success/fail counts, pool occupancy)

---

## 3. Project Scope

### Final target

The end goal is a real custom ingress controller.

That means this project should eventually support:

- standard Kubernetes ingress behavior
- correct host and path routing semantics
- ingress class ownership
- robust endpoint discovery and reconciliation
- stable proxying behavior
- operator-facing observability
- pluggable balancing policies

### Algorithm target

The balancing goal is not generic "smart load balancing." It is specifically a Kubernetes adaptation of the Prequal paper:

- use RIF and latency, not CPU, as the primary decision signals
- use HCL rather than a linear combination of latency and RIF
- use bounded probe pools
- sample backend load information via active probes

### Signal source decision

The intended long-term signal source is now:

- backend services expose a lightweight probe endpoint
- that endpoint returns server-local RIF and latency estimate
- the ingress proxy collects probe responses and feeds them into the pool

The current implementation is a temporary approximation:

- each proxy observes client-local RIF and latency from its own requests
- each proxy seeds the pool from random backends
- each proxy feeds completed-request observations back into the pool

This is useful for early algorithm development, but it is not the final design.

### Protocol scope

The initial target is `HTTP/1.1` backends only.

This is the correct first scope because:

- backend probe endpoints are easy to define over HTTP
- request inference is much more feasible than for `HTTP/2` or gRPC
- ingress correctness can be developed without immediately solving multiplexed protocols

Explicit non-goal for the first phase:

- do not treat `HTTP/2` and gRPC as solved

---

## 4. Architecture Judgment

### What to keep

- control-plane/data-plane split
- informer + workqueue controller model
- in-memory routing state
- `EndpointSlice`-driven backend discovery
- selector abstraction
- independent per-proxy decision-making with no shared balancing state across proxies

### What to change

- move toward explicit internal models instead of coupling everything directly to Kubernetes object details
- publish route and backend state more coherently
- move from proxy-local approximation to backend probe integration
- keep the data plane correct even when probe data is missing or stale

### Target package shape

You do not need to refactor everything immediately, but the code should move toward these boundaries:

- `controller/`
  - watches, queue workers, reconciliation
- `routing/`
  - route model, host/path matching, precedence rules
- `discovery/`
  - endpoint normalization, endpoint metadata
- `balancer/`
  - selector interface, round robin, least-connections, Prequal
- `proxy/`
  - request forwarding, transport behavior, per-backend accounting
- `signals/`
  - probe client, probe ingestion, RIF tracking, latency estimation
- `observability/`
  - metrics, logs, debug, health

### Multi-proxy operation model

Each ingress proxy instance should operate independently.

That means:

- each proxy keeps its own local in-memory balancing state
- each proxy maintains its own probe pool
- there is no shared global coordination layer for backend selection
- backends expose server-local signals, but selection decisions remain proxy-local

This is an important architectural property because it:

- avoids coordination overhead between proxies
- preserves horizontal scalability
- stays closer to the distributed spirit of the Prequal paper
- makes failure domains simpler

---

## 5. Critical Issues To Fix Or Improve

### 5.1 Ingress-controller completeness

The project is moving beyond routing correctness and now needs controller completeness work.

Still missing:

- `IngressClass` resource handling
- ingress status updates
- TLS support
- production-oriented service exposure and deployment story

### 5.2 State publication model

Right now route state and backend state are updated separately.

That is workable for the current code, but it will become fragile once you add:

- backend probe metadata
- staleness handling
- multiple policies
- more reconciliation paths

Move toward explicit domain models such as:

- `Route`
- `BackendRef`
- `BackendSet`
- `EndpointState`
- `SelectionPolicy`
- `ProbeObservation`

### 5.3 Current virtual-probe limitation

The current proxy-local HCL path is a useful intermediate step, but it is not yet faithful to the paper's sampling model.

Current behavior:

- the pool is seeded from random backends on each request
- completed requests also feed observations back into the pool
- selection still depends on proxy-local observations, not backend-reported state

Implication:

- the current implementation should be treated as an HCL-inspired approximation, not a full Prequal reproduction

### 5.4 Missing active probing

The biggest remaining algorithm gap is the absence of real probing.

Still missing:

- background goroutines that issue probe requests
- backend probe timeout handling
- integration of probe responses into the pool
- probe freshness and staleness handling
- probe error handling

### 5.5 Missing backend-native signal contract

The backend probe API has not been defined yet.

Before implementing probes, define exactly what a backend returns.

At minimum:

- `rif`
- `latency_median_ms`
- `timestamp_ms`

Possibly also:

- `sample_count`
- `latency_p90_ms`
- `healthy`
- `signal_age_ms`

### 5.6 Paper fidelity gaps

Even after backend probing is added, there are still paper-level details that need attention:

- RIF-conditioned latency estimation
- configurable probe rate
- configurable removal rate
- reuse budget behavior matching the paper more closely
- clear handling of stale observations

### 5.7 Missing probing-path test coverage

The probing path is now part of the core architecture, not an optional experiment.

That means correctness no longer depends only on:

- route matching
- endpoint discovery
- proxy forwarding

It also depends on:

- probe request generation
- probe response parsing
- probe freshness handling
- probe ingestion into the pool
- selection behavior when probes are missing, stale, or failing

This area needs direct unit and integration tests.

---

## 6. Backend Probe Endpoint Plan

### Goal

Build a small backend-side service or embedded handler that exposes server-local signals for probing.

The backend probe endpoint should provide:

- server-local request RIF
- recent latency estimate
- timestamp of the measurement

### Why this is the right direction

This is closer to the paper than sidecar/eBPF for your project:

- the paper assumes server-side load reporting
- you avoid kernel/eBPF complexity
- you get explicit request-level semantics
- the probe contract is easier to test and reason about

### Probe response contract

The recommended initial response payload is:

```json
{
  "rif": 3,
  "latency_median_ms": 12,
  "timestamp_ms": 1710000000000
}
```

Recommended future extensions:

```json
{
  "rif": 3,
  "latency_median_ms": 12,
  "latency_p90_ms": 25,
  "sample_count": 64,
  "healthy": true,
  "timestamp_ms": 1710000000000
}
```

### Backend responsibilities

Each backend should:

- increment RIF when a request starts
- decrement RIF when a request completes
- record request durations
- expose recent latency summary
- return probe data quickly and cheaply

### Latency estimation roadmap

The current backend returns a single rolling median latency.

That is a reasonable first step, but it is not yet faithful to the paper.

The paper's idea is:

- latency should be estimated at or near the backend's current `RIF`
- not as one unconditional median across all recent traffic

Why:

- latency is load-dependent
- a backend may be fast at low `RIF` and much slower at high `RIF`
- one global median can hide the actual response-time curve under load

Recommended next implementation:

- bucket completed-request latencies by `RIF` at request arrival
- examples:
  - `0`
  - `1`
  - `2-3`
  - `4-7`
  - `8+`
- on probe:
  - read current `RIF`
  - choose the nearest relevant bucket
  - return the median from that bucket

This gives you a practical approximation of RIF-conditioned latency without requiring a complex model.

### Proxy responsibilities

The ingress proxy should:

- sample backends uniformly at random for probes
- call backend probe endpoints asynchronously
- store probe responses in the local probe pool
- use those responses for HCL selection
- fall back safely when probes are missing or stale

### Probe freshness policy

Probe freshness is now an explicit architectural concern.

The system should define:

- maximum acceptable probe age at ingest
- maximum acceptable probe age at selection time
- fallback behavior when too many probes are stale
- handling for suspicious or skewed backend timestamps

Recommended policy:

- if a probe response timestamp is older than the allowed threshold relative to local proxy time, discard it on ingest
- if cleanup leaves the pool below a safe occupancy threshold, fall back to a simpler selection path
- export metrics for stale probe drops and stale pool occupancy

This keeps the proxy from selecting backends based on obsolete load data.

### Probe rate and removal policy

The current system has the basic mechanics, but the rates are still effectively fixed.

These should become explicit configuration values:

- probes per request
- background idle probe interval
- pool max age
- pool max size
- reuse budget
- remove-worst rate
- QRIF threshold

Why this matters:

- too few probes causes stale decisions
- too many probes adds overhead
- too little removal keeps biased or stale entries
- too much removal empties the pool too often

The goal is to make these tunable so experiments can be run without code changes.

### Transitional strategy

Until backend probing is fully implemented:

- keep the current proxy-local trackers
- keep the current random seeding and virtual-probe feedback
- use that path only as a temporary approximation

Once backend probes exist:

- populate the pool primarily from backend probe responses
- keep local request observations only as optional supplementary signal

Recommended direction from here:

- backend probe responses should gradually become the authoritative source for the `prequal` path
- local request observations should remain a fallback or supplementary freshness aid, not the primary source forever

---

## 7. Detailed Next Steps

This is the concrete work that should happen next.

### Step 1: Update the plan and remove sidecar direction (DONE)

- sidecar/eBPF removed as intended architecture direction
- backend-native probe endpoints adopted instead
- `probe/probe.go` remains as deprecated experiment

### Step 2: Define the backend probe API (DONE)

- probe path: `/prequal/probe`
- JSON schema: `{"rif": int, "latency_median_ms": float, "timestamp_ms": uint}`
- probe endpoint lives in the backend service directly

### Step 3: Implement a minimal backend probe service (DONE)

- Rust backend in `backend/` with axum
- tracks in-flight requests via AtomicI64
- tracks recent latencies via circular buffer with median
- exposes `/prequal/probe`, `/work`, `/health`
- `WORK_MULTIPLIER` env var for simulating heterogeneous hardware

### Step 4: Add async probe collection in the proxy (DONE)

- `loadbalancer/prober.go` with `TriggerProbes` (per-request) and `Run` (background loop)
- probes random backends uniformly
- rejects non-200 responses
- uses backend-reported timestamps for freshness
- wired into `server.go` and `main.go`

### Step 5: Make pool population probe-driven

Shift the pool toward real probe responses.

Actions:

- reduce dependence on virtual probe feedback
- use backend probe responses as the primary pool input
- define how old entries expire
- decide how local observations should supplement or not supplement the pool

Deliverable:

- pool population mainly reflects backend-reported state

### Step 6: Add an explicit stale-probe policy

Do not leave freshness as an implicit byproduct of timestamps and cleanup alone.

Actions:

- define max accepted probe age on ingest
- define max accepted probe age during selection
- reject backend probe responses with clearly stale timestamps
- define fallback behavior when the fresh pool is too small
- export metrics for stale probe drops

Deliverable:

- a documented and testable freshness policy

### Step 7: Make probe/removal behavior configurable

Move fixed algorithm constants into config.

Actions:

- make probes-per-request configurable
- make background probe interval configurable
- make pool max age configurable
- make QRIF configurable
- make reuse limit configurable
- make remove-worst cadence or rate configurable

Deliverable:

- the probing path can be tuned experimentally without code edits

### Step 8: Implement RIF-conditioned latency estimation

Move the backend closer to the paper's latency model.

Actions:

- record request latency together with the RIF level at request arrival
- choose a practical bucketing scheme for RIF
- compute medians per bucket
- update the backend probe endpoint to return latency estimated for current RIF
- document the approximation clearly

Deliverable:

- backend-reported latency reflects current load more faithfully than a global rolling median

### Step 9: Improve algorithm and probing tests

Current tests are useful, but the algorithm layer needs more specific coverage.

Add tests for:

- annotation-driven algorithm dispatch
- unknown algorithm fallback
- least-connections behavior under uneven RIF
- HCL behavior on synthetic pool states
- stale probe handling
- empty/low-occupancy pool fallback
- successful backend probe ingestion
- non-200 probe responses
- malformed probe JSON
- timestamp handling
- background probing shutdown behavior

Deliverable:

- algorithm-specific confidence, not just proxy smoke tests

### Step 10: Improve state modeling

The current controller shape works, but probe integration will add complexity.

Actions:

- define explicit route and backend models
- reduce coupling between controller internals and proxy runtime internals
- decide where probe metadata lives

Deliverable:

- clearer ownership between reconciliation, routing, and balancing state

### Step 11: Add observability for probing

Probing is hard to reason about without visibility.

Add metrics for:

- probes sent
- probes succeeded
- probes timed out
- pool occupancy
- pool age distribution
- selection algorithm counts
- stale probe drops

Deliverable:

- enough visibility to debug probe behavior under load

### Step 12: Improve ingress-controller completeness

Keep the product direction moving, not just the algorithm.

Actions:

- add `IngressClass` handling
- add ingress status management
- add TLS roadmap
- improve deployment manifests

Deliverable:

- progress toward a real ingress controller, not just a research proxy

### Step 13: Evaluate algorithm behavior under load

Eventually you need evidence, not only design.

Compare:

- round robin
- least-connections
- current proxy-local HCL approximation
- probe-driven Prequal path

Measure:

- median latency
- tail latency
- error rate
- fairness
- sensitivity to uneven backend load

Deliverable:

- evidence that the probe-driven path is better than the approximation and better than simpler policies

---

## 8. Recommended Immediate Priorities

If you want the most sensible order from here, do this:

1. Define the backend probe contract.
2. Implement a minimal backend probe endpoint.
3. Add async probe collection in the proxy.
4. Feed backend probe responses into the pool.
5. Add an explicit stale-probe policy.
6. Add tests and metrics around probing.
7. Make probe/removal behavior configurable.
8. Then iterate on RIF-conditioned latency and paper fidelity details.

In parallel, continue ingress-controller completeness work, but do not block the probing architecture on full controller maturity.

---

## 9. Final Guidance

The strongest version of this project is now:

- a real custom ingress controller as the end goal
- `HTTP/1.1` first
- Prequal-inspired backend selection as the advanced policy layer
- backend-exposed probe endpoints as the mechanism for server-local signals

The biggest mistakes to avoid now are:

- keeping the docs pointed at sidecar/eBPF after deciding against it
- treating the current proxy-local approximation as if it were already full Prequal
- adding too much probe complexity before defining the backend probe contract clearly

The right order is:

1. keep the ingress core stable
2. define the probe contract
3. add real async backend probing
4. make the pool probe-driven
5. evaluate and refine paper fidelity
