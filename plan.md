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
- use probing rather than only passive historical metrics
- use HCL rather than a linear combination of latency and RIF
- use bounded probe pools and async probing

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

### Phase 1: Make the ingress core correct

Build the minimum standards-aware ingress controller core:

- correct ingress-class handling
- legacy ingress annotation support
- path matching that follows Kubernetes semantics
- exact and prefix precedence tests
- deterministic route update and deletion behavior
- robust endpoint resolution from `EndpointSlice`
- coherent route/backend publication model
- maintain and expand controller, router, and proxy tests

Deliverable:

- a controller that behaves correctly for standard `HTTP/1.1` ingress routing

### Phase 2: Harden the data plane and algorithm interface

Strengthen the proxy and balancing interfaces:

- keep round robin as the baseline
- add explicit policy selection from ingress annotations
- separate route metadata from backend metadata
- add better debug and metrics coverage
- add request-level per-backend accounting inside the proxy

Deliverable:

- a clean baseline ingress data plane with pluggable selectors

### Phase 3: Add passive request-level signals in the data plane

Before active probing, validate the core signals in the proxy:

- per-backend in-flight request counters
- per-backend completed request latency tracking
- latency summaries from recent request windows
- least-connections or RIF-only selector as a validation step

Important note:

- this is not yet full Prequal
- it is a signal-validation phase

Deliverable:

- proof that RIF-aware and latency-aware selection behaves sensibly in your ingress proxy

### Phase 4: Implement Prequal core mechanics

Build the actual Prequal-inspired mechanism:

- bounded probe pool
- async probing
- pool occupancy fallback to random when too small
- RIF-conditioned latency estimation
- HCL selection rule
- probe reuse and removal logic
- request-side RIF increment on selected probe entries
- each proxy instance maintains its own independent probe pool
- no shared probe state or centralized balancing coordinator

Important discipline:

- do not replace HCL with a linear combination
- keep this aligned with the paper unless you intentionally document a deviation

Deliverable:

- a Kubernetes-oriented Prequal adaptation using proxy-local signals first, not just a generic "smart" selector

### Phase 5: Build the request-aware sidecar for `HTTP/1.1`

Implement the deployability layer:

- sidecar reports request-level RIF and latency estimates
- start with the simplest request-aware implementation that works
- validate sidecar signals against proxy-observed truth
- document error bounds and unsupported protocols

Deliverable:

- no-application-change signal collection for `HTTP/1.1` backends

### Phase 6: Integrate sidecar-fed probing

Use the sidecar as the source of server-local signals:

- async probes target the sidecar endpoint
- proxy consumes reported RIF and latency values
- compare passive local signals vs sidecar-fed server-local signals
- measure whether server-local signals improve tail latency under uneven load
- keep proxies independent even when probing the same backend set

Deliverable:

- Prequal-style probing without application source-code changes

### Phase 7: Ingress-controller completeness and validation

Move toward a serious ingress-controller implementation:

- `IngressClass` support
- ingress status handling
- better deployment manifests
- high-churn reconciliation tests
- route-scale tests
- end-to-end cluster tests
- load tests comparing:
  - round robin
  - least-connections
  - passive RIF-aware selection
  - Prequal HCL

Primary evaluation metric:

- tail latency improvement under uneven and antagonistic load

Secondary metrics:

- error rate
- reconciliation latency
- request throughput
- backend fairness
- operational complexity

---

## 8. Success Criteria

The project should be considered successful in stages.

### Success level 1: ingress core

- routes correctly according to Kubernetes ingress semantics
- handles endpoint updates correctly
- passes unit and end-to-end routing tests

### Success level 2: signal correctness

- request-level RIF can be measured accurately for `HTTP/1.1`
- recent latency estimates correlate with observed backend behavior
- sidecar signals are validated against a trusted baseline

### Success level 3: Prequal adaptation

- bounded probe pool and HCL are implemented
- sidecar-fed server-local signals work without app changes
- experiments show better tail behavior than round robin in adversarial conditions

### Success level 4: ingress-controller maturity

- standards-aware ingress behavior
- stable observability
- deployable manifests
- credible operator story

---

## 9. Final Guidance

The project is strongest when described this way:

- a real custom ingress controller as the end goal
- `HTTP/1.1` first
- Prequal-inspired backend selection as the advanced policy layer
- sidecar plus eBPF as the deployability mechanism for request-level signals without changing app code

The biggest mistake to avoid is jumping straight to advanced algorithm work before:

- ingress correctness is fixed
- request-level signal collection is well-defined
- the sidecar proves request lifecycle inference rather than connection counting

The right order is:

1. ingress correctness
2. clean selector architecture
3. request-aware signal validation
4. full Prequal mechanics using proxy-local signals first
5. sidecar request lifecycle inference
6. sidecar-fed Prequal signal integration
