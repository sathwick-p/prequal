# Prequal: Benchmarking and Load-Testing Plan

## 1. Purpose

This document describes how to evaluate the current system in a way that is:

- faithful to the current implementation
- repeatable
- useful for algorithm comparison
- strong enough to support public claims if executed rigorously

The goals are:

- validate correctness under load
- compare algorithms under realistic traffic
- measure tail-latency impact
- measure probe overhead
- understand failure and churn behavior
- identify where the current implementation differs from the Prequal paper in practice

This document is an execution guide, not a wish list.

---

## 2. Current Implementation Status

The benchmark plan must match the code that exists today.

Current relevant implementation facts:

- the controller now replaces ingress routes atomically during reconciliation
- probe pools are route-scoped, not global
- probe generation is bounded by a worker pool and queue
- pool maintenance is periodic and off the request hot path
- request metrics are route-key based, not raw path based
- benchmark assets now exist under `benchmark/`

Relevant code:

- route-scoped pools: `loadbalancer/pool/pools.go`
- pool maintenance config: `loadbalancer/config.go`
- bounded probing: `loadbalancer/prober.go`
- atomic route replacement: `controller/controller.go`, `controller/router.go`
- benchmark runtime manifests and scripts: `benchmark/`

Important limitations that still exist:

- the backend only reads `WORK_MULTIPLIER` at startup
- the backend always emits current probe timestamps
- there is no built-in runtime fault injection endpoint

Implication:

- “dynamic slowdown” must be tested by rollout / replica-mix change, not by changing one pod live unless you add a control endpoint
- “stale timestamp” testing requires an explicit fault-injection backend or response mangling layer

---

## 3. What Must Be Measured

At minimum, every benchmark run should capture:

- throughput
- median latency
- p95 latency
- p99 latency
- p99.9 latency
- error rate
- backend request distribution
- backend selection distribution by algorithm
- route-scoped pool occupancy
- probe success rate
- probe failure rate by reason
- probe queue depth
- probe drops due to full queue
- CPU and memory usage of:
  - ingress controller pods
  - backend pods

If possible, also capture:

- reconciliation latency under control-plane churn
- stale probe rejection count
- backend-reported probe latency distribution
- backend-reported RIF distribution
- saturation point where tail latency begins rising sharply

If you add more observability later, also capture:

- per-route pool maintenance effects
- fresh pool occupancy
- controller workqueue depth

---

## 4. What Must Be Compared

The primary algorithm comparison set should be:

- `round-robin`
- `least-connections`
- `prequal`

The primary baseline set should also include:

- direct backend baseline, if you want to separate proxy cost from algorithm cost
- proxy with probing effectively disabled, if you want to isolate probe overhead from routing overhead

Every comparison should use the same:

- backend topology
- traffic pattern
- request payload
- request rate model
- runtime duration
- cluster shape

Otherwise results are not comparable.

---

## 5. Benchmark Environments

You should run benchmarks in multiple environments because each one answers a different question.

### Environment A: Local correctness/perf smoke tests

Purpose:

- quick iteration
- obvious regressions
- harness validation

Examples:

- single-node Kubernetes cluster
- `kind`
- `minikube`

Good for:

- validating manifests
- validating scripts
- basic algorithm comparison
- probe-path sanity checks

Not good for:

- final latency claims
- production-like network conclusions

### Environment B: Controlled multi-node cluster

Purpose:

- realistic service routing
- multiple backend replicas
- believable contention

Examples:

- multi-node `kind`
- real cloud test cluster
- bare-metal lab cluster

Good for:

- serious algorithm comparison
- background probing behavior
- route isolation checks
- failure and churn testing

### Environment C: Stress/scale environment

Purpose:

- high route count
- high concurrency
- larger state churn

Good for:

- scalability limits
- controller and data-plane stability
- route-scale cost
- queue pressure and probe drop behavior

---

## 6. Existing Benchmark Assets

These assets already exist in the repository:

- `benchmark/manifests/controller-benchmark.yaml`
- `benchmark/manifests/workload-uniform.yaml`
- `benchmark/manifests/workload-heterogeneous.yaml`
- `benchmark/manifests/workload-multiroute.yaml`
- `benchmark/k6/steady_state.js`
- `benchmark/k6/open_loop.js`
- `benchmark/k6/multi_route.js`
- `benchmark/scripts/churn.sh`

These are the current preferred starting points.

---

## 7. Required Workloads

You need multiple workload shapes. One workload is not enough.

### Workload 1: Uniform backend capacity

Description:

- all backends use the same `WORK_MULTIPLIER`
- all replicas have equal compute behavior

Purpose:

- sanity-check balancing
- verify `prequal` does not regress badly in a simple environment

Asset:

- `benchmark/manifests/workload-uniform.yaml`

### Workload 2: Heterogeneous backend capacity

Description:

- some replicas are intentionally slower via different `WORK_MULTIPLIER`

Purpose:

- verify signal-based routing actually matters

Asset:

- `benchmark/manifests/workload-heterogeneous.yaml`

### Workload 3: Time-varying load

Description:

- request rate ramps up and down over time

Purpose:

- test responsiveness of probe-driven decisions
- observe route-local pool freshness under changing load

### Workload 4: Burst load

Description:

- long quiet periods followed by bursts

Purpose:

- test background probing usefulness
- test idle-to-burst preparedness

### Workload 5: Uneven traffic skew

Description:

- a subset of routes or hosts receives most traffic

Purpose:

- verify route-local behavior under imbalance
- confirm route pools do not bleed across hosts/routes

Asset:

- `benchmark/manifests/workload-multiroute.yaml`
- `benchmark/k6/multi_route.js`

### Workload 6: Failure and timeout workload

Description:

- some probe endpoints fail
- some backends become slow
- some backends return errors

Purpose:

- test resilience of the probing path
- verify fallback and stability

### Workload 7: Churn workload

Description:

- scale backend replicas up/down during live traffic
- update ingress/controller-relevant state during live traffic

Purpose:

- measure control-plane to data-plane stability

Asset:

- `benchmark/scripts/churn.sh`

---

## 8. Benchmark Scenarios

Each scenario should be run for every algorithm under comparison.

### Scenario A: Baseline steady-state

Setup:

- 3-5 healthy backend replicas
- fixed request rate
- fixed request payload

Measure:

- throughput
- median and tail latency
- request distribution

Recommended asset:

- `benchmark/k6/open_loop.js`

### Scenario B: Load ramp

Setup:

- start low
- gradually increase request rate

Measure:

- where p95 starts rising
- where p99 starts rising
- saturation point
- error onset point

### Scenario C: Slow backend minority

Setup:

- mix fast and slow replicas behind one service

Measure:

- fraction of traffic sent to slow replicas
- tail-latency impact
- adaptation speed

### Scenario D: Dynamic backend slowdown

Setup:

- start with one backend mix
- during the run, change effective capacity by rollout or replica-mix change

Examples:

- scale out the slow deployment
- scale in the fast deployment
- replace a fast deployment with a slower image/env config

Measure:

- time to detect and route away
- transient tail-latency spike

Note:

- the backend does not currently support changing `WORK_MULTIPLIER` live in-process

### Scenario E: Probe endpoint degradation

Setup:

- induce non-200 probe responses
- induce probe timeouts
- induce malformed probe payloads if possible

Measure:

- probe failure metrics
- fallback behavior
- user-visible latency effect

Note:

- stale timestamps require explicit fault injection because the current backend always emits current time

### Scenario F: Idle-to-burst transition

Setup:

- allow the system to idle
- then apply sudden high request rate

Measure:

- initial route-scoped pool occupancy
- startup tail latency
- usefulness of background probing

### Scenario G: Churn and reconciliation

Setup:

- add/remove backend replicas
- update ingress rules
- update endpoint slices through scaling

Measure:

- reconciliation latency
- request error spikes during change
- pool adaptation correctness

### Scenario H: Route scale

Setup:

- many routes
- many services
- several hosts

Measure:

- route match stability
- memory footprint
- request latency overhead from routing scale

---

## 9. Algorithm Questions To Answer

The benchmarking effort should answer these exact questions.

### For round-robin

- how much tail latency does it pay under heterogeneous capacity?
- how evenly does it distribute requests?

### For least-connections

- does client-local RIF alone meaningfully outperform round-robin?
- how sensitive is it to probe-free local tracker behavior?

### For prequal

- does backend probing improve over local RIF-only and round-robin?
- how much does probe freshness matter?
- what is the overhead of probing?
- how sensitive is the algorithm to configuration values?
- does route-local probe state remain isolated under multi-route traffic?

---

## 10. Probe-Specific Measurements

Treat the probing subsystem as a first-class benchmark target.

Measure:

- probes sent per second
- probes succeeded per second
- probes failed per second
- probe failures by reason:
  - timeout
  - non_200
  - decode_error
  - stale
- dropped probes by reason:
  - queue_full
- probe queue depth
- distribution of backend-reported `RIF`
- distribution of backend-reported latency estimates
- route-scoped pool occupancy over time

Important questions:

- does probe volume materially affect backend CPU or request latency?
- do probe queue drops appear before tail latency degradation?

---

## 11. Configuration Sweep Plan

Do not benchmark only one configuration.

Sweep at least these:

### Probe rate sweep

Vary:

- `ProbesPerQuery`

Suggested values:

- `0.5`
- `1.0`
- `2.0`
- `3.0`

Goal:

- identify latency benefit vs overhead curve

### Background interval sweep

Vary:

- `BackgroundInterval`

Suggested values:

- `50ms`
- `100ms`
- `250ms`
- `500ms`

Goal:

- understand idle-to-burst preparedness vs wasted probe overhead

### Pool age sweep

Vary:

- `PoolMaxAge`
- `MaxProbeAge`

Goal:

- understand stale-data sensitivity

### Pool maintenance sweep

Vary:

- `PoolMaintenanceInterval`

Suggested values:

- `50ms`
- `100ms`
- `250ms`
- `500ms`

Goal:

- understand maintenance overhead vs stale-pool cleanup responsiveness

### QRIF sweep

Vary:

- `QRIF`

Suggested values:

- `0.5`
- `0.75`
- `0.9`

Goal:

- understand the latency vs RIF tradeoff

### Reuse-limit sweep

Vary:

- `PoolReuseLimit`

Goal:

- understand depletion vs staleness tradeoff

### Probe worker sweep

Vary:

- `ProbeWorkers`
- `TriggerQueueSize`

Goal:

- identify when probe production becomes a bottleneck or starts dropping work

---

## 12. Backend Configuration Sweep

Also vary backend characteristics.

### Capacity skew

Vary:

- `WORK_MULTIPLIER`

Examples:

- all `1.0`
- three fast + one slow at `4.0`
- three fast + one slow at `8.0`

### Replica count

Vary:

- 2 replicas
- 3 replicas
- 5 replicas
- 10 replicas

### Load intensity

Vary:

- low
- medium
- saturation-adjacent
- overloaded

---

## 13. Tooling Recommendations

Primary tools:

- `k6` for repeatable script-driven tests
- `wrk2` if you want another fixed-rate latency analysis tool

Current repository assets:

- `benchmark/k6/steady_state.js`
- `benchmark/k6/open_loop.js`
- `benchmark/k6/multi_route.js`
- `benchmark/scripts/churn.sh`

Cluster-facing helpers:

- `kubectl top`
- Prometheus + Grafana
- direct scraping of `/metrics`

For reproducibility:

- check in scenario configs
- check in result schema
- save raw outputs per run

---

## 14. Metrics Collection Plan

For every run, save:

- algorithm name
- config values
- backend topology
- request rate model
- request rate target
- run duration
- total requests
- success count
- error count
- p50/p95/p99/p99.9
- probe metrics
- pool metrics
- CPU/memory samples

Result format should be machine-readable:

- CSV
- JSON
- or a directory-per-run with raw outputs

Do not rely on ad hoc terminal screenshots.

---

## 15. Run Discipline

To make results trustworthy:

- warm up before measuring
- use fixed-duration steady-state windows
- repeat every scenario multiple times
- report median across runs, not a single lucky result
- isolate noisy background jobs when possible
- keep cluster configuration constant across algorithm comparisons

Suggested pattern:

1. warmup for 30-60s
2. measure for 2-5 minutes
3. repeat 3-5 times
4. aggregate results

For the first serious pass, prefer open-loop testing over closed-loop testing.

---

## 16. Failure and Robustness Testing

Do not benchmark only healthy cases.

You should test:

- backend pod restart during traffic
- backend pod scale-down during traffic
- ingress update during traffic
- probe endpoint returning 500
- probe endpoint timing out
- malformed probe payloads
- empty pool fallback behavior

Only test stale timestamps after you add an explicit fault-injection path.

You want to know:

- does request traffic remain correct?
- do route-local pools recover quickly?
- does the system fail closed or degrade gracefully?

---

## 17. Control-Plane Benchmarks

The data plane is not the only thing that matters.

You should also measure:

- ingress reconciliation latency
- endpoint update propagation latency
- time from backend scale event to new routing behavior
- routing correctness during churn

These are especially important if you claim “ingress controller,” not just “smart proxy.”

---

## 18. Success Criteria

The benchmarking effort should produce answers to these:

1. Is `prequal` better than `round-robin` on tail latency under heterogeneous load?
2. Is `prequal` better than `least-connections` enough to justify probe overhead?
3. What probe configuration gives the best latency/overhead tradeoff?
4. Does the system remain stable during churn, failure, and idle-to-burst transitions?
5. Does route-local state stay isolated under multi-route traffic?
6. Which remaining paper-fidelity refinements are justified by measured results?

If you cannot answer those, benchmarking is not complete.

---

## 19. Deliverables

The full benchmarking work should produce:

- repeatable benchmark scripts
- scenario manifests/configs
- saved raw results
- summary tables
- latency comparison graphs
- probe overhead graphs
- a written conclusion on:
  - best current algorithm
  - best current config
  - route-isolation correctness
  - next refinement worth implementing

If you want to publish the results publicly, also produce:

- environment description
- exact commands and config used
- raw run artifacts or downloadable result bundle
- limitations section

---

## 20. Recommended Execution Order

Do the benchmarking work in this order:

1. deploy `benchmark/manifests/controller-benchmark.yaml`
2. run `workload-uniform.yaml` with `benchmark/k6/steady_state.js`
3. run `workload-uniform.yaml` with `benchmark/k6/open_loop.js`
4. compare algorithms on `workload-heterogeneous.yaml`
5. validate route isolation with `workload-multiroute.yaml` and `benchmark/k6/multi_route.js`
6. run idle-to-burst analysis
7. run churn tests with `benchmark/scripts/churn.sh`
8. run failure scenarios
9. run configuration sweeps

This order gives useful signal early without blocking on full complexity.
