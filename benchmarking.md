# Prequal: Load Testing and Benchmarking Plan

## 1. Purpose

This document describes how to evaluate the current system thoroughly.

The goals are:

- validate correctness under load
- compare algorithms under realistic traffic
- measure tail-latency impact
- measure probe overhead
- understand failure and churn behavior
- identify where the current implementation differs from the Prequal paper in practice

This document is intentionally detailed so it can be used as an execution guide, not just a wish list.

---

## 2. What Must Be Measured

At minimum, every benchmark run should capture:

- throughput
- median latency
- p95 latency
- p99 latency
- p99.9 latency
- error rate
- backend request distribution
- per-backend observed RIF
- backend-reported probe latency
- probe success rate
- probe failure rate by reason
- pool occupancy
- selection algorithm counts
- CPU and memory usage of:
  - ingress controller
  - backend pods

If possible, also capture:

- reconciliation latency under control-plane churn
- stale probe rejection count
- per-backend service time distribution
- saturation point where tail latency begins rising sharply

---

## 3. What Must Be Compared

The primary algorithm comparison set should be:

- `round-robin`
- `least-connections`
- `prequal`

If you later add more variants, compare:

- probe-authoritative `prequal`
- any hybrid fallback mode
- any future paper-fidelity refinements

Every comparison should use the same:

- backend topology
- traffic pattern
- concurrency
- request mix
- runtime duration

Otherwise results are not comparable.

---

## 4. Benchmark Environments

You should run benchmarks in multiple environments because each one answers a different question.

### Environment A: Local correctness/perf smoke tests

Purpose:

- quick iteration
- obvious regressions
- logic validation

Examples:

- single-node Kubernetes cluster
- `kind`
- `minikube`
- local Docker-based setup

Good for:

- validating manifests
- basic algorithm comparison
- probe-path sanity checks

Not good for:

- final latency claims
- noisy-neighbor conclusions
- production-like network conclusions

### Environment B: Controlled multi-node cluster

Purpose:

- realistic service routing
- multiple backend replicas
- more believable contention

Examples:

- multi-node `kind`
- real cloud test cluster
- bare-metal lab cluster

Good for:

- serious algorithm comparison
- background probing behavior
- failure/churn testing

### Environment C: Stress/scale environment

Purpose:

- high route count
- high concurrency
- more replicas
- larger state churn

Good for:

- scalability limits
- probe overhead under load
- controller and data-plane stability

---

## 5. Required Workloads

You need multiple workload shapes. One workload is not enough.

### Workload 1: Uniform backend capacity

Description:

- all backends use the same `WORK_MULTIPLIER`
- all replicas have equal compute behavior

Purpose:

- sanity-check balancing
- verify `prequal` does not regress badly in a simple environment

Expected result:

- round-robin and least-connections may be fairly competitive
- `prequal` should not behave erratically

### Workload 2: Heterogeneous backend capacity

Description:

- different backend groups have different `WORK_MULTIPLIER`
- some replicas are intentionally slower

Purpose:

- verify signal-based routing actually matters

Expected result:

- `prequal` should prefer replicas showing lower effective latency and lower RIF
- round-robin should degrade tail latency more

### Workload 3: Time-varying load

Description:

- request rate ramps up and down over time
- not just a fixed steady-state load

Purpose:

- test responsiveness of probe-driven decisions
- observe pool freshness under changing load

Expected result:

- `prequal` should adapt faster than static or weakly informed methods

### Workload 4: Burst load

Description:

- long quiet periods followed by bursts

Purpose:

- test background probing usefulness
- test stale-pool and bootstrap behavior

Expected result:

- background probing should help avoid empty/stale pool starts

### Workload 5: Uneven traffic skew

Description:

- a subset of routes or hosts receives most traffic

Purpose:

- verify route-local behavior under imbalance
- observe if probe coverage stays healthy

### Workload 6: Failure and timeout workload

Description:

- some probe endpoints fail
- some backends become slow
- some backends return errors

Purpose:

- test resilience of the probing path
- verify fallback and stability

---

## 6. Benchmark Scenarios

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

### Scenario B: Load ramp

Setup:

- start low
- gradually increase concurrency and request rate

Measure:

- where p95 starts rising
- where p99 starts rising
- saturation point
- error onset point

### Scenario C: Slow backend minority

Setup:

- one or two backends use higher `WORK_MULTIPLIER`
- others remain normal

Measure:

- fraction of traffic sent to slow replicas
- tail-latency impact
- adaptation speed

### Scenario D: Dynamic backend slowdown

Setup:

- start with equal backends
- during the run, increase `WORK_MULTIPLIER` for one subset

Measure:

- time to detect and route away
- transient tail-latency spike

### Scenario E: Probe endpoint degradation

Setup:

- induce non-200 probe responses
- induce probe timeouts
- induce stale timestamps if possible

Measure:

- probe failure metrics
- fallback behavior
- user-visible latency effect

### Scenario F: Idle-to-burst transition

Setup:

- allow the system to idle
- then apply sudden high concurrency

Measure:

- initial pool occupancy
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

## 7. Algorithm Questions To Answer

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

---

## 8. Probe-Specific Measurements

You should treat the probing subsystem as a first-class benchmark target.

Measure:

- probes sent per second
- probes succeeded per second
- probes failed per second
- probe failures by reason:
  - timeout
  - non_200
  - decode_error
  - stale
- average probe latency
- distribution of backend-reported `RIF`
- distribution of backend-reported latency estimates
- pool occupancy over time
- fresh occupancy over time if you add that metric later

Important question:

- does probe volume materially affect backend CPU or request latency?

---

## 9. Configuration Sweep Plan

You should not benchmark only one configuration.

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

---

## 10. Backend Configuration Sweep

Also vary backend characteristics.

### Capacity skew

Vary:

- `WORK_MULTIPLIER`

Examples:

- all `1.0`
- two at `1.0`, one at `2.0`
- two at `1.0`, one at `4.0`

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

## 11. Tooling Recommendations

Choose one or two load generators and keep them consistent.

Suggested tools:

- `k6`
- `vegeta`
- `wrk2` if you want fixed-rate latency analysis

Cluster-facing helpers:

- `kubectl top`
- Prometheus + Grafana
- direct scraping of `/metrics`

If you want reproducibility:

- check in benchmark scripts
- check in scenario configs
- check in result schema

---

## 12. Metrics Collection Plan

For every run, save:

- algorithm name
- config values
- backend topology
- request rate
- concurrency
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

## 13. Run Discipline

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

---

## 14. Failure and Robustness Testing

Do not benchmark only healthy cases.

You should test:

- backend pod restart during traffic
- backend pod scale-down
- ingress update during traffic
- probe endpoint returning 500
- probe endpoint timing out
- stale timestamps
- empty pool fallback behavior

You want to know:

- does request traffic remain correct?
- does the pool recover quickly?
- does the system fail closed or degrade gracefully?

---

## 15. Control-Plane Benchmarks

The data plane is not the only thing that matters.

You should also measure:

- ingress reconciliation latency
- endpoint update propagation latency
- time from backend scale event to new routing behavior
- routing correctness during churn

These are especially important if you claim “ingress controller,” not just “smart proxy.”

---

## 16. Success Criteria

The benchmarking effort should produce answers to these:

1. Is `prequal` better than `round-robin` on tail latency under heterogeneous load?
2. Is `prequal` better than `least-connections` enough to justify probe overhead?
3. What probe configuration gives the best latency/overhead tradeoff?
4. Does the system remain stable during churn, failure, and idle-to-burst transitions?
5. Which remaining paper-fidelity refinements are justified by measured results?

If you cannot answer those, benchmarking is not complete.

---

## 17. Deliverables

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
  - next refinement worth implementing

---

## 18. Recommended Execution Order

Do the benchmarking work in this order:

1. baseline steady-state comparison
2. heterogeneous backend comparison
3. load ramp and saturation analysis
4. idle-to-burst analysis
5. failure and timeout scenarios
6. controller churn scenarios
7. configuration sweeps

This order gives you useful signal early without blocking on full complexity.
