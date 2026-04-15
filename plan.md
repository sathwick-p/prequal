# Prequal: Current Architecture Review and Implementation Plan

## 1. Executive Summary

This project is now best understood as:

- a custom Kubernetes ingress controller
- with pluggable backend-selection algorithms
- using a Kubernetes-friendly adaptation of the Prequal paper
- with backend-exposed probe endpoints for server-local signals
- scoped first to `HTTP/1.1` backends

That is a strong and coherent systems direction.

The architecture is now materially beyond the prototype stage:

- control plane watches Kubernetes objects and reconciles routing state
- data plane matches requests and proxies them to selected backends
- multiple backend-selection algorithms are wired
- asynchronous backend probing exists
- backend-native probe endpoints exist
- bounded probe-pool selection exists
- tests exist for controller, pool, prober, and proxy behavior

This is still not a full ingress-controller product and not yet a paper-faithful end-state Prequal implementation, but the core direction is now solid.

The most important conclusion is:

- the architecture is valid
- the backend-probe direction is the right choice for this project
- the next priority is no longer basic plumbing
- the next priority is completeness, evaluation, and refinement

---

## 2. Current Repository Assessment

### What exists today

- `main.go` wires:
  - informers
  - workqueue
  - controller
  - selectors
  - trackers
  - probe pool
  - async prober
  - proxy server
  - debug server
- `controller/controller.go` reconciles `Ingress` and `EndpointSlice` into:
  - router state
  - backend endpoint state
  - service-to-ingress mappings for resyncs
- `controller/router.go` performs host and path matching using a segment-aware trie in `tree/tree.go`
- `loadbalancer/selector.go` defines the selector interface
- `loadbalancer/roundrobin/rr.go` implements round-robin selection
- `loadbalancer/leastconn.go` implements least-connections using tracked RIF
- `loadbalancer/rif.go` tracks per-backend requests-in-flight
- `loadbalancer/latency.go` tracks per-backend latency medians
- `loadbalancer/pool/pool.go` implements:
  - bounded probe pool
  - HCL-style selection
  - stale-entry purging
  - reuse accounting
- `loadbalancer/prober.go` implements:
  - async request-triggered probes
  - async background probes
  - probe response parsing
  - stale probe rejection
  - probe metrics
- `loadbalancer/config.go` provides tunable probe/pool configuration
- `server/server.go` dispatches by route algorithm and uses backend probes as the authoritative source for the `prequal` path when the prober is active
- `backend/` contains a Rust backend exposing:
  - `/work`
  - `/health`
  - `/prequal/probe`
  - server-local RIF
  - RIF-conditioned latency buckets
- `observability/metrics.go` exports:
  - request metrics
  - controller metrics
  - probe metrics
  - pool occupancy
  - selection algorithm counts
- test coverage exists for:
  - controller
  - router
  - circular buffer
  - RIF tracker
  - latency tracker
  - least-connections
  - probe pool
  - prober
  - proxy server

### What is already good

- informer plus workqueue controller model is correct
- `EndpointSlice` usage is modern and correct
- segment-aware path matching handles Kubernetes `Prefix` semantics correctly
- ingress class handling supports:
  - `spec.ingressClassName`
  - legacy annotation `kubernetes.io/ingress.class`
  - label fallback for compatibility
- route-level algorithm selection exists
- probe-authoritative `prequal` behavior is now implemented when the prober is active
- local completed-request feedback is now fallback-only behavior when the prober is absent
- async probing exists in both request-driven and background forms
- backend-native probe endpoint exists
- stale-probe rejection exists
- stale pool entries are eagerly purged
- probing metrics and tests now exist
- RIF-conditioned latency estimation now exists in the backend in bucketed form

### What is still incomplete or not yet finished

- routing state and backend state are still separate mutable structures
- there is no full ingress-controller operator story yet:
  - no `IngressClass` resource handling
  - no ingress status updates
  - no TLS support
  - no production-oriented deployment/operator story
- the probe contract exists in code, but is not yet formalized as a documented stable interface
- the current Prequal adaptation is still simplified relative to the paper:
  - bucketed RIF-conditioned latency is an approximation
  - probe/removal policy is configurable but still simplified
  - reuse/removal behavior is not yet justified against measured outcomes
- algorithm and probing behavior have not yet been comprehensively evaluated under load

---

## 3. Project Scope

### Final target

The end goal is a real custom ingress controller.

That means the project should eventually support:

- standard Kubernetes ingress behavior
- correct host and path routing semantics
- ingress class ownership
- robust endpoint discovery and reconciliation
- stable proxying behavior
- operator-facing observability
- pluggable balancing policies

### Algorithm target

The balancing goal is specifically a Kubernetes adaptation of the Prequal paper:

- use RIF and latency, not CPU, as the main decision signals
- use HCL rather than a linear combination of latency and RIF
- use bounded probe pools
- sample backend load information via active probes

### Signal source decision

The intended signal source is now implemented as:

- backend services expose a lightweight probe endpoint
- the endpoint returns server-local RIF and latency estimate
- the ingress proxy asynchronously collects probe responses
- the probe pool is fed by backend-reported observations

Operational policy:

- when the prober is active, backend probes are authoritative for the `prequal` path
- local completed-request observations are fallback/debug signal only
- when the prober is absent, local observations may bootstrap/fallback the pool

### Protocol scope

The initial target remains `HTTP/1.1` backends only.

This is the correct first scope because:

- backend probe endpoints are easy to define over HTTP
- request-level accounting is straightforward
- ingress correctness can be developed without solving multiplexed protocols

Explicit non-goal for the current phase:

- do not treat `HTTP/2` or gRPC as solved

---

## 4. Architecture Judgment

### What to keep

- control-plane/data-plane split
- informer + workqueue controller model
- in-memory routing state
- `EndpointSlice`-driven backend discovery
- selector abstraction
- independent per-proxy decision-making with no shared balancing state across proxies
- backend-native probe endpoints as the server-local signal source

### What to improve next

- move toward explicit internal models instead of loosely coupled mutable structures
- formalize the probe API contract and version it in docs
- continue refining paper fidelity based on measured behavior
- keep the data plane correct when probes are missing, stale, or failing

### Target package shape

The code should continue moving toward these conceptual boundaries:

- `controller/`
  - watches, queue workers, reconciliation
- `routing/`
  - route model, precedence, host/path matching
- `discovery/`
  - endpoint normalization, backend metadata
- `balancer/`
  - selector interface, round robin, least-connections, Prequal
- `proxy/`
  - request forwarding, transport behavior, per-backend accounting
- `signals/`
  - probe client, probe ingestion, RIF tracking, latency estimation
- `observability/`
  - metrics, logs, debug, health

### Multi-proxy operation model

Each ingress proxy instance should continue to operate independently.

That means:

- each proxy keeps its own local balancing state
- each proxy maintains its own probe pool
- there is no shared coordination layer between proxies
- backends expose server-local signals, but selection decisions remain proxy-local

This preserves:

- horizontal scalability
- lower coordination overhead
- simpler failure domains
- closer alignment with the paper’s distributed shape

---

## 5. Remaining Work

### 5.1 Ingress-controller completeness

Still missing:

- `IngressClass` resource handling
- ingress status updates
- TLS support
- more production-oriented manifests and deployment story

### 5.2 State model cleanup

The current controller/runtime shape works, but it will remain awkward as the project grows unless state ownership is clarified.

Still desirable:

- explicit `Route`
- explicit `BackendSet`
- explicit `EndpointState`
- explicit `SelectionPolicy`
- explicit `ProbeObservation`

These types already exist as migration-target vocabulary and should eventually be used more directly.

### 5.3 Probe contract formalization

The backend probe API exists in code, but it should be documented as an explicit contract:

- endpoint path
- expected status codes
- JSON schema
- timestamp semantics
- timeout expectations
- freshness semantics
- future-compatible fields

### 5.4 Paper-fidelity refinement

The current design is directionally correct, but still simplified relative to the paper.

Remaining refinement areas:

- justify the current RIF bucket scheme with experiments
- justify current probe rates and background interval with experiments
- justify current removal/reuse policy with experiments
- clarify how closely the current HCL and pool-management behavior should track the paper versus remain pragmatic

### 5.5 Evaluation and benchmarking

The largest remaining gap is now measured validation.

You need evidence for:

- correctness under load
- comparative behavior between algorithms
- tail-latency improvements
- probe overhead
- stability under churn and failures

This is now the highest-value next area of work.

---

## 6. Backend Probe Endpoint Plan

### Goal

Expose backend-side server-local signals in a lightweight and testable way.

Current backend responsibilities:

- increment RIF when request work starts
- decrement RIF when request work completes
- record request latency
- maintain RIF-conditioned latency buckets
- expose `/prequal/probe`

### Probe response

Current fields:

- `rif`
- `latency_median_ms`
- `timestamp_ms`

Recommended future extension fields:

- `sample_count`
- `latency_p90_ms`
- `healthy`
- `signal_age_ms`

### Proxy responsibilities

The proxy currently:

- probes backends asynchronously
- rejects stale or invalid probe responses
- adds successful probe responses to the pool
- uses probe-authoritative selection for `prequal`
- falls back to simpler behavior when probe information is insufficient

This is the correct direction and should be preserved.

---

## 7. Immediate Priorities

According to the current reality of the codebase, the next most useful priorities are:

1. Keep `plan.md` and architecture docs aligned with the implementation.
2. Formalize the backend probe API contract in writing.
3. Build a full benchmarking/load-testing plan and execute it.
4. Compare:
   - round robin
   - least-connections
   - current `prequal`
5. Measure:
   - median latency
   - p95/p99/p99.9 latency
   - error rate
   - throughput
   - probe success/failure behavior
   - pool occupancy and freshness
6. Use those results to decide what paper-fidelity refinements are worth implementing next.

---

## 8. Final Guidance

The strongest description of the project now is:

- a custom ingress controller
- `HTTP/1.1` first
- backend-exposed probe endpoints for server-local signals
- Prequal-inspired backend selection implemented in the ingress proxy

The biggest mistakes to avoid now are:

- letting docs drift behind the code again
- making paper-fidelity changes before measuring the current design
- optimizing policy details before doing serious load testing and benchmarks

The right order from here is:

1. keep the implementation stable
2. document the probe contract clearly
3. benchmark the system thoroughly
4. refine algorithm details based on measured results
