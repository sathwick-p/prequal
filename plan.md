# Prequal: Architecture Review, Learning Roadmap, and Implementation Plan

## 1. Executive Summary

The project is moving in a valid direction.

You are building a custom Kubernetes ingress controller with:

- a control plane that watches cluster state
- a data plane that routes live traffic
- room for custom backend-selection algorithms
- an experimental sidecar/probe mechanism for richer load-balancing signals

That is a strong learning project because it forces you to understand:

- Kubernetes controllers and informers
- routing and reverse proxies
- endpoint discovery and reconciliation
- concurrency in Go
- observability
- testing distributed systems
- performance and scale tradeoffs

The high-level architecture is good, but the implementation is still in the "prototype proving the core loop" stage, not the "valid ingress controller" stage yet.

Right now the repository proves these ideas:

- watch `Ingress` and `EndpointSlice`
- build an in-memory routing table
- map routes to backend endpoints
- proxy live traffic to discovered backends

What is still missing is the production-critical layer around that core:

- clean API boundaries
- correct ingress-class handling
- real load-balancing strategies
- failure handling and health policy
- proper metrics and observability
- systematic tests
- scale and benchmark validation

So the answer is:

- direction: correct
- architecture: valid as a foundation
- current implementation: underbuilt relative to the stated ambition
- next move: harden the core before adding advanced algorithm ideas

---

## 2. Current Repository Assessment

### What exists today

- `main.go` wires informers, queue, controller, proxy server, and debug server.
- `controller/controller.go` performs reconciliation from `Ingress` and `EndpointSlice` state into:
  - a route table
  - a backend endpoint store
  - a service-to-ingress mapping for resync triggers
- `controller/router.go` provides host + path matching using a radix tree.
- `server/server.go` proxies requests to the selected backend.
- `probe/probe.go` is a sidecar-style observer for connection information.
- `deploy/controller.yaml` and `test.yaml` provide a basic Kubernetes deployment story.

### What is good

- The split between controller/router/backend store/proxy is sensible.
- Using informer caches plus a workqueue is the right controller pattern.
- Using `EndpointSlice` instead of old `Endpoints` is the correct modern choice.
- Using a radix tree for longest-prefix path matching is a good direction.
- A separate probe process is a reasonable experiment if you want richer balancing signals later.

### What is weak or incomplete

- Ingress class handling is non-standard: the code uses `metadata.labels["ingress.class"]` instead of `spec.ingressClassName` and/or the legacy annotation.
- The proxy always chooses the first backend, so the system is not yet a load balancer in practice.
- There is no explicit algorithm interface yet, even though the architecture aims to support multiple strategies.
- Backend state is only endpoint-address based; there is no health, latency, inflight, or readiness model beyond EndpointSlice readiness.
- The route and store models are tightly coupled to current implementation details.
- There are no unit, integration, or e2e tests.
- There is no metrics pipeline for controller reconciliation or proxy traffic.
- Multi-replica controller behavior and data-plane scale behavior have not been validated.

---

## 3. Is The Architecture Valid?

### Short answer

Yes, with one important clarification:

You are not yet building a full "Ingress Controller competitor". You are building a custom ingress gateway/controller prototype that can evolve into one.

That is the right scope.

### Why the architecture is valid

The control-plane/data-plane split is correct:

- control plane:
  - watch Kubernetes resources
  - reconcile desired routing state
  - publish immutable-ish routing/backend config into memory
- data plane:
  - perform request matching
  - select a backend
  - proxy traffic efficiently

This is how serious systems are structured conceptually, even if mature projects split responsibilities across separate components or embed Envoy/NGINX instead of using Go's `ReverseProxy`.

### Why the architecture is not yet complete

The architecture notes in `arch.md` are ahead of the code. The current repo does not yet fully implement:

- standard ingress API semantics
- robust reconciliation model
- pluggable balancing algorithms
- health-aware endpoint selection
- observability and operator-facing debugging
- correctness tests around routing precedence and updates
- scale behavior under churn

That is fine. It means your next step should be "finish the core platform shape", not "jump to fancy algorithms first".

---

## 4. Architectural Judgment: What To Keep, What To Change

### Keep

- informer + workqueue controller model
- in-memory routing state
- separate router abstraction
- separate proxy abstraction
- `EndpointSlice`-driven backend discovery
- sidecar/probe as an experiment, not as a hard dependency for the first stable version

### Change

- introduce explicit internal domain models instead of passing Kubernetes objects deep into routing logic
- introduce a selector/algorithm interface now, before adding more balancing behavior
- treat the sidecar signal as optional metadata, not required for correctness
- formalize config ownership:
  - ingress parsing
  - endpoint resolution
  - routing state publication
  - backend selection
  - proxying
- add observability before adding advanced heuristics

### Architectural target after the next major phase

Aim for these packages/concepts:

- `controller/`
  - watchers, queue workers, reconciliation
- `routing/`
  - host/path matching and route table
- `discovery/`
  - endpoint resolution and endpoint metadata normalization
- `balancer/`
  - interfaces and algorithms
- `proxy/`
  - request forwarding and transport behavior
- `observability/`
  - metrics, logs, health/debug endpoints

You do not need to do a full package split immediately, but your code changes should move toward these boundaries.

---

## 5. Key Risks In The Current Code

These are the most important issues to address next.

### 5.1 Ingress API semantics are not correct yet

Current code filters using a label named `ingress.class`. That is not how ingress class is typically expressed.

You should support:

- `spec.ingressClassName`
- optionally the legacy annotation `kubernetes.io/ingress.class`

Why this matters:

- correctness
- compatibility with normal Kubernetes usage
- easier testing with standard manifests

### 5.2 The system does not really load balance yet

`server/server.go` always forwards to `backends[0]`.

That means:

- no fairness
- no algorithm behavior
- no resilience to uneven load
- no validation of the core product idea

### 5.3 Reconciliation and state publication need stronger modeling

Today the controller updates router state and endpoint state as separate mutable structures.

This works for a prototype, but it becomes fragile when you add:

- multiple algorithms
- metadata-driven endpoint selection
- retries
- richer routing rules

The next version should move toward a clearer internal model such as:

- `Route`
- `BackendSet`
- `EndpointMetadata`
- `SelectionPolicy`

### 5.4 Testing is effectively absent

`go test ./...` passes because there are no tests.

That is the largest project risk right now because controllers and routing code fail in edge cases, not only in happy paths.

### 5.5 The sidecar/probe idea is promising but premature as a primary mechanism

The sidecar currently exposes observed connection stats, which could be useful.

But if you push too hard on this too early, you risk spending time on:

- noisy signals
- consistency issues
- probe polling complexity
- coupling traffic policy to pod-local observations before the base system is stable

Use it later as an enhancement layer.

---

## 6. What To Implement Next

The next direction should be:

### Phase 1: Stabilize the core controller and proxy

Implement the minimum system that is correct, testable, and extensible:

- standard ingress-class handling
- explicit route model
- explicit backend-selection interface
- round-robin algorithm first
- deterministic router tests
- controller reconciliation tests
- proxy integration tests
- Prometheus metrics and structured logs

Do not make the probe sidecar central yet.

### Phase 2: Add algorithmic value safely

After the core works:

- least-connections
- random-two-choices
- optional sticky routing / hash-based selection
- endpoint metadata and live counters

Only after this should you attempt:

- sidecar-informed balancing
- EWMA latency based selection
- prequalification logic driven by probe data

### Phase 3: Validate scale and operator experience

- churn tests
- higher route counts
- multiple services and hosts
- replica behavior
- benchmark reconciliation latency
- benchmark request throughput and tail latency

---

## 7. Detailed Learning Plan

This section is about what you should learn in parallel with implementation.

### Learning Track A: Kubernetes controllers

#### Learn

- informer lifecycle
- listers vs direct client calls
- workqueue semantics
- reconciliation loops
- idempotent sync functions
- tombstones and delete handling
- cache sync guarantees

#### Learn it by doing

- trace the current event flow from informer event to queue to `syncKey`
- write tests that feed fake ingress and endpointslice objects into the controller logic
- simulate add/update/delete events and verify route/backend state

#### Outcome you should reach

You should be able to explain:

- why controllers queue keys instead of doing work directly in event handlers
- why reconciliation must be idempotent
- how informer cache state differs from live API state

### Learning Track B: Kubernetes ingress semantics

#### Learn

- `Ingress` rule structure
- path precedence rules
- exact vs prefix matching
- default backends
- `IngressClass`
- legacy vs current ingress-class handling

#### Learn it by doing

- create a matrix of ingress manifests for host/path cases
- turn that matrix into unit and integration tests
- compare your router behavior with expected Kubernetes semantics

#### Outcome you should reach

You should be able to state exactly how these should behave:

- `/api` vs `/api/v2`
- exact match `/health`
- empty host / default host
- default backend fallback

### Learning Track C: Go concurrency and state management

#### Learn

- mutex design
- copy-on-read vs copy-on-write
- immutability as a concurrency simplifier
- data races in shared maps/slices
- goroutine lifecycle and shutdown

#### Learn it by doing

- run tests with `go test -race ./...`
- refactor state publication so readers see coherent snapshots
- write tests around concurrent route reads and controller updates

#### Outcome you should reach

You should be able to defend why your shared-state design is safe under concurrent traffic and reconciliation.

### Learning Track D: Reverse proxy and transport behavior

#### Learn

- `httputil.ReverseProxy`
- connection reuse
- transport tuning
- timeout settings
- retry boundaries
- header forwarding and `X-Forwarded-*`

#### Learn it by doing

- add request timeout and transport configuration tests
- inspect how upstream errors propagate
- test backend failures and connection reuse behavior

#### Outcome you should reach

You should understand the difference between:

- route selection
- connection management
- request forwarding
- upstream failure handling

### Learning Track E: Load-balancing algorithms

#### Learn

- round robin
- least connections
- power of two choices
- consistent hashing
- EWMA latency selection
- stickiness tradeoffs

#### Learn it by doing

- start with a tiny `Selector` interface
- write deterministic tests per algorithm
- measure distribution fairness under simulated request patterns

#### Outcome you should reach

You should be able to explain when each algorithm is better or worse.

### Learning Track F: Observability and scale testing

#### Learn

- Prometheus metric types
- RED/USE metrics
- controller metrics
- p50/p95/p99 latency
- load generation
- benchmark design

#### Learn it by doing

- expose request, backend, and reconciliation metrics
- load test with `hey`, `vegeta`, or `k6`
- record routing behavior under backend churn

#### Outcome you should reach

You should be able to answer:

- how many routes/endpoints can this handle?
- what happens during endpoint churn?
- what is the latency overhead of the proxy?

---

## 8. Implementation Roadmap

This roadmap is ordered for learning value and engineering correctness.

### Milestone 1: Make routing semantics correct

#### Goals

- support real ingress-class semantics
- make route matching deterministic and test-covered
- handle host/path/default backend behavior cleanly

#### Tasks

- add ingress parsing helpers:
  - extract class
  - extract rules
  - normalize backend service references
- define internal route structs independent from raw Kubernetes types
- improve router semantics for:
  - exact match
  - longest prefix
  - default host
  - default backend handling
- add unit tests for routing precedence

#### What to learn while doing it

- ingress API
- router design
- table-driven testing in Go

#### Exit criteria

- route tests cover all path precedence cases in `test.yaml` plus additional edge cases
- ingress manifests using `spec.ingressClassName` are supported
- route behavior is deterministic and documented

### Milestone 2: Introduce a real balancing abstraction

#### Goals

- stop hardcoding `backends[0]`
- make algorithm implementation a first-class concept

#### Tasks

- create a `Selector` or `Balancer` interface
- implement `round_robin`
- attach algorithm selection to route config
- keep algorithm state separate from raw endpoint storage
- add deterministic tests for backend selection

#### Suggested interface shape

```go
type Selector interface {
	Select(req *http.Request, endpoints []EndpointView) (EndpointView, error)
}
```

You may later split this into:

- stateless selectors
- stateful selectors

#### What to learn while doing it

- interface design
- stateful algorithms in concurrent systems
- fairness testing

#### Exit criteria

- requests distribute across backends under repeated load
- algorithm behavior is test-covered
- adding a new algorithm does not require editing proxy core logic

### Milestone 3: Harden controller reconciliation

#### Goals

- make sync behavior more reliable and understandable
- reduce coupling and hidden state behavior

#### Tasks

- refactor `syncIngress` into smaller pure-ish helper functions
- define clearer mapping structures:
  - ingress -> routes
  - service -> dependent routes
  - route -> backend set
- review delete/update behavior carefully
- ensure removed or changed routes clean up backend state correctly
- add controller unit tests with fake informers/listers or extracted pure functions

#### What to learn while doing it

- idempotent reconciliation
- controller cleanup logic
- testing with Kubernetes fake objects

#### Exit criteria

- add/update/delete tests pass
- route and backend state stay consistent after updates
- queue reprocessing produces the same final state

### Milestone 4: Add observability before sophistication

#### Goals

- make the system understandable while running
- expose enough signals to debug correctness and performance

#### Tasks

- add Prometheus metrics:
  - request count
  - request duration
  - response status counts
  - backend selection counts
  - active backends per route
  - reconciliation count/errors/duration
- improve logs:
  - route matched
  - backend selected
  - reconciliation result
  - sync failures
- add health/readiness endpoints for the controller process

#### What to learn while doing it

- metrics design
- cardinality pitfalls
- practical debugging of distributed systems

#### Exit criteria

- you can explain what the system is doing without reading raw code
- you can identify broken routes, empty backend sets, and proxy failures quickly

### Milestone 5: Build the test pyramid

#### Goals

- make correctness enforceable
- avoid regressions while you learn

#### Test layers

##### Unit tests

- router matching
- ingress parsing
- endpoint extraction
- selector algorithms
- helper functions

##### Integration tests

- controller reconciliation from fake ingress + endpointslice inputs
- proxy forwarding to `httptest` backends
- route updates reflected in live proxy behavior

##### E2E tests

- deploy to `kind`
- apply ingress + services + deployments
- send traffic through the controller
- validate:
  - route selection
  - backend distribution
  - behavior after pod deletion

#### What to learn while doing it

- table-driven tests
- `httptest`
- `kind`
- race detector
- black-box vs white-box testing

#### Exit criteria

- CI-quality local test suite exists
- `go test ./...` is meaningful
- there is at least one repeatable cluster-level test workflow

### Milestone 6: Add better algorithms

#### Goals

- create actual product differentiation
- build algorithm knowledge safely on top of stable infrastructure

#### Implementation order

1. round robin
2. random
3. power of two choices
4. least connections
5. header/IP hash stickiness
6. EWMA latency

#### Notes

- least-connections needs active request accounting
- EWMA latency needs careful decay and metric freshness
- sticky routing needs clear fallback behavior when endpoints disappear

#### What to learn while doing it

- algorithmic tradeoffs
- distributed systems approximation
- state drift and noisy measurements

#### Exit criteria

- each algorithm has unit tests
- at least round robin, least connections, and hash-based selection have integration validation
- metrics show per-algorithm behavior

### Milestone 7: Integrate the probe sidecar deliberately

#### Goals

- validate whether the sidecar signal adds real value
- keep the main architecture correct even without it

#### Tasks

- define a clear contract for probe data:
  - schema
  - freshness window
  - failure behavior
- decide how controller or proxy retrieves probe data
- cache and bound probe reads
- add algorithm variants that optionally use probe signals

#### Critical warning

Do not make request forwarding depend on per-request probe lookups.

If you use probe data, it should be:

- cached
- optional
- bounded by timeouts
- ignored safely when stale

#### What to learn while doing it

- signal quality vs complexity
- polling and cache design
- failure containment

#### Exit criteria

- probe-enhanced selection works as an optional layer
- stale or missing probe data does not break routing

### Milestone 8: Scale and performance validation

#### Goals

- prove the controller and proxy remain usable under realistic load and churn

#### Tasks

- create load-test scripts
- benchmark:
  - request throughput
  - p95/p99 latency
  - controller reconcile latency
  - backend update propagation latency
- test at increasing scales:
  - 10 routes
  - 100 routes
  - 1000 routes
  - increasing endpoint counts per service
- simulate churn:
  - pod restarts
  - scaling deployments up/down
  - frequent ingress updates

#### What to learn while doing it

- benchmarking methodology
- profiling
- memory and CPU analysis
- scale bottleneck identification

#### Exit criteria

- you have measured limits, not guesses
- you know the next bottleneck
- architecture decisions are supported by data

---

## 9. Testing Strategy In Detail

Testing should not be a final phase. It should be built alongside each milestone.

### Immediate test files to create

- `controller/router_test.go`
- `controller/ingress_parser_test.go`
- `controller/controller_test.go`
- `server/server_test.go`
- `loadbalancer/prequal/selector_test.go`

### Initial test cases

#### Router tests

- exact `/health` beats prefix `/`
- `/api/v2` beats `/api`
- unknown host falls back to default host only when appropriate
- exact path does not match longer paths
- route removal on ingress update/delete works correctly

#### Controller tests

- ingress add populates route and backend store
- endpointslice update refreshes backend store
- ingress delete removes route mappings
- endpoint readiness filtering works
- named port and numeric port cases both work

#### Proxy tests

- request is forwarded to matched backend
- no route returns `404`
- no backends returns `503`
- backend error returns `502`
- round robin distributes requests across backends

#### Concurrency and safety

- `go test -race ./...`
- repeated route updates while serving requests
- repeated endpoint churn while selecting backends

### E2E environment

Use `kind` and automate:

- cluster creation
- image build/load
- controller deploy
- test workload deploy
- ingress apply
- request validation
- teardown

---

## 10. Suggested Refactor Sequence

Refactor in this order to avoid chaos.

1. Add tests around current router behavior before changing it.
2. Introduce ingress parsing helpers.
3. Introduce internal route model.
4. Introduce selector interface with round robin.
5. Move proxy selection logic behind the selector.
6. Add metrics and health endpoints.
7. Refactor reconciliation internals for clarity.
8. Add more algorithms.
9. Add probe integration.

This order matters because it keeps the system working while you increase sophistication.

---

## 11. What You Need To Learn Exactly, In Order

If you want the learning path to track implementation, use this sequence.

### Week/Block 1: Controller fundamentals

- informers
- listers
- workqueues
- idempotent reconciliation
- Kubernetes ingress resource structure

Build:

- ingress-class fix
- route parsing helpers
- controller tests for add/update/delete

### Week/Block 2: Routing and proxying

- radix/prefix matching
- `httputil.ReverseProxy`
- transport tuning
- timeout behavior

Build:

- router correctness improvements
- proxy tests
- health/debug endpoint cleanup

### Week/Block 3: Load balancing basics

- round robin
- random
- least connections
- state management for selectors

Build:

- selector interface
- round robin implementation
- algorithm-based route config

### Week/Block 4: Observability and reliability

- Prometheus metrics
- structured logging
- race detection
- failure-mode testing

Build:

- metrics endpoint
- request/reconcile metrics
- better logs
- race-safe validation

### Week/Block 5: Cluster-level validation

- `kind`
- realistic test deployments
- endpoint churn
- benchmark tooling

Build:

- repeatable e2e workflow
- scale scripts
- churn and failover tests

### Week/Block 6+: Advanced algorithms and probe integration

- consistent hashing
- EWMA latency
- queueing/load heuristics
- signal freshness and staleness handling

Build:

- advanced selectors
- optional probe-assisted routing
- measured comparison of algorithms

---

## 12. Practical Next Sprint Plan

If you only do one focused sprint next, do this exact sequence.

### Sprint goal

Turn the project from "interesting prototype" into "correct, testable ingress controller core".

### Sprint tasks

1. Fix ingress-class handling.
2. Add table-driven router tests.
3. Add controller tests for ingress + endpointslice reconciliation.
4. Introduce a selector interface.
5. Implement round robin.
6. Update proxy to use the selector.
7. Add basic Prometheus metrics and health endpoints.
8. Add `go test -race ./...` to your local validation workflow.

### Sprint deliverables

- correct ingress parsing
- real load balancing
- meaningful automated tests
- basic observability

### Sprint learning outcomes

By the end of that sprint you should understand:

- how a controller actually reconciles cluster state
- how route matching correctness is validated
- how a reverse proxy and balancer interact
- how to add features without destroying architecture

---

## 13. Definition Of "Moving In The Right Direction"

You are moving in the right direction if, after the next 2 to 3 milestones, the project can do all of this reliably:

- watch ingress and endpointslice changes
- build correct route state
- distribute requests across live backends
- survive backend churn
- expose enough metrics/logs to debug behavior
- pass unit and integration tests consistently
- run repeatable e2e validation in `kind`

If you cannot do those things yet, do not jump to advanced "prequal" intelligence. Finish the platform core first.

---

## 14. Final Recommendation

The best next direction is:

- keep the architecture
- harden the controller/proxy core
- add tests before complexity
- add round robin before advanced algorithms
- treat the sidecar probe as a later optimization and research track

In practical terms:

build a clean, correct, test-covered ingress controller core first; then layer in smarter balancing.

---

## 15. RIF And Estimated Latency: eBPF Strategy

This section updates the earlier recommendation with a more precise direction for collecting:

- RIF: requests or connections in flight
- estimated latency: backend response latency or connection-level latency

### Short answer

Yes, learning eBPF here is a strong idea, but it should be used carefully.

The right architecture is not:

- "replace core balancing with eBPF immediately"

The right architecture is:

- keep proxy-level instrumentation as the source of truth for request lifecycle inside the ingress
- use eBPF as an optional signal pipeline for deeper socket/network visibility
- aggregate those signals safely across multiple ingress pods

### What RIF should mean in this project

You need to define this clearly before implementing anything.

There are two different meanings:

#### Option A: Request inflight count

This means:

- how many HTTP requests are currently being served for a backend

This is the best signal for:

- HTTP-aware least-connections
- request scheduling inside your ingress proxy

Best place to measure it:

- inside your Go proxy process

Why:

- exact
- cheap
- request-aware
- works correctly even when HTTP keepalive reuses one TCP connection for many requests

#### Option B: Connection inflight count

This means:

- how many active TCP connections currently exist for a backend or pod

This is the signal your current probe sidecar is closest to.

Best place to measure it:

- eBPF or kernel/proc observation

Why:

- visible without application instrumentation
- useful for TCP-oriented traffic
- useful as a rough load heuristic

But it is weaker than request inflight for HTTP load balancing because:

- one connection may carry many requests
- idle keepalive connections can distort the signal
- HTTP/2 multiplexing breaks "one connection ~= one active request"

### Recommendation

Use this definition split:

- primary RIF for balancing: request inflight in the ingress proxy
- secondary RIF for experiments: connection inflight from eBPF

That gives you a correct baseline and still lets you learn eBPF meaningfully.

### What estimated latency should mean

You should also separate two kinds of latency:

#### Proxy-observed request latency

This is:

- time from forwarding request upstream to receiving response headers/body completion

Measure this in the ingress process first.

This is the best signal for:

- EWMA latency balancing
- request-level routing decisions

#### Network/socket latency

This is:

- connect latency
- retransmission behavior
- RTT-like transport signals
- socket queuing / kernel timing hints

This is where eBPF can help, but it is not a drop-in replacement for request latency.

### eBPF is a good fit for these cases

- observing TCP connect/close lifecycle per backend pod
- measuring connection establishment latency
- counting active sockets per pod/backend
- capturing kernel-level network health signals
- building pod-local load hints without modifying the app container

### eBPF is a poor first fit for these cases

- exact HTTP inflight requests
- exact per-request end-to-end latency in a keepalive-heavy proxy
- making every routing decision depend on synchronous kernel probing

---

## 16. Multi-Ingress-Pod eBPF Architecture

If you run multiple ingress pods, you must decide whether balancing signals are:

- local to each ingress pod
- or globally shared across all ingress pods

### Recommended model

Start with local decision-making and optional global approximation.

#### Local signals per ingress pod

Each ingress pod keeps:

- local request inflight counters
- local EWMA latency per backend
- local backend selection state

This is fast and simple.

It works well because each pod only needs to choose well for the requests it receives.

#### Optional cluster-wide signal sharing

Add this only later if you need cluster-wide least-connections behavior.

You can aggregate:

- eBPF-derived connection counts
- proxy-derived request inflight counts
- proxy-derived latency EWMAs

But the sharing should be:

- asynchronous
- approximate
- bounded by freshness windows

Do not try to build a strongly consistent global load-balancing state first.

That complexity is not worth it at this stage.

### Best deployment shape for eBPF

For eBPF, the cleanest model is:

- a node-level eBPF agent as a DaemonSet
- each agent observes socket/network events on its node
- it exports summarized metrics keyed by:
  - pod IP
  - namespace
  - service/backend identity
  - timestamp/freshness

Then your ingress pods or controller can consume summarized state, not raw kernel events.

Why this is better than one sidecar per app pod:

- eBPF usually needs elevated privileges and kernel access
- node-level deployment is operationally more realistic
- one agent can observe many pods on the node
- you avoid putting privileged logic in every workload pod

### Data flow for the recommended architecture

1. Ingress proxy records request inflight and request latency locally.
2. Node eBPF agent observes socket-level activity and exports connection metrics.
3. A lightweight collector or shared cache aggregates metrics by backend pod.
4. Each ingress pod periodically refreshes backend metrics into an in-memory cache.
5. Selector algorithms use:
   - proxy-local request inflight as the primary signal
   - optional eBPF connection/load hints as secondary signals

### Important rule

Never make the request path depend on querying eBPF data synchronously.

Always use cached snapshots with:

- timeout bounds
- freshness TTLs
- safe fallback to simpler algorithms

---

## 17. What To Learn For The eBPF Path

If you want this to be a learning track, do it in this order.

### Stage 1: Networking and Linux basics

Learn:

- TCP lifecycle
- listen, accept, connect, close
- keepalive
- HTTP/1.1 vs HTTP/2 multiplexing
- socket states and why connection count is only an approximation

Implement:

- document exactly what signal you want to collect
- define metric schemas for:
  - inflight requests
  - active TCP connections
  - connect latency
  - EWMA request latency

### Stage 2: Proxy-native instrumentation first

Learn:

- middleware timing
- atomic counters
- histogram and EWMA calculation

Implement:

- per-backend inflight request counters in the ingress proxy
- per-backend request latency measurement
- EWMA latency update logic
- tests proving counters increment/decrement correctly on success and failure

Why this comes first:

- this gives you a correct baseline before eBPF

### Stage 3: eBPF fundamentals

Learn:

- BPF maps
- kprobes
- tracepoints
- perf/ring buffers
- verifier constraints
- CO-RE
- user space loader pattern

Implement:

- a minimal eBPF program that tracks TCP connect/close events
- user space code that reads events and maintains:
  - active connections per pod/backend IP
  - connect latency samples if available from chosen hooks

### Stage 4: Kubernetes identity mapping

Learn:

- mapping IPs to pods
- CNI/network namespace implications
- node-local visibility

Implement:

- a node agent that enriches socket events with pod metadata
- stable keys such as:
  - namespace/pod
  - service key
  - endpoint key

### Stage 5: Aggregate and consume metrics safely

Learn:

- pull vs push metrics
- staleness handling
- cache invalidation

Implement:

- a small in-memory metrics cache in the ingress
- freshness TTL
- fallback behavior when metrics are missing
- metrics snapshot format for debugging

### Stage 6: Algorithm experiments

Learn:

- combining strong and weak signals
- noisy metric smoothing
- bias and oscillation in adaptive balancing

Implement:

- least-connections using proxy inflight counts
- latency-aware selection using proxy EWMA
- hybrid selector that uses eBPF connection counts only as a tie-breaker or penalty term

---

## 18. Concrete Implementation Plan For eBPF Integration

Follow this exact order.

### Step 1: Add correct request-level metrics in the ingress

Implement:

- `inflight_requests{backend}`
- `request_duration_seconds{backend}`
- backend-local EWMA latency state

Do not start with eBPF before this exists.

### Step 2: Build least-connections and EWMA selectors without eBPF

Implement:

- least-connections from proxy inflight counters
- EWMA latency selector from proxy-observed durations

This proves your balancing framework.

### Step 3: Prototype eBPF as a separate node agent

Implement:

- node DaemonSet
- eBPF program for socket lifecycle events
- user space exporter
- debug output only at first

Success criteria:

- you can print active connection counts per backend pod reliably

### Step 4: Add a metrics API between eBPF agent and ingress

Implement one of:

- Prometheus scrape path
- node-local HTTP endpoint
- gRPC stream if you need lower latency later

Recommended first choice:

- Prometheus-style or simple HTTP JSON endpoint

Keep it simple.

### Step 5: Ingest eBPF metrics into the ingress as optional hints

Implement:

- periodic background refresh
- cache by backend endpoint key
- freshness TTL
- selector fallback when metrics are stale

### Step 6: Compare algorithm quality

Test:

- round robin
- least-connections using proxy inflight
- EWMA using proxy latency
- hybrid EWMA + eBPF connection penalty

Measure:

- throughput
- p95/p99 latency
- fairness across backends
- recovery under pod churn

### Step 7: Decide if eBPF adds enough value

Possible outcomes:

- eBPF materially improves decisions under some workloads
- eBPF is useful only for observability, not balancing
- eBPF is not worth the operational complexity yet

All three are valid outcomes.

The learning still pays off.

That path will maximize both learning value and engineering quality.
