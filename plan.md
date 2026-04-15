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

### What is still incomplete or incorrect

- routing state and backend state are published as separate mutable structures
- there is no full ingress-controller operator story yet:
  - no `IngressClass` resource handling
  - no ingress status updates
  - no TLS support
  - no production-oriented service exposure model
- the `prequal` path is still based on proxy-local signals plus local random pool seeding
- there is still no async probing loop to backend services
- there is still no backend-native probe endpoint integration
- there is no server-local signal source in the current request path
- there is no RIF-conditioned latency estimation matching the paper
- there are no configurable probe rates and removal policies matching the paper

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

### Proxy responsibilities

The ingress proxy should:

- sample backends uniformly at random for probes
- call backend probe endpoints asynchronously
- store probe responses in the local probe pool
- use those responses for HCL selection
- fall back safely when probes are missing or stale

### Transitional strategy

Until backend probing is fully implemented:

- keep the current proxy-local trackers
- keep the current random seeding and virtual-probe feedback
- use that path only as a temporary approximation

Once backend probes exist:

- populate the pool primarily from backend probe responses
- keep local request observations only as optional supplementary signal

---

## 7. Detailed Next Steps

This is the concrete work that should happen next.

### Step 1: Update the plan and remove sidecar direction

Done conceptually, but this should remain consistent across the repo.

Actions:

- remove sidecar/eBPF as the intended architecture direction
- treat `probe/probe.go` as deprecated experiment or remove it later
- align comments and docs with backend-native probe endpoints

### Step 2: Define the backend probe API

Before implementing probing logic, define the interface.

Actions:

- choose probe path, for example `/prequal/probe`
- define JSON schema
- define timeout expectations
- define meaning of each field
- decide whether probe endpoint lives in the backend service directly or a tiny co-deployed helper process

Deliverable:

- written probe contract with example request/response

### Step 3: Implement a minimal backend probe service

Build a small backend implementation that exposes real server-local signals.

Actions:

- track in-flight requests
- track recent latencies
- expose probe endpoint
- add tests for probe responses

Deliverable:

- one backend that can be probed by the ingress proxy

### Step 4: Add async probe collection in the proxy

This is the biggest technical next step.

Actions:

- add background probe goroutines
- select backend targets uniformly at random
- call probe endpoints with short timeouts
- parse probe responses
- feed them into the pool
- keep probe logic isolated from request forwarding logic

Deliverable:

- live backend probe responses entering the pool

### Step 5: Make pool population probe-driven

Shift the pool toward real probe responses.

Actions:

- reduce dependence on virtual probe feedback
- use backend probe responses as the primary pool input
- define how old entries expire
- decide how local observations should supplement or not supplement the pool

Deliverable:

- pool population mainly reflects backend-reported state

### Step 6: Improve algorithm tests

Current tests are useful, but the algorithm layer needs more specific coverage.

Add tests for:

- annotation-driven algorithm dispatch
- unknown algorithm fallback
- least-connections behavior under uneven RIF
- HCL behavior on synthetic pool states
- stale probe handling
- empty/low-occupancy pool fallback

Deliverable:

- algorithm-specific confidence, not just proxy smoke tests

### Step 7: Improve state modeling

The current controller shape works, but probe integration will add complexity.

Actions:

- define explicit route and backend models
- reduce coupling between controller internals and proxy runtime internals
- decide where probe metadata lives

Deliverable:

- clearer ownership between reconciliation, routing, and balancing state

### Step 8: Add observability for probing

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

### Step 9: Improve ingress-controller completeness

Keep the product direction moving, not just the algorithm.

Actions:

- add `IngressClass` handling
- add ingress status management
- add TLS roadmap
- improve deployment manifests

Deliverable:

- progress toward a real ingress controller, not just a research proxy

### Step 10: Evaluate algorithm behavior under load

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
5. Add tests and metrics around probing.
6. Then iterate on paper fidelity details.

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
