# Prequal: Corrected Architecture Review and Implementation Plan

## 1. Executive Summary

This project is moving in a good direction, but the right framing is:

- build a standards-aware custom Kubernetes ingress controller first
- make the data plane support pluggable backend-selection algorithms
- implement a Kubernetes-friendly adaptation of the Prequal paper on top of that core
- use a sidecar plus eBPF to avoid application source-code changes
- scope the first serious implementation to `HTTP/1.1` backends

That is a valid and ambitious systems project.

The architecture is sound as a foundation:

- control plane watches Kubernetes objects and reconciles desired routing state
- data plane matches requests and proxies them to selected backends
- backend selection can evolve from round robin to RIF-aware and then Prequal-style selection
- sidecar instrumentation can upgrade signal quality without requiring application changes

The current repository is no longer just a toy prototype:

- it watches `Ingress` and `EndpointSlice`
- it builds an in-memory route table
- it maintains backend endpoint state
- it proxies live traffic to discovered backends
- it already has a selector abstraction
- it already has round-robin selection
- it already has basic Prometheus metrics
- it already has unit and integration-style tests

But it is still not a real ingress controller yet, and it is not a real Prequal implementation yet.

The most important conclusion is:

- the project idea is good
- the architecture is valid
- the end goal is realistic
- the next priority is ingress correctness and protocol-correct signal collection, not advanced balancing heuristics

---

## 2. Correct Assessment Of The Current Repository

### What exists today

- `main.go` wires informers, queue, controller, proxy server, and debug server
- `controller/controller.go` reconciles `Ingress` and `EndpointSlice` into:
  - router state
  - backend endpoint state
  - service-to-ingress mappings for resyncs
- `controller/router.go` performs host and path matching using a radix tree
- `loadbalancer/selector.go` defines a selector interface
- `loadbalancer/roundrobin/rr.go` implements round-robin backend selection
- `server/server.go` proxies requests using the selected backend
- `observability/metrics.go` exports basic Prometheus metrics
- `probe/probe.go` provides an early sidecar-style observer
- controller and proxy tests already exist

### What is already good

- informer plus workqueue controller model is correct
- `EndpointSlice` usage is the right modern Kubernetes choice
- the split between controller, router, backend store, selector, and proxy is sensible
- a separate algorithm interface already exists
- round robin is already implemented
- the project already has tests and basic metrics, which is better than an empty prototype

### What is still incomplete or incorrect

- ingress semantics are not fully standards-correct yet
- ingress class support is incomplete and uses a non-standard label fallback
- path matching does not fully match Kubernetes `Prefix` behavior
- exact-route fallback behavior is not correct
- routing state and backend state are published as separate mutable structures
- there is no full ingress-controller operator story yet:
  - no `IngressClass` resource handling
  - no ingress status updates
  - no TLS support
  - no production-oriented service exposure model
- the sidecar currently counts TCP socket state, which is not equivalent to request-level RIF
- there is no real Prequal probe pool, HCL rule, async probing loop, or request-aware latency model yet

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
- use proxy-local signals (client-local RIF and latency observed from completed requests)

### Signal source decision

The current implementation uses proxy-local signals only — no eBPF sidecar, no async probe RPCs to backends. Each proxy observes RIF and latency from its own requests and feeds "virtual probes" into the pool.

This is a deliberate simplification:

- with a single ingress replica, client-local signals are identical to server-local signals
- the paper shows client-local RIF-based selection already significantly outperforms round-robin
- the full Prequal algorithm (probe pool, HCL, pool management) works identically regardless of signal source
- eBPF sidecar and async probing can be added later as a signal quality upgrade without changing the algorithm

If server-local signals are needed later (multiple ingress replicas, backends receiving external traffic), the pool's `Add` method accepts entries from any source — plug in a probe endpoint or eBPF sidecar without changing the selection logic.

### Protocol scope

The initial target is `HTTP/1.1` backends only.

This is the correct first scope because:

- request inference is much more feasible than for `HTTP/2` or gRPC
- keep-alive still exists, but request boundaries are easier to reason about
- the sidecar can be useful earlier
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
- sidecar as an optional signal-upgrade layer
- independent per-proxy decision-making with no shared balancing state across proxies

### What to change

- move toward explicit internal models instead of coupling everything directly to Kubernetes object details
- fix routing semantics before more algorithm work
- publish route and backend state more coherently
- make the sidecar request-aware rather than connection-aware
- keep the data plane correct even when no sidecar is present

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
  - RIF tracking, latency estimation, sidecar integration, probe handling
- `observability/`
  - metrics, logs, debug, health

### Multi-proxy operation model

Each ingress proxy instance should operate independently.

That means:

- each proxy keeps its own local in-memory balancing state
- each proxy maintains its own probe pool
- there is no shared global coordination layer for backend selection
- sidecar responses expose server-local signals, but selection decisions remain proxy-local

This is an important architectural property because it:

- avoids coordination overhead between proxies
- preserves horizontal scalability
- stays closer to the distributed spirit of the Prequal paper
- makes failure domains simpler

---

## 5. Critical Issues To Fix First

### 5.1 Ingress semantics

The project wants to become a real ingress controller, so standards correctness matters.

You should support:

- `spec.ingressClassName`
- legacy annotation `kubernetes.io/ingress.class`
- eventually `IngressClass` resources

You should not depend on:

- `metadata.labels["ingress.class"]` as a primary compatibility mechanism

### 5.2 Kubernetes path matching correctness

Current routing is based on radix longest-prefix lookup, but Kubernetes `Prefix` matching is path-element aware, not raw byte-prefix matching.

That means:

- `/api` should match `/api` and `/api/...`
- `/api` should not match `/apiv2`

This needs to be fixed early because otherwise the controller is not ingress-correct.

### 5.3 Exact vs prefix fallback behavior

If the longest raw prefix is an `Exact` route that does not exactly match the request path, the router should still be able to fall back to a shorter valid prefix route where appropriate.

That behavior is currently too naive and should be corrected before advanced selector work.

### 5.4 State publication model

Right now route state and backend state are updated separately.

That is workable for the current code, but it will become fragile once you add:

- richer route semantics
- policy selection
- backend metadata
- signal-driven selection
- multiple concurrent reconciliation events

Move toward explicit domain models such as:

- `Route`
- `BackendRef`
- `BackendSet`
- `EndpointState`
- `SelectionPolicy`

### 5.5 Sidecar signal fidelity

This is the biggest conceptual risk in the Prequal adaptation.

The reason for sidecar plus eBPF is valid:

- avoid requiring application code changes
- make signal collection deployable across arbitrary workloads

But for the sidecar to be useful, it should aim to infer request start and response completion, not just socket presence.

Why:

- TCP `ESTABLISHED` count is not request RIF
- with `HTTP/1.1` keep-alive, one socket may be idle or active
- connection count may undercount or misrepresent true concurrent in-flight requests
- latency cannot be estimated correctly from socket presence alone

This point must be treated as a core architectural requirement, not a nice-to-have.

---

## 6. Sidecar And eBPF Plan

### Goal

Build a sidecar that can expose server-local load signals for `HTTP/1.1` workloads without requiring application code changes.

The sidecar should provide:

- server-local request RIF
- recent request latency estimates
- a probe endpoint the ingress data plane can query

### Important principle

The sidecar should infer request lifecycle, not merely connection lifecycle.

The useful events are:

- request start
- response completion

From these, you can derive:

- current RIF: increment on request start, decrement on response completion
- request latency: completion time minus start time
- recent latency summaries for probing

### What not to do

Do not treat these as sufficient:

- number of `ESTABLISHED` sockets
- total open file descriptors
- TCP connection count alone

Those are at best rough pressure signals, not true request-level signals.

### How to do this for `HTTP/1.1`

There are several reasonable approaches. The plan should use them in this order:

#### Stage A: define the signal model first

Before deep eBPF work, define exactly what the sidecar reports:

- `rif`: number of active in-flight HTTP requests currently being processed by the backend
- `latency_median_ms`: median of recent completed request latencies
- `timestamp_ms`
- optional:
  - `sample_count`
  - `latency_p90_ms`
  - `signal_age_ms`

#### Stage B: start with request-aware but simpler instrumentation

Use the simplest approach that can infer request begin and end for `HTTP/1.1`.

Possible options:

- user-space transparent proxy sidecar
  - intercept app traffic locally
  - parse `HTTP/1.1` request boundaries
  - increment RIF when request headers/body are accepted
  - decrement when response is fully sent
- socket-level sidecar with protocol parsing
  - observe reads/writes for the backend process
  - reconstruct request/response boundaries for `HTTP/1.1`

This phase is about getting correct request-aware signals, even if it is not yet the final eBPF implementation.

#### Stage C: eBPF-based lifecycle inference

Once the signal model is validated, move to eBPF-assisted collection.

Potential eBPF strategy:

- attach to socket and syscall boundaries relevant to backend traffic
- correlate events by connection tuple and process identity
- detect request bytes arriving and response completion progress
- maintain per-connection parser state for `HTTP/1.1`
- maintain a sidecar-local in-flight request counter
- record per-request completion durations into a sliding window

The sidecar then serves `/probe` by returning:

- current request-level RIF
- recent latency estimate

### Practical constraints

You should explicitly account for:

- kernel version compatibility
- required capabilities and security posture
- per-connection parser complexity
- chunked responses and persistent connections
- large bodies and streaming behavior
- correctness under retries and client disconnects

### Recommended first implementation rule

For the first useful version:

- support only non-upgraded `HTTP/1.1`
- support keep-alive
- do not promise correctness for `HTTP/2`, WebSockets, or gRPC

---

## 7. Corrected Implementation Roadmap

### Phase 1: Make the ingress core correct (DONE)

- correct ingress-class handling via `spec.ingressClassName`
- path matching using segment-aware trie (Kubernetes `Prefix` semantics)
- exact route with correct fallback to shorter prefix routes
- deterministic route update and deletion behavior
- robust endpoint resolution from `EndpointSlice`
- controller, router, and proxy tests
- Prometheus metrics and health endpoints

### Phase 2: Harden the data plane and algorithm interface (DONE)

- round robin as baseline selector
- selector interface for pluggable algorithms
- explicit policy selection from ingress annotations (`lb/algo`)
- request-level per-backend accounting (RIF tracker, latency tracker)

### Phase 3: Add passive request-level signals in the data plane (DONE)

- per-backend in-flight request counters via atomic RIF tracker
- per-backend completed request latency tracking via circular buffer
- median latency estimation
- least-connections selector using RIF (validated signal correctness)

### Phase 4: Implement Prequal core mechanics (DONE)

- bounded probe pool (16 entries)
- HCL (Hot-Cold Lexicographic) selection rule
- pool management: age timeout (1s), reuse budget, worst-probe removal (alternating oldest/highest-load)
- pool occupancy fallback to random when below 2 entries
- RIF increment on selected probe entries
- virtual probes fed from completed proxy requests (proxy-local signals)
- each proxy instance maintains its own independent probe pool
- no shared probe state or centralized balancing coordinator

### Phase 5: Validation and load testing (NEXT)

Deploy and measure the Prequal implementation:

- rebuild and deploy to kind cluster
- build a backend service with configurable latency/load for realistic testing
- load tests comparing algorithms head-to-head:
  - round robin (baseline)
  - least-connections (RIF-only)
  - Prequal HCL (probe pool with RIF + latency)
- test with 1 ingress replica (client-local = server-local, best case for proxy-local signals)
- test with multiple ingress replicas (client-local ≠ server-local, reveals the blind spot)
- compare tail latency between single and multiple replica setups
- simulate uneven backend load (some backends slower than others)
- simulate antagonist load (external traffic hitting some backends)
- measure churn behavior (pod restarts, scaling up/down)

Primary evaluation metric:

- tail latency improvement (p90, p99, p99.9) under uneven and antagonistic load

Secondary metrics:

- error rate
- reconciliation latency
- request throughput
- backend fairness
- RIF distribution across backends

### Phase 6 (Future): eBPF sidecar and server-local signals

Deferred. The current implementation uses proxy-local signals which work well for single-replica setups. If validation in Phase 5 shows significant degradation with multiple ingress replicas, this phase upgrades signals to server-local:

- eBPF sidecar for `HTTP/1.1` request lifecycle inference
- async probe RPCs to sidecar endpoints
- sidecar-fed probes replace virtual probes in the pool
- no changes to HCL or pool management — only the signal source changes

### Phase 7 (Future): Ingress-controller completeness

- `IngressClass` resource handling
- ingress status updates
- TLS support
- production deployment manifests
- operator documentation

---

## 8. Success Criteria

The project should be considered successful in stages.

### Success level 1: ingress core

- routes correctly according to Kubernetes ingress semantics
- handles endpoint updates correctly
- passes unit and end-to-end routing tests

### Success level 2: signal correctness (DONE)

- request-level RIF measured via atomic counters in the proxy
- per-backend latency tracked via circular buffer with median estimation
- least-connections selector validated RIF signal correctness

### Success level 3: Prequal adaptation (DONE)

- bounded probe pool (16 entries) and HCL selection rule implemented
- pool management: age timeout, reuse budget, worst-probe removal
- virtual probes from proxy-local observations feed the pool

### Success level 4: validation (NEXT)

- load tests demonstrate HCL improves tail latency over round-robin
- single-replica vs multi-replica comparison quantifies client-local signal limitation
- system behaves correctly under backend churn and uneven load

### Success level 5 (future): server-local signals

- eBPF sidecar provides server-local RIF and latency for arbitrary workloads
- measured improvement over proxy-local signals in multi-replica setups

### Success level 6 (future): ingress-controller maturity

- standards-aware ingress behavior
- stable observability
- deployable manifests
- credible operator story

---

## 9. Final Guidance

The project is strongest when described this way:

- a custom Kubernetes ingress controller implementing the Prequal paper's HCL algorithm
- `HTTP/1.1` backends as the initial scope
- proxy-local signals (client-local RIF + latency) as the current signal source
- eBPF sidecar as a future upgrade to server-local signals

Current status: Phases 1-4 are complete. The full Prequal algorithm (probe pool, HCL, pool management) is implemented and running. The next step is validation — deploy, load test, and measure tail latency improvement.

The key validation questions to answer:

1. Does HCL measurably improve tail latency over round-robin under uneven backend load?
2. How much does the improvement degrade with multiple ingress replicas (client-local signal blind spot)?
3. At what point does the blind spot matter enough to justify the eBPF sidecar?
